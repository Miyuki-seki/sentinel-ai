package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Miyuki-seki/sentinel-ai/internal/store"
)

type Notifier interface {
	SendIncident(ctx context.Context, inc store.Incident) error
}

type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewDiscordNotifier(webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordEmbedFooter struct {
	Text string `json:"text"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Footer      *discordEmbedFooter `json:"footer,omitempty"`
}

type discordWebhookPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

func (d *DiscordNotifier) SendIncident(ctx context.Context, inc store.Incident) error {
	if d == nil || d.webhookURL == "" {
		return nil
	}

	// Severity によるカラー設定 (CRITICAL: 赤 0xE74C3C, WARNING: 橙 0xE67E22)
	color := 0xE74C3C
	if inc.Severity == "WARNING" {
		color = 0xE67E22
	}

	title := fmt.Sprintf("🚨 [%s] Incident Detected: %s", inc.Severity, inc.TargetURL)
	if len(title) > 256 {
		title = title[:253] + "..."
	}

	var fields []discordEmbedField
	fields = append(fields, discordEmbedField{
		Name:   "Target URL",
		Value:  truncate(inc.TargetURL, 1024),
		Inline: true,
	})

	statusCodeStr := fmt.Sprintf("%d", inc.StatusCode)
	if inc.StatusCode == 0 {
		statusCodeStr = "N/A (Connection Error)"
	}
	fields = append(fields, discordEmbedField{
		Name:   "Status Code",
		Value:  statusCodeStr,
		Inline: true,
	})

	if inc.RootCause != "" {
		fields = append(fields, discordEmbedField{
			Name:   "Root Cause",
			Value:  truncate(inc.RootCause, 1024),
			Inline: false,
		})
	}

	if len(inc.Remediation) > 0 {
		var sb strings.Builder
		for i, rem := range inc.Remediation {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, rem))
		}
		fields = append(fields, discordEmbedField{
			Name:   "Remediation",
			Value:  truncate(sb.String(), 1024),
			Inline: false,
		})
	}

	if inc.ErrorSample != "" {
		sample := strings.TrimSpace(inc.ErrorSample)
		if len(sample) > 900 {
			sample = sample[:900] + "..."
		}
		fields = append(fields, discordEmbedField{
			Name:   "Error Sample",
			Value:  fmt.Sprintf("```\n%s\n```", sample),
			Inline: false,
		})
	}

	createdAt := inc.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	payload := discordWebhookPayload{
		Embeds: []discordEmbed{
			{
				Title:       title,
				Description: truncate(inc.Summary, 2048),
				Color:       color,
				Fields:      fields,
				Timestamp:   createdAt.Format(time.RFC3339),
				Footer: &discordEmbedFooter{
					Text: "Sentinel-AI SRE Agent",
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("send discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
