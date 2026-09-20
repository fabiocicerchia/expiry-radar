// Package source enumerates things that expire, across providers. Every source
// is read-only — expiry-radar never needs write access, and that must stay true
// (ship read-only IAM policy examples; credential sprawl is the main risk).
package source

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// Kind identifies what expires, for blast-radius weighting.
type Kind string

// The kinds of thing that expire. Each weights differently: an
// intermediate CA takes out an estate, a single leaf certificate one host.
const (
	KindTLSCert      Kind = "tls_cert"
	KindIntermediate Kind = "intermediate_ca" // nobody tracks these; they take out whole estates
	KindSecret       Kind = "secret"
	KindIAMKey       Kind = "iam_access_key"
	KindVaultLease   Kind = "vault_lease"
	KindDomain       Kind = "domain"
	// KindTrustAnchor is what everything else validates against: an admission
	// webhook CA, a service-mesh root, a federation signing certificate. It is
	// not an intermediate — nothing behind it fails gracefully. When one lapses
	// the control plane stops admitting, or every mTLS handshake in the cluster
	// stops, at once.
	KindTrustAnchor Kind = "trust_anchor"
)

// Item is one expiring thing, normalised across sources.
type Item struct {
	Kind      Kind
	Name      string
	Expires   time.Time
	Source    string            // e.g. "aws:acm", "k8s:ingress", "vault"
	Namespace string            // k8s namespace / AWS account / etc.
	Labels    map[string]string // ingress class, path, traffic hints — feed blast-radius ranking
}

// Well-known label keys. Sources populate whichever they can see; ranking reads
// them. An operator can set LabelBlastRadius on the resource itself to bypass
// inference entirely.
const (
	LabelIngressClass = "ingress.class"
	LabelPublic       = "public"  // "true" when reachable from the internet
	LabelTraffic      = "traffic" // requests/sec, as a decimal string
	LabelHosts        = "hosts"   // comma-separated hostnames a cert covers
	LabelIssuer       = "issuer"
	LabelSerial       = "serial"
	LabelBlastRadius  = "expiry-radar/blast-radius"
	// LabelInUse is "false" when the provider says nothing references this.
	LabelInUse = "in-use"
	// LabelEnvironment names the environment when the provider knows it, rather
	// than leaving ranking to infer one from the namespace and name.
	LabelEnvironment = "environment"
	// LabelRenewal says whether something else is already renewing this:
	// RenewalManaged when automation is demonstrably healthy, RenewalStuck when
	// it exists and is failing. Absent means nobody is renewing it but a person.
	LabelRenewal = "renewal"
)

// Values for LabelRenewal.
const (
	RenewalManaged = "managed"
	RenewalStuck   = "stuck"
)

// Source is a read-only inventory provider.
type Source interface {
	Name() string
	// Collect returns everything this source knows expires. It must never mutate.
	Collect(ctx context.Context) ([]Item, error)
}

// collectConcurrency bounds how many sources are in flight at once, the same
// way tlsProbeConcurrency bounds the endpoint probes inside one of them.
const collectConcurrency = 8

// CollectAll runs every source and merges the results. One source failing must
// not lose the others' findings — a broken AWS credential should not hide the
// cert expiring tomorrow — so errors are returned alongside the items.
//
// The sources run concurrently because the caller gives the whole run a single
// deadline. Collected one after another that deadline is a budget instead, and
// the first source can spend it on the last one's behalf: with two dozen
// network-bound sources, the ones at the end report nothing and the run looks
// like a clean estate.
func CollectAll(ctx context.Context, sources []Source) ([]Item, []error) {
	type result struct {
		items []Item
		err   error
	}
	results := make([]result, len(sources))
	sem := make(chan struct{}, collectConcurrency)
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Each goroutine writes only its own element, so the merge below
			// needs no lock and the order does not depend on who finished first.
			results[i].items, results[i].err = s.Collect(ctx)
		}(i, s)
	}
	wg.Wait()

	// Merged in configured order, not completion order: rank.Rank sorts stably
	// on a priority rounded to two places, so tied rows come out in the order
	// they were collected and two runs of one config must not disagree.
	var items []Item
	var errs []error
	for i, s := range sources {
		// Sources deliberately return what they managed to read alongside the
		// error, so take both: one unreachable host must not discard the certs
		// its neighbours reported.
		items = append(items, results[i].items...)
		if results[i].err != nil {
			errs = append(errs, sourceError{name: s.Name(), err: results[i].err})
		}
	}
	return items, errs
}

// collectUnit is one independently-collected unit of work inside a source: an
// AWS service, a Kubernetes resource class. Splitting a source into units is
// what makes the degradation rule — one denied permission must not lose the
// other units' findings — testable without an account or a cluster, which is
// the one property of these sources nobody could check before.
type collectUnit struct {
	Name    string
	Skipped bool
	Collect func() ([]Item, error)
}

// unitResult is what one unit returned. A unit that returned nothing is not the
// same as one that was denied, and not the same as one that was skipped:
// collapsing the three would let an account with no certificates read as an
// account whose ACM adapter works.
type unitResult struct {
	Name    string
	Skipped bool
	Items   int
	Err     error
}

func collectUnits(units []collectUnit) ([]Item, []unitResult, error) {
	var items []Item
	var warnings []string
	results := make([]unitResult, 0, len(units))
	for _, u := range units {
		if u.Skipped {
			results = append(results, unitResult{Name: u.Name, Skipped: true})
			continue
		}
		got, err := u.Collect()
		if err != nil {
			// The partial items are returned alongside the error, so a caller
			// that ignores the error is not silently throwing away what worked.
			warnings = append(warnings, u.Name+": "+err.Error())
			results = append(results, unitResult{Name: u.Name, Items: len(got), Err: err})
			items = append(items, got...)
			continue
		}
		results = append(results, unitResult{Name: u.Name, Items: len(got)})
		items = append(items, got...)
	}
	if len(warnings) > 0 {
		return items, results, errors.New(strings.Join(warnings, "; "))
	}
	return items, results, nil
}

type sourceError struct {
	name string
	err  error
}

func (e sourceError) Error() string { return e.name + ": " + e.err.Error() }
func (e sourceError) Unwrap() error { return e.err }

func label(m map[string]string, k, v string) map[string]string {
	if v == "" {
		return m
	}
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}
