package analyzer

import "context"

type ProbeFailure struct {
	TargetURL  string
	StatusCode int
	LatencyMs  int64
	RawBody    string
	ErrMessage string
}

type TriageReport struct {
	Summary     string   `json:"summary"`
	RootCause   string   `json:"root_cause"`
	Severity    string   `json:"severity"`
	Remediation []string `json:"remediation"`
}

type Analyzer interface {
	Analyze(ctx context.Context, failure ProbeFailure) (TriageReport, error)
}
