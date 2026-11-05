package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These drive run() the way the binary does — config file in, report and exit
// code out — so the config → collect → rank → render → gate path is covered as
// one journey. Manual items only: no network, no credentials.

// writeConfig puts two certs expiring on the same day in a temp config: one in
// prod, one in staging. Dates are relative to now because run() ranks against
// the wall clock.
func writeConfig(t *testing.T) string {
	t.Helper()
	expires := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02")
	cfg := `{"manual": [
		{"name": "staging-dashboard", "kind": "tls_cert", "expires": "` + expires + `", "namespace": "staging"},
		{"name": "payments-api", "kind": "tls_cert", "expires": "` + expires + `", "namespace": "prod"}
	]}`
	path := filepath.Join(t.TempDir(), "expiry-radar.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The product in one assertion: same expiry, but the prod cert ranks first.
func TestE2EJSONReportRanksProdAboveStaging(t *testing.T) {
	var stdout bytes.Buffer
	code, err := run(context.Background(), []string{"-config", writeConfig(t), "-format", "json"}, &stdout)
	if err != nil || code != exitClean {
		t.Fatalf("want exit %d and no error, got %d, %v", exitClean, code, err)
	}
	var report struct {
		Count int `json:"count"`
		Items []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	if report.Count != 2 || len(report.Items) != 2 {
		t.Fatalf("want 2 items, got %d: %s", report.Count, stdout.String())
	}
	if report.Items[0].Name != "payments-api" || report.Items[1].Name != "staging-dashboard" {
		t.Errorf("want payments-api ranked above staging-dashboard, got %q then %q",
			report.Items[0].Name, report.Items[1].Name)
	}
	if report.Items[0].Source != "manual" {
		t.Errorf("want source manual, got %q", report.Items[0].Source)
	}
}

// The CI gate: -fail-within exits 1 and names the top-ranked item in the window.
func TestE2EFailWithinExitsBreachedNamingTheTopItem(t *testing.T) {
	var stdout bytes.Buffer
	code, err := run(context.Background(), []string{"-config", writeConfig(t), "-fail-within", "30"}, &stdout)
	if code != exitBreached {
		t.Fatalf("want exit %d, got %d (err %v)", exitBreached, code, err)
	}
	if err == nil || !strings.Contains(err.Error(), "payments-api") {
		t.Errorf("want the breach to name payments-api, got %v", err)
	}
	if !strings.Contains(stdout.String(), "payments-api") {
		t.Errorf("the report must still be written on a breach, got %q", stdout.String())
	}
}

// -out writes the report to disk and leaves stdout empty; -within narrows it.
func TestE2EOutWritesTheReportToAFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer
	code, err := run(context.Background(),
		[]string{"-config", writeConfig(t), "-format", "json", "-within", "5", "-out", out}, &stdout)
	if err != nil || code != exitClean {
		t.Fatalf("want exit %d and no error, got %d, %v", exitClean, code, err)
	}
	if stdout.Len() != 0 {
		t.Errorf("want nothing on stdout with -out, got %q", stdout.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"count": 0`) {
		t.Errorf("-within 5 should drop items 10 days out, got %s", data)
	}
}

// No config file and no flags is a usage error, not a clean empty report.
func TestE2ENoSourcesIsAUsageError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, err := run(context.Background(), []string{"-config", missing}, &bytes.Buffer{})
	if code != exitUsage || err == nil || !strings.Contains(err.Error(), "no sources configured") {
		t.Errorf("want exit %d with the no-sources message, got %d, %v", exitUsage, code, err)
	}
}
