package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

type GeminiAnalyzer struct {
	client *genai.Client
	model  string
}

func NewGeminiAnalyzer(ctx context.Context, apiKey string) (*GeminiAnalyzer, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("init genai client: %w", err)
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-3.8-flash"
	}

	return &GeminiAnalyzer{
		client: client,
		model:  model,
	}, nil
}

func (g *GeminiAnalyzer) Analyze(ctx context.Context, failure ProbeFailure) (TriageReport, error) {
	prompt := fmt.Sprintf(`You are an experienced Site Reliability Engineer (SRE).
Analyze the following service health check failure and return a structured incident triage report.

Target URL: %s
HTTP Status Code: %d
Latency: %d ms
Transport Error: %s
Response / Error Log Payload:
%s
`, failure.TargetURL, failure.StatusCode, failure.LatencyMs, failure.ErrMessage, failure.RawBody)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"summary": {
					Type:        genai.TypeString,
					Description: "Concise one-line summary of the incident.",
				},
				"root_cause": {
					Type:        genai.TypeString,
					Description: "Most likely technical root cause based on the error payload.",
				},
				"severity": {
					Type:        genai.TypeString,
					Enum:        []string{"CRITICAL", "WARNING"},
					Description: "Incident severity level.",
				},
				"remediation": {
					Type:        genai.TypeArray,
					Items:       &genai.Schema{Type: genai.TypeString},
					Description: "Actionable troubleshooting commands or recovery steps.",
				},
			},
			Required: []string{"summary", "root_cause", "severity", "remediation"},
		},
	}

	resp, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(prompt), config)
	if err != nil {
		return TriageReport{}, fmt.Errorf("gemini generate content: %w", err)
	}

	var report TriageReport
	if err := json.Unmarshal([]byte(resp.Text()), &report); err != nil {
		return TriageReport{}, fmt.Errorf("parse structured output: %w", err)
	}
	return report, nil
}

// FallbackAnalyzer is used when GEMINI_API_KEY is not configured (e.g., local quick demo).
type FallbackAnalyzer struct{}

func (f *FallbackAnalyzer) Analyze(_ context.Context, failure ProbeFailure) (TriageReport, error) {
	cause := "Upstream service returned an unexpected error response."
	if strings.Contains(failure.RawBody, "53300") || strings.Contains(failure.RawBody, "connection") {
		cause = "PostgreSQL connection pool exhaustion (SQLSTATE 53300) preventing new database sessions."
	}
	return TriageReport{
		Summary:   fmt.Sprintf("Health check failed on %s (HTTP %d)", failure.TargetURL, failure.StatusCode),
		RootCause: cause + " (Simulated fallback mode: set GEMINI_API_KEY for live LLM analysis)",
		Severity:  "CRITICAL",
		Remediation: []string{
			"Check active DB connections: SELECT count(*) FROM pg_stat_activity;",
			"Inspect container logs: docker logs sentinel-ai-mock-target-1",
			"Restart target service or increase max_connections / connection pool limits",
		},
	}, nil
}
