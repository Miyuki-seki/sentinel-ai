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
	"syscall"
	"time"

	"github.com/Miyuki-seki/sentinel-ai/internal/analyzer"
	"github.com/Miyuki-seki/sentinel-ai/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	targetURL := getEnv("TARGET_URL", "http://localhost:8081/health")
	intervalSec, _ := strconv.Atoi(getEnv("CHECK_INTERVAL_SEC", "5"))
	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "./data/sentinel.db")
	apiKey := os.Getenv("GEMINI_API_KEY")

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

	go runProber(ctx, logger, dbStore, aiAnalyzer, targetURL, time.Duration(intervalSec)*time.Second)

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
		logger.Info("monitor web server listening", "addr", srv.Addr, "target", targetURL)
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
	url string,
	interval time.Duration,
) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0

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

		// 2回連続失敗でインシデント化（既存のOPENインシデントがあれば重複起票しない）
		if consecutiveFailures >= 2 {
			hasOpen, err := dbStore.HasOpenIncident(url)
			if err != nil || hasOpen {
				return
			}

			logger.Info("triggering AI incident triage...", "url", url)
			triageCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			report, err := aiAnalyzer.Analyze(triageCtx, analyzer.ProbeFailure{
				TargetURL:  url,
				StatusCode: statusCode,
				LatencyMs:  latency,
				RawBody:    bodyStr,
				ErrMessage: errMsg,
			})
			if err != nil {
				logger.Error("ai triage failed", "err", err)
				return
			}

			sample := bodyStr
			if sample == "" {
				sample = errMsg
			}

			id, err := dbStore.CreateIncident(store.Incident{
				TargetURL:   url,
				StatusCode:  statusCode,
				ErrorSample: sample,
				Summary:     report.Summary,
				RootCause:   report.RootCause,
				Severity:    report.Severity,
				Remediation: report.Remediation,
			})
			if err != nil {
				logger.Error("failed to save incident", "err", err)
				return
			}

			logger.Info("AI incident report saved",
				"incident_id", id,
				"severity", report.Severity,
				"summary", report.Summary,
			)
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
