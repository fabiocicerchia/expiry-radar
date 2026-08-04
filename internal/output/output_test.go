package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func sample() []rank.Scored {
	return []rank.Scored{
		{
			Item: source.Item{
				Kind: source.KindTLSCert, Name: "checkout.example.com", Namespace: "payments",
				Expires: now.AddDate(0, 0, 12), Source: "k8s:secret",
				Labels: map[string]string{source.LabelHosts: "checkout.example.com"},
			},
			DaysLeft: 12, BlastRadius: 0.9, Priority: 0.88, Why: "base tls_cert, internet-facing, production",
		},
		{
			Item:     source.Item{Kind: source.KindDomain, Name: "example.com", Expires: now.AddDate(0, 0, 200), Source: "domain:rdap"},
			DaysLeft: 200, BlastRadius: 0.85, Priority: 0.38, Why: "base domain, internet-facing",
		},
	}
}

func render(t *testing.T, format Format) string {
	t.Helper()
	var b bytes.Buffer
	if err := RenderAt(&b, sample(), format, Options{Now: now}); err != nil {
		t.Fatalf("render %s: %v", format, err)
	}
	return b.String()
}

func TestTableShowsWhy(t *testing.T) {
	out := render(t, FormatTable)
	if !strings.Contains(out, "internet-facing") {
		t.Error("the table must carry the explanation, or nobody trusts the order")
	}
	if !strings.Contains(out, "12d") {
		t.Errorf("missing day count:\n%s", out)
	}
}

func TestTableFlagsExpiredInsteadOfSayingZero(t *testing.T) {
	items := sample()
	items[0].DaysLeft = -4
	var b bytes.Buffer
	if err := RenderAt(&b, items, FormatTable, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "EXPIRED 4d ago") {
		t.Errorf("expired items must be unmissable:\n%s", b.String())
	}
}

