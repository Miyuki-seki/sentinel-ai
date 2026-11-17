package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init sqlite store: %v", err)
	}
	defer st.Close()

	// 1. probe_logs のクリーンアップ検証
	now := time.Now().UTC()
	// 古いログ (2日前) を手動挿入
	oldTime := now.Add(-48 * time.Hour)
	recentTime := now.Add(-2 * time.Hour)

	_, err = st.db.Exec(`INSERT INTO probe_logs (target_url, status_code, latency_ms, healthy, created_at) VALUES (?, ?, ?, ?, ?)`,
		"http://target/health", 200, 10, true, oldTime)
	if err != nil {
		t.Fatalf("insert old probe: %v", err)
	}
	_, err = st.db.Exec(`INSERT INTO probe_logs (target_url, status_code, latency_ms, healthy, created_at) VALUES (?, ?, ?, ?, ?)`,
		"http://target/health", 200, 15, true, recentTime)
	if err != nil {
		t.Fatalf("insert recent probe: %v", err)
	}

	// 24時間以前のログをクリーンアップ
	deleted, err := st.CleanupProbeLogs(24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup probe logs: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 probe log deleted, got %d", deleted)
	}

	var remainingProbes int
	_ = st.db.QueryRow(`SELECT count(*) FROM probe_logs`).Scan(&remainingProbes)
	if remainingProbes != 1 {
		t.Errorf("expected 1 remaining probe log, got %d", remainingProbes)
	}

	// 2. incidents のクリーンアップ検証
	// 40日前に解決済みのインシデント
	resolvedOld := now.Add(-40 * 24 * time.Hour)
	_, err = st.db.Exec(`INSERT INTO incidents (target_url, status, status_code, error_sample, summary, root_cause, severity, remediation_json, created_at, resolved_at)
		VALUES (?, 'RESOLVED', 500, '', 'Old summary', 'cause', 'CRITICAL', '[]', ?, ?)`,
		"http://target/health", resolvedOld.Add(-1*time.Hour), resolvedOld)
	if err != nil {
		t.Fatalf("insert old resolved incident: %v", err)
	}

	// 5日前に解決済みのインシデント
	resolvedRecent := now.Add(-5 * 24 * time.Hour)
	_, err = st.db.Exec(`INSERT INTO incidents (target_url, status, status_code, error_sample, summary, root_cause, severity, remediation_json, created_at, resolved_at)
		VALUES (?, 'RESOLVED', 500, '', 'Recent summary', 'cause', 'CRITICAL', '[]', ?, ?)`,
		"http://target/health", resolvedRecent.Add(-1*time.Hour), resolvedRecent)
	if err != nil {
		t.Fatalf("insert recent resolved incident: %v", err)
	}

	// 40日前に作成されたがまだ OPEN なインシデント（削除されてはならない）
	_, err = st.db.Exec(`INSERT INTO incidents (target_url, status, status_code, error_sample, summary, root_cause, severity, remediation_json, created_at, resolved_at)
		VALUES (?, 'OPEN', 500, '', 'Open summary', 'cause', 'CRITICAL', '[]', ?, NULL)`,
		"http://target/health", resolvedOld)
	if err != nil {
		t.Fatalf("insert open incident: %v", err)
	}

	// 30日以上の解決済みインシデントをクリーンアップ
	deletedInc, err := st.CleanupResolvedIncidents(30 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup incidents: %v", err)
	}
	if deletedInc != 1 {
		t.Errorf("expected 1 incident deleted, got %d", deletedInc)
	}

	var remainingIncidents int
	_ = st.db.QueryRow(`SELECT count(*) FROM incidents`).Scan(&remainingIncidents)
	if remainingIncidents != 2 {
		t.Errorf("expected 2 remaining incidents (1 recent resolved + 1 open), got %d", remainingIncidents)
	}
}
