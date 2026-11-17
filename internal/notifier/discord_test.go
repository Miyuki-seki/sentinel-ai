package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Miyuki-seki/sentinel-ai/internal/store"
)

func TestDiscordNotifier_SendIncident(t *testing.T) {
	var receivedPayload discordWebhookPayload

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	n := NewDiscordNotifier(ts.URL)

	inc := store.Incident{
		TargetURL:   "http://service.local/health",
		StatusCode:  503,
		Summary:     "Service Unavailable due to overload",
		RootCause:   "Thread pool exhausted",
		Severity:    "CRITICAL",
		Remediation: []string{"Scale replicas", "Restart pods"},
		ErrorSample: "HTTP 503 Backend Timeout",
		CreatedAt:   time.Now().UTC(),
	}

	err := n.SendIncident(context.Background(), inc)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(receivedPayload.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(receivedPayload.Embeds))
	}

	embed := receivedPayload.Embeds[0]
	if embed.Color != 0xE74C3C {
		t.Errorf("expected critical color 0xE74C3C, got %x", embed.Color)
	}
	if embed.Description != inc.Summary {
		t.Errorf("expected summary %q, got %q", inc.Summary, embed.Description)
	}
}

func TestDiscordNotifier_EmptyURL(t *testing.T) {
	n := NewDiscordNotifier("")
	err := n.SendIncident(context.Background(), store.Incident{})
	if err != nil {
		t.Fatalf("expected nil error when webhook URL is empty, got %v", err)
	}
}
