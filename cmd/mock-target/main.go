package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

type State struct {
	mu   sync.RWMutex
	mode string
}

func (s *State) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *State) Set(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	state := &State{mode: "ok"}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		mode := state.Get()
		w.Header().Set("Content-Type", "application/json")

		switch mode {
		case "slow":
			time.Sleep(2500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "degraded",
				"detail": "high database query latency detected (2.5s)",
			})
		case "error":
			logger.Error("database connection pool exhausted",
				"component", "postgres-primary",
				"active_conns", 100,
				"max_conns", 100,
				"err", "dial tcp 10.0.1.42:5432: connect: connection refused",
			)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "unhealthy",
				"error":  "FATAL: remaining connection slots are reserved for superuser (SQLSTATE 53300)",
			})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "healthy",
			})
		}
	})

	mux.HandleFunc("POST /simulate", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode != "ok" && mode != "error" && mode != "slow" {
			http.Error(w, "invalid mode: use ok, error, or slow", http.StatusBadRequest)
			return
		}
		state.Set(mode)
		logger.Info("simulation mode changed", "mode", mode)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"mode": mode})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	logger.Info("mock-target listening", "addr", ":"+port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
