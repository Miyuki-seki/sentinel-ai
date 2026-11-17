package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Incident struct {
	ID          int64      `json:"id"`
	TargetURL   string     `json:"target_url"`
	Status      string     `json:"status"` // "OPEN" or "RESOLVED"
	StatusCode  int        `json:"status_code"`
	ErrorSample string     `json:"error_sample"`
	Summary     string     `json:"summary"`
	RootCause   string     `json:"root_cause"`
	Severity    string     `json:"severity"` // "CRITICAL" or "WARNING"
	Remediation []string   `json:"remediation"`
	CreatedAt   time.Time  `json:"created_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	pragmas := `
	PRAGMA journal_mode=WAL;
	PRAGMA busy_timeout=5000;
	`
	if _, err := db.Exec(pragmas); err != nil {
		return fmt.Errorf("set pragmas: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS probe_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_url TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL,
		healthy BOOLEAN NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_url TEXT NOT NULL,
		status TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		error_sample TEXT NOT NULL,
		summary TEXT NOT NULL,
		root_cause TEXT NOT NULL,
		severity TEXT NOT NULL,
		remediation_json TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		resolved_at DATETIME
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) RecordProbe(targetURL string, statusCode int, latencyMs int64, healthy bool) error {
	_, err := s.db.Exec(
		`INSERT INTO probe_logs (target_url, status_code, latency_ms, healthy, created_at) VALUES (?, ?, ?, ?, ?)`,
		targetURL, statusCode, latencyMs, healthy, time.Now().UTC(),
	)
	return err
}

func (s *SQLiteStore) HasOpenIncident(targetURL string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM incidents WHERE target_url = ? AND status = 'OPEN'`,
		targetURL,
	).Scan(&count)
	return count > 0, err
}

func (s *SQLiteStore) CreateIncident(inc Incident) (int64, error) {
	remBytes, err := json.Marshal(inc.Remediation)
	if err != nil {
		return 0, fmt.Errorf("marshal remediation: %w", err)
	}

	res, err := s.db.Exec(`
		INSERT INTO incidents (
			target_url, status, status_code, error_sample,
			summary, root_cause, severity, remediation_json, created_at
		) VALUES (?, 'OPEN', ?, ?, ?, ?, ?, ?, ?)`,
		inc.TargetURL, inc.StatusCode, inc.ErrorSample,
		inc.Summary, inc.RootCause, inc.Severity, string(remBytes), time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) ResolveOpenIncidents(targetURL string) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE incidents SET status = 'RESOLVED', resolved_at = ? WHERE target_url = ? AND status = 'OPEN'`,
		time.Now().UTC(), targetURL,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteStore) ListRecentIncidents(limit int) ([]Incident, error) {
	rows, err := s.db.Query(`
		SELECT id, target_url, status, status_code, error_sample,
		       summary, root_cause, severity, remediation_json, created_at, resolved_at
		FROM incidents
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Incident
	for rows.Next() {
		var inc Incident
		var remJSON string
		var resolved sql.NullTime
		if err := rows.Scan(
			&inc.ID, &inc.TargetURL, &inc.Status, &inc.StatusCode, &inc.ErrorSample,
			&inc.Summary, &inc.RootCause, &inc.Severity, &remJSON, &inc.CreatedAt, &resolved,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(remJSON), &inc.Remediation)
		if resolved.Valid {
			t := resolved.Time
			inc.ResolvedAt = &t
		}
		list = append(list, inc)
	}
	return list, rows.Err()
}
