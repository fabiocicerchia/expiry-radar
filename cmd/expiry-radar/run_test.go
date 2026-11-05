package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These drive run() the way the binary does — a config file on disk, flags,
// a report on stdout, an exit code — using only manual items, so no network.
// Dates are decades away from any plausible now, so the outcome does not
// depend on when the suite runs.
const e2eConfig = `{
  "manual": [
    {"name": "staging-dashboard", "kind": "tls_cert", "expires": "2999-01-01", "namespace": "staging"},
    {"name": "payments-ca", "kind": "intermediate_ca", "expires": "2000-01-01", "namespace": "payments"}
  ]
}`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "expiry-radar.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunJSONReportRanksTheExpiredItemFirst(t *testing.T) {
	var stdout bytes.Buffer
	code, err := run(context.Background(), []string{"-config", writeConfig(t, e2eConfig), "-format", "json"}, &stdout)
	if err != nil || code != exitClean {
		t.Fatalf("want exit %d and no error, got %d, %v", exitClean, code, err)
	}
	var report struct {
		Count   int `json:"count"`
		Expired int `json:"expired"`
		Items   []struct {
			Name    string `json:"name"`
			Source  string `json:"source"`
			Expired bool   `json:"expired"`
		} `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not the JSON report: %v\n%s", err, stdout.String())
	}
	if report.Count != 2 || report.Expired != 1 || len(report.Items) != 2 {
		t.Fatalf("want 2 items with 1 expired, got %+v", report)
	}
	if first := report.Items[0]; first.Name != "payments-ca" || !first.Expired || first.Source != "manual" {
		t.Errorf("want the expired payments CA ranked first, got %+v", first)
	}
}

// -fail-within is the CI gate: crossing it is exit 1 with the offending item
// named, and the report is still written.
func TestRunFailWithinExitsOneAndNamesTheItem(t *testing.T) {
	var stdout bytes.Buffer
	code, err := run(context.Background(), []string{"-config", writeConfig(t, e2eConfig), "-fail-within", "30"}, &stdout)
	if code != exitBreached || err == nil || !strings.Contains(err.Error(), "payments-ca") {
		t.Fatalf("want exit %d naming payments-ca, got %d, %v", exitBreached, code, err)
	}
	if !strings.Contains(stdout.String(), "staging-dashboard") {
		t.Errorf("the report should still list every item, got:\n%s", stdout.String())
	}
}

func TestRunWithinFiltersAndOutWritesTheFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer
	code, err := run(context.Background(),
		[]string{"-config", writeConfig(t, e2eConfig), "-format", "json", "-within", "30", "-out", out}, &stdout)
	if err != nil || code != exitClean {
		t.Fatalf("want exit %d, got %d, %v", exitClean, code, err)
	}
	if stdout.Len() != 0 {
		t.Errorf("-out should leave stdout empty, got %q", stdout.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, "payments-ca") || strings.Contains(got, "staging-dashboard") {
		t.Errorf("-within 30 should keep only the expired item, got:\n%s", got)
	}
}

func TestRunWithNoSourcesIsAUsageError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	code, err := run(context.Background(), []string{"-config", missing}, &bytes.Buffer{})
	if code != exitUsage || err == nil || !strings.Contains(err.Error(), "no sources configured") {
		t.Fatalf("want exit %d with the empty-config message, got %d, %v", exitUsage, code, err)
	}
}

// A typo in a manual kind ranks plausibly and wrongly if it loads, so the
// whole run must refuse it.
func TestRunRejectsAnUnknownManualKind(t *testing.T) {
	cfg := writeConfig(t, `{"manual": [{"name": "x", "kind": "tls-cert", "expires": "2999-01-01"}]}`)
	code, err := run(context.Background(), []string{"-config", cfg}, &bytes.Buffer{})
	if code != exitUsage || err == nil {
		t.Fatalf("want exit %d with an error, got %d, %v", exitUsage, code, err)
	}
}