func TestJSONCountsExpired(t *testing.T) {
	items := sample()
	items[1].DaysLeft = -1
	var b bytes.Buffer
	if err := RenderAt(&b, items, FormatJSON, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Count   int `json:"count"`
		Expired int `json:"expired"`
		Items   []struct {
			Name    string `json:"name"`
			Expired bool   `json:"expired"`
			Why     string `json:"why"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.Count != 2 || got.Expired != 1 {
		t.Errorf("count=%d expired=%d, want 2 and 1", got.Count, got.Expired)
	}
	if !got.Items[1].Expired {
		t.Error("the expired item must be flagged")
	}
}

func TestICalIsWellFormed(t *testing.T) {
	out := render(t, FormatICal)
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n", "END:VCALENDAR\r\n", "BEGIN:VEVENT\r\n",
		"DTSTART;VALUE=DATE:20260813", // 12 days after 2026-08-01
		"DTEND;VALUE=DATE:20260814",   // all-day events end the next day
		"TRIGGER:-P30D",               // blast radius 0.9 earns the longest lead time
		"@expiry-radar",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "BEGIN:VEVENT") != 2 {
		t.Errorf("want one event per item, got %d", strings.Count(out, "BEGIN:VEVENT"))
	}
	for _, line := range strings.Split(out, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line exceeds the RFC 5545 limit (%d octets): %q", len(line), line)
		}
	}
}

func TestICalUIDIsStableAcrossRuns(t *testing.T) {
	first := render(t, FormatICal)
	var b bytes.Buffer
	if err := RenderAt(&b, sample(), FormatICal, Options{Now: now.Add(48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	uidOf := func(s string) string {
		for _, line := range strings.Split(s, "\r\n") {
			if strings.HasPrefix(line, "UID:") {
				return line
			}
		}
		return ""
	}
	if uidOf(first) != uidOf(b.String()) {
		t.Error("UIDs must be stable, or every refresh duplicates the calendar")
	}
}

func TestICalEscapesSeparators(t *testing.T) {
	items := sample()
	items[0].Item.Name = "a,b;c"
	var b bytes.Buffer
	if err := RenderAt(&b, items, FormatICal, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `a\,b\;c`) {
		t.Errorf("commas and semicolons must be escaped:\n%s", b.String())
	}
}

func TestPrometheusExposition(t *testing.T) {
	out := render(t, FormatPrometheus)
	for _, want := range []string{
		"# TYPE expiry_radar_seconds_left gauge",
		"# TYPE expiry_radar_blast_radius gauge",
		`expiry_radar_items{kind="domain"} 1`,
		`kind="tls_cert"`,
		`namespace="payments"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hosts=") {
		t.Error("high-cardinality labels must not reach the metrics")
	}
	// 12 days in seconds, relative to the injected clock.
	if !strings.Contains(out, " 1036800\n") {
		t.Errorf("seconds_left is not measured from the injected clock:\n%s", out)
	}
}

func TestPrometheusEscapesLabelValues(t *testing.T) {
	items := sample()
	items[0].Item.Name = `we"ird`
	var b bytes.Buffer
	if err := RenderAt(&b, items, FormatPrometheus, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `name="we\"ird"`) {
		t.Errorf("quotes in a label value must be escaped:\n%s", b.String())
	}
}

func TestHTMLReportIsSelfContainedAndFlagsTheDeadline(t *testing.T) {
	out := render(t, FormatHTML)
	for _, want := range []string{"payments/checkout.example.com", "internet-facing", "12d", "2026-08-13", `class="urgent"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// A report that pulls in a CDN is a report that renders blank in an email
	// client, offline, or behind a strict CSP.
	if strings.Contains(out, "http://") || strings.Contains(out, "https://") {
		t.Error("the report must reference no external resources")
	}
}

func TestHTMLEscapesNamesFromUntrustedSources(t *testing.T) {
	items := sample()
	items[0].Item.Name = `<script>alert(1)</script>` // certificates and registries are not trusted input
	var b bytes.Buffer
	if err := RenderAt(&b, items, FormatHTML, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<script>alert") {
		t.Errorf("name was not escaped:\n%s", b.String())
	}
}

// The whole point of grouping is that everything belonging to one domain is read
// together — the cert, the registration, and the wildcard in front of it.
func TestHTMLGroupsRowsUnderTheDomainTheyBelongTo(t *testing.T) {
	items := []rank.Scored{
		{Item: source.Item{Kind: source.KindDomain, Name: "example.com", Expires: now.AddDate(0, 0, 40), Source: "domain:rdap"}, DaysLeft: 40},
		{Item: source.Item{Kind: source.KindTLSCert, Name: "*.example.com", Expires: now.AddDate(0, 0, 9), Source: "tls:endpoint"}, DaysLeft: 9},
		{Item: source.Item{Kind: source.KindIntermediate, Name: "Issuing CA", Expires: now.AddDate(0, 0, 400), Source: "tls:chain"}, DaysLeft: 400},
		{Item: source.Item{Kind: source.KindSecret, Name: "db-password", Namespace: "payments", Expires: now.AddDate(0, 0, -2), Source: "vault"}, DaysLeft: -2},
	}
	groups, stats := groupRows(items)

	got := map[string]int{}
	for _, g := range groups {
		got[g.Name] = len(g.Rows)
	}
	want := map[string]int{"example.com": 2, "shared chain": 1, "payments": 1}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	for name, n := range want {
		if got[name] != n {
			t.Errorf("group %q has %d rows, want %d", name, got[name], n)
		}
	}
	// The group headline must be the worst deadline in it, not the first row.
	for _, g := range groups {
		if g.Name == "example.com" && g.Soonest != "9d" {
			t.Errorf("example.com soonest = %q, want 9d", g.Soonest)
		}
	}
	if stats.Total != 4 || stats.Expired != 1 || stats.In14 != 1 || stats.In30 != 1 {
		t.Errorf("stats = %+v, want 4 total / 1 expired / 1 in14 / 1 in30", stats)
	}
	if stats.NextName != "payments/db-password" {
		t.Errorf("soonest = %q, want the already-expired secret", stats.NextName)
	}
}

func TestHTMLShipsTheFilterControls(t *testing.T) {
	out := render(t, FormatHTML)
	for _, want := range []string{`id="q"`, `data-kind="domain"`, `data-kind="tls_cert"`, `data-sev=`, `data-text=`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing filter hook %q", want)
		}
	}
}

func TestHTMLSaysSoWhenThereIsNothing(t *testing.T) {
	var b bytes.Buffer
	if err := RenderAt(&b, nil, FormatHTML, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Nothing expiring") {
		t.Errorf("an empty report must say it is empty:\n%s", b.String())
	}
}

func TestUnknownFormatIsAnError(t *testing.T) {
	if err := RenderAt(&bytes.Buffer{}, sample(), Format("csv"), Options{Now: now}); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}
