package source

import (
	"context"
	"sort"
	"strings"
	"time"
)

// certRef is what a cert-manager Certificate says about the secret it manages.
//
// The Certificate does not supply the expiry — the secret it produced already
// did, and reporting both would be the same deadline twice. What it supplies is
// whether the renewal that was supposed to make that deadline a non-event is
// actually working, which is the failure that matters and the one nothing else
// can see.
type certRef struct {
	Name        string
	Namespace   string
	SecretName  string
	NotAfter    time.Time
	RenewalTime time.Time
	Ready       bool
	Issuer      string
	DNSNames    []string
}

type certificateItem struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		SecretName string   `json:"secretName"`
		DNSNames   []string `json:"dnsNames"`
		IssuerRef  struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"issuerRef"`
	} `json:"spec"`
	Status struct {
		NotAfter    string `json:"notAfter"`
		RenewalTime string `json:"renewalTime"`
		Conditions  []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

// certificates reads cert-manager Certificate CRs.
//
// It emits an item only for a Certificate whose secret we did not see: one that
// has never been issued, or one in a namespace we cannot read secrets in. Those
// are real findings — a certificate that was supposed to exist and does not.
// For the rest, the Certificate's contribution is the renewal evidence applied
// by annotateRenewal, not a second row at the same date.
func (s *K8sSource) certificates(ctx context.Context, api *k8sAPI, st *k8sState) ([]Item, error) {
	err := listEach(ctx, api, s.paths("/apis/cert-manager.io/v1", "certificates"), func(c certificateItem) {
		ref := certRef{
			Name:       c.Metadata.Name,
			Namespace:  c.Metadata.Namespace,
			SecretName: c.Spec.SecretName,
			DNSNames:   c.Spec.DNSNames,
			Issuer:     c.Spec.IssuerRef.Name,
		}
		ref.NotAfter = parseK8sTime(c.Status.NotAfter)
		ref.RenewalTime = parseK8sTime(c.Status.RenewalTime)
		for _, cond := range c.Status.Conditions {
			if cond.Type == "Ready" {
				ref.Ready = cond.Status == "True"
			}
		}
		if ref.SecretName == "" {
			return
		}
		st.certs[c.Metadata.Namespace+"/"+ref.SecretName] = ref
	})
	// Sorted, not map order: two items at the same deadline must not swap places
	// between runs of the same unchanged cluster.
	keys := make([]string, 0, len(st.certs))
	for key := range st.certs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	now := s.clock()
	var items []Item
	for _, key := range keys {
		if it, ok := certificateItemFor(key, st, now); ok {
			items = append(items, it)
		}
	}
	return items, err
}

// certificateItemFor decides what, if anything, a Certificate is worth
// reporting on its own account. The four cases are genuinely different, and
// collapsing them is what made this collector claim things it had not checked.
func certificateItemFor(key string, st *k8sState, now time.Time) (Item, bool) {
	ref := st.certs[key]
	switch st.secrets[key] {
	case secretParsed:
		// The secret is reported with its own date; this Certificate's
		// contribution is the renewal evidence annotateRenewal folds in.
		return Item{}, false

	case secretUnreadable:
		// Something is there but we cannot date it, so the Certificate is now
		// the only readable source of truth for this certificate.
		if ref.NotAfter.IsZero() {
			return Item{}, false
		}
		return certManagerItem(key, ref, ref.NotAfter, now, secretUnreadableLabel), true
	}

	if !st.readSecretsIn(ref.Namespace) {
		// We never read secrets here, so we have not earned the word "missing".
		// Report the Certificate on its own date and claim nothing else.
		if ref.NotAfter.IsZero() {
			return Item{}, false
		}
		return certManagerItem(key, ref, ref.NotAfter, now, ""), true // secrets unread: claim nothing
	}

	// We looked, and it is not there. Nothing issued at all means the deadline
	// is now, because the thing that was supposed to exist does not.
	expires := ref.NotAfter
	if expires.IsZero() {
		expires = now
	}
	return certManagerItem(key, ref, expires, now, secretMissingLabel), true
}

// Values for the secret-state label.
const (
	secretUnreadableLabel = "unreadable"
	secretMissingLabel    = "missing"
)

// renewalFor reconciles what cert-manager claims with what we can see.
//
// "managed" earns a 0.25 de-rank, so it may only be claimed when nothing
// contradicts it. A Certificate can report Ready with its renewal comfortably
// ahead while the secret it was supposed to produce has been deleted — taking
// blast radius off that is the exact inversion addRenewal exists to prevent.
// A secret we could not parse supports neither claim, so it gets no label.
func renewalFor(ref certRef, now time.Time, secretState string) string {
	switch secretState {
	case secretMissingLabel:
		// The automation did not produce the secret. That is the failure.
		return RenewalStuck
	case secretUnreadableLabel:
		return ""
	}
	return renewalState(ref, now)
}

func certManagerItem(key string, ref certRef, expires, now time.Time, secretState string) Item {
	labels := map[string]string{}
	labels = label(labels, LabelHosts, strings.Join(ref.DNSNames, ","))
	labels = label(labels, LabelIssuer, ref.Issuer)
	labels = label(labels, "cert-manager", ref.Name)
	labels = label(labels, "secret", ref.SecretName)
	labels = label(labels, "secret-state", secretState)
	labels = label(labels, LabelRenewal, renewalFor(ref, now, secretState))
	return Item{
		Kind:      KindTLSCert,
		Name:      key,
		Expires:   expires,
		Source:    "k8s:cert-manager",
		Namespace: ref.Namespace,
		Labels:    labels,
	}
}

// annotateRenewal folds the renewal evidence into the secrets already reported.
//
// Ranking reads it as evidence about whether the deadline is real, not about
// how much it would hurt: a Certificate that is Ready with its renewal still
// ahead of it is a date nobody has to act on, and de-ranking it is what lets
// the ones nobody is renewing rise.
func (st *k8sState) annotateRenewal(items []Item, now time.Time) {
	for i := range items {
		if items[i].Source != "k8s:secret" {
			continue
		}
		ref, ok := st.certs[items[i].Name]
		if !ok {
			continue
		}
		items[i].Labels = label(items[i].Labels, "cert-manager", ref.Name)
		items[i].Labels = label(items[i].Labels, LabelRenewal, renewalState(ref, now))
		if !ref.RenewalTime.IsZero() {
			items[i].Labels = label(items[i].Labels, "renew-at", ref.RenewalTime.Format(time.RFC3339))
		}
	}
}

// renewalState is deliberately three-valued. "managed" is a claim that renewal
// is demonstrably working and earns the de-rank; "stuck" is a claim that it is
// demonstrably not. A Certificate that is Ready but has told us nothing about
// when it renews supports neither claim, and gets no label rather than a guess.
func renewalState(ref certRef, now time.Time) string {
	if !ref.Ready {
		return RenewalStuck
	}
	if ref.RenewalTime.IsZero() {
		return ""
	}
	if ref.RenewalTime.Before(now) {
		return RenewalStuck
	}
	return RenewalManaged
}

func parseK8sTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
