package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Miyuki-seki/sentinel-ai/internal/analyzer"
	"github.com/Miyuki-seki/sentinel-ai/internal/notifier"
	"github.com/Miyuki-seki/sentinel-ai/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	targetURLs := parseTargetURLs()
	intervalSec, _ := strconv.Atoi(getEnv("CHECK_INTERVAL_SEC", "5"))
	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "./data/sentinel.db")
	apiKey := os.Getenv("GEMINI_API_KEY")
	discordWebhookURL := os.Getenv("DISCORD_WEBHOOK_URL")

	probeRetentionHours, _ := strconv.Atoi(getEnv("PROBE_LOG_RETENTION_HOURS", "24"))
	incidentRetentionDays, _ := strconv.Atoi(getEnv("INCIDENT_RETENTION_DAYS", "30"))
	cleanupIntervalMin, _ := strconv.Atoi(getEnv("CLEANUP_INTERVAL_MIN", "60"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		logger.Error("failed to initialize sqlite store", "err", err)
		os.Exit(1)
	}
	defer dbStore.Close()

	var aiAnalyzer analyzer.Analyzer
	if apiKey != "" && apiKey != "your_gemini_api_key_here" {
		ga, err := analyzer.NewGeminiAnalyzer(ctx, apiKey)
		if err != nil {
			logger.Error("failed to initialize gemini analyzer", "err", err)
			os.Exit(1)
		}
		aiAnalyzer = ga
		logger.Info("gemini ai analyzer enabled")
	} else {
		aiAnalyzer = &analyzer.FallbackAnalyzer{}
		logger.Warn("GEMINI_API_KEY not set; using fallback analyzer")
	}

	var discordNotifier notifier.Notifier
	if discordWebhookURL != "" {
		discordNotifier = notifier.NewDiscordNotifier(discordWebhookURL)
		logger.Info("discord webhook notifier enabled")
	} else {
		logger.Info("DISCORD_WEBHOOK_URL not set; notifications will be skipped")
	}

	interval := time.Duration(intervalSec) * time.Second
	for _, targetURL := range targetURLs {
		logger.Info("starting independent prober", "url", targetURL, "interval_sec", intervalSec)
		go runProber(ctx, logger, dbStore, aiAnalyzer, discordNotifier, targetURL, interval)
	}

	go runCleanupWorker(
		ctx,
		logger,
		dbStore,
		time.Duration(probeRetentionHours)*time.Hour,
		time.Duration(incidentRetentionDays)*24*time.Hour,
		time.Duration(cleanupIntervalMin)*time.Minute,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Sentinel-AI Monitor is running. Check GET /api/incidents for JSON reports.\n"))
	})
	mux.HandleFunc("GET /api/incidents", func(w http.ResponseWriter, r *http.Request) {
		incidents, err := dbStore.ListRecentIncidents(20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(incidents)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go func() {
		logger.Info("monitor web server listening", "addr", srv.Addr, "targets", targetURLs)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func runProber(
	ctx context.Context,
	logger *slog.Logger,
	dbStore *store.SQLiteStore,
	aiAnalyzer analyzer.Analyzer,
	discordNotifier notifier.Notifier,
	url string,
	interval time.Duration,
) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var consecutiveFailures int
	var isTriaging atomic.Bool

	check := func() {
		start := time.Now()
		resp, err := client.Get(url)
		latency := time.Since(start).Milliseconds()

		var statusCode int
		var bodyStr, errMsg string
		healthy := false

		if err != nil {
			errMsg = err.Error()
		} else {
			statusCode = resp.StatusCode
			bodyBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			bodyStr = string(bodyBytes)
			healthy = statusCode >= 200 && statusCode < 400
		}

		_ = dbStore.RecordProbe(url, statusCode, latency, healthy)

		if healthy {
			if consecutiveFailures > 0 {
				logger.Info("target recovered", "url", url, "previous_failures", consecutiveFailures)
			}
			consecutiveFailures = 0

			if resolved, err := dbStore.ResolveOpenIncidents(url); err == nil && resolved > 0 {
				logger.Info("open incident resolved automatically", "url", url, "count", resolved)
			}
			logger.Info("probe ok", "url", url, "status", statusCode, "latency_ms", latency)
			return
		}

		consecutiveFailures++
		logger.Warn("unhealthy target detected",
			"url", url,
			"consecutive_failures", consecutiveFailures,
			"status", statusCode,
			"latency_ms", latency,
			"err", errMsg,
		)

		// 2回連続失敗でインシデント化
		if consecutiveFailures >= 2 {
			hasOpen, err := dbStore.HasOpenIncident(url)
			if err != nil || hasOpen {
				return
			}

			// 既にトリアージ実行中であれば二重起動を防ぐ
			if !isTriaging.CompareAndSwap(false, true) {
				logger.Debug("triage already in progress, skipping duplicate run", "url", url)
				return
			}

			// プロービング周期をブロックしないよう、トリアージ処理を非同期で実行
			go func(url string, sc int, lat int64, rawBody, errTxt string) {
				defer isTriaging.Store(false)

				logger.Info("triggering AI incident triage...", "url", url)
				triageCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				defer cancel()

				report, err := aiAnalyzer.Analyze(triageCtx, analyzer.ProbeFailure{
					TargetURL:  url,
					StatusCode: sc,
					LatencyMs:  lat,
					RawBody:    rawBody,
					ErrMessage: errTxt,
				})
				if err != nil {
					logger.Error("ai triage failed", "url", url, "err", err)
					return
				}

				sample := rawBody
				if sample == "" {
					sample = errTxt
				}

				inc := store.Incident{
					TargetURL:   url,
					StatusCode:  sc,
					ErrorSample: sample,
					Summary:     report.Summary,
					RootCause:   report.RootCause,
					Severity:    report.Severity,
					Remediation: report.Remediation,
					CreatedAt:   time.Now().UTC(),
				}

				id, err := dbStore.CreateIncident(inc)
				if err != nil {
					logger.Error("failed to save incident", "url", url, "err", err)
					return
				}
				inc.ID = id

				logger.Info("AI incident report saved",
					"incident_id", id,
					"url", url,
					"severity", report.Severity,
					"summary", report.Summary,
				)

				// Discord Webhook へ投稿
				if discordNotifier != nil {
					notifyCtx, notifyCancel := context.WithTimeout(ctx, 10*time.Second)
					defer notifyCancel()

					if err := discordNotifier.SendIncident(notifyCtx, inc); err != nil {
						logger.Error("failed to send incident to discord", "url", url, "err", err)
					} else {
						logger.Info("incident posted to discord successfully", "url", url, "incident_id", id)
					}
				}
			}(url, statusCode, latency, bodyStr, errMsg)
		}
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func parseTargetURLs() []string {
	raw := os.Getenv("TARGET_URLS")
	if raw == "" {
		raw = os.Getenv("TARGET_URL")
	}
	if raw == "" {
		raw = "http://localhost:8081/health"
	}

	var urls []string
	for _, u := range strings.Split(raw, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return []string{"http://localhost:8081/health"}
	}
	return urls
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runCleanupWorker(
	ctx context.Context,
	logger *slog.Logger,
	dbStore *store.SQLiteStore,
	probeRetention time.Duration,
	incidentRetention time.Duration,
	cleanupInterval time.Duration,
) {
	if cleanupInterval <= 0 {
		cleanupInterval = 1 * time.Hour
	}

	clean := func() {
		logger.Debug("starting periodic log cleanup...")
		deletedProbes, err := dbStore.CleanupProbeLogs(probeRetention)
		if err != nil {
			logger.Error("failed to cleanup probe logs", "err", err)
		}

		deletedIncidents, err := dbStore.CleanupResolvedIncidents(incidentRetention)
		if err != nil {
			logger.Error("failed to cleanup resolved incidents", "err", err)
		}

		if deletedProbes > 0 || deletedIncidents > 0 {
			logger.Info("periodic log cleanup completed",
				"deleted_probe_logs", deletedProbes,
				"deleted_resolved_incidents", deletedIncidents,
				"probe_retention", probeRetention,
				"incident_retention", incidentRetention,
			)
		}
	}

	// 起動時初回実行
	clean()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clean()
		}
	}
}
