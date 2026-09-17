// Package source enumerates things that expire, across providers. Every source
// is read-only — expiry-radar never needs write access, and that must stay true
// (ship read-only IAM policy examples; credential sprawl is the main risk).
package source

import (
	"context"
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

// CollectAll runs every source and merges the results. One source failing must
// not lose the others' findings — a broken AWS credential should not hide the
// cert expiring tomorrow — so errors are returned alongside the items.
func CollectAll(ctx context.Context, sources []Source) ([]Item, []error) {
	var items []Item
	var errs []error
	for _, s := range sources {
		got, err := s.Collect(ctx)
		// Sources deliberately return what they managed to read alongside the
		// error, so take both: one unreachable host must not discard the certs
		// its neighbours reported.
		items = append(items, got...)
		if err != nil {
			errs = append(errs, sourceError{name: s.Name(), err: err})
		}
	}
	return items, errs
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
