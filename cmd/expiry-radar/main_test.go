package main

import (
	"context"
	"strings"
	"testing"

	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

type stubSource struct{ name string }

func (s stubSource) Name() string { return s.name }

func (s stubSource) Collect(context.Context) ([]source.Item, error) { return nil, nil }

func names(sources []source.Source) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.Name())
	}
	return out
}

func configured() []source.Source {
	return []source.Source{
		stubSource{name: "tls:endpoint"},
		stubSource{name: "domain:rdap"},
		stubSource{name: "aws"},
		stubSource{name: "k8s"},
	}
}

// -only with nothing named is not a filter: the default is every configured
// source, so an unset flag must not narrow anything.
func TestNoOnlyRunsEveryConfiguredSource(t *testing.T) {
	got, err := pick(configured(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "tls:endpoint,domain:rdap,aws,k8s"; strings.Join(names(got), ",") != want {
		t.Errorf("want %q, got %q", want, strings.Join(names(got), ","))
	}
}

func TestOnlyRunsJustTheNamedSources(t *testing.T) {
	got, err := pick(configured(), []string{"aws", "domain:rdap"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "domain:rdap,aws"; strings.Join(names(got), ",") != want {
		t.Errorf("want %q, got %q", want, strings.Join(names(got), ","))
	}
}

// The mistake this catches is naming a real source that this config never
// built. Returning an empty run would report a clean estate for a source that
// was never asked anything — the failure this whole tool exists to prevent.
func TestOnlyNamingAnUnconfiguredSourceIsAnErrorNotAnEmptyRun(t *testing.T) {
	_, err := pick(configured(), []string{"cloudflare"})
	if err == nil {
		t.Fatal("want an error for a source this config did not build")
	}
	got := err.Error()
	if !strings.Contains(got, `"cloudflare"`) {
		t.Errorf("error should quote the name that did not match, got %q", got)
	}
	if !strings.Contains(got, "aws, domain:rdap, k8s, tls:endpoint") {
		t.Errorf("error should name what this config built, got %q", got)
	}
}

func TestOnlyReportsEveryNameThatDidNotMatch(t *testing.T) {
	_, err := pick(configured(), []string{"aws", "vault", "okta"})
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); !strings.Contains(got, `"okta", "vault"`) {
		t.Errorf("want both unmatched names, got %q", got)
	}
}

// Priority is rounded to two places before a stable sort, so ties are common
// and the configured order is visible in the report. -only must not reorder it.
func TestOnlyKeepsTheConfiguredOrderNotTheFlagOrder(t *testing.T) {
	got, err := pick(configured(), []string{"k8s", "tls:endpoint", "aws"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "tls:endpoint,aws,k8s"; strings.Join(names(got), ",") != want {
		t.Errorf("want configured order %q, got %q", want, strings.Join(names(got), ","))
	}
}

// With nothing configured at all, -only has nothing to narrow and the caller's
// "no sources configured" message is the useful one. Claiming an unknown source
// and then listing what this config built — an empty list — helps nobody.
func TestOnlyWithNothingConfiguredDefersToTheEmptyConfigMessage(t *testing.T) {
	got, err := pick(nil, []string{"aws"})
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want nothing back, got %d sources", len(got))
	}
}
