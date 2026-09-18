package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// K8sSource reads everything the API server knows expires: TLS secrets and the
// ingresses that reference them, cert-manager Certificates, the CA bundles the
// admission webhooks and the aggregation layer validate against, and service
// mesh trust anchors.
//
// It talks to the API server directly over HTTPS with net/http rather than
// pulling in client-go: a handful of GETs against stable, versioned endpoints
// do not justify that dependency tree. Every verb used is `list` or `get` —
// see docs/rbac-readonly.yaml for the exact Role and ClusterRole.
//
// Certificate expiry comes from the secret's own tls.crt, not from the ingress:
// the ingress only says which secret is public and under which class, which is
// what the blast-radius ranking needs. A cert-manager Certificate does not
// supply a second date for the same certificate either — it supplies whether
// the renewal that was supposed to make the date a non-event is working.
//
// Not covered, deliberately: kubeadm control-plane certificates. admin.conf,
// the kubelet client certificate and the etcd peer certificates live in
// /etc/kubernetes/pki on the nodes, not behind the API, and reading them needs
// an agent on every node — a different product shape, and a privilege level
// this tool has promised not to need. The API server's own serving certificate
// is already reachable: point a tls endpoint at :6443.
type K8sSource struct {
	// Server is the API server URL. Empty means in-cluster
	// (https://kubernetes.default.svc), which is the normal deployment.
	Server string
	// Token authenticates the request; empty falls back to the mounted service
	// account token. A `kubectl proxy` on localhost needs neither.
	Token      string
	CAFile     string
	Namespaces []string // empty = all namespaces
	Insecure   bool
	Timeout    time.Duration

	// SkipSecrets turns off the TLS-secret collector. It is a skip rather than
	// an opt-in because reading TLS secrets is what this source has always
	// done, and an existing deployment already has the permission.
	SkipSecrets bool

	// The rest are opt-in, and deliberately so. Each needs a permission the
	// previous release did not ask for, so defaulting them on would take an
	// install that was exiting 0 and start it exiting 3 on upgrade — a CI gate
	// going red because of a config change nobody was prompted to make. A
	// permission the operator never granted is not a partial result; it is a
	// collector that is not configured.
	//
	// CertManager reads Certificate CRs: `list` on cert-manager.io.
	CertManager bool
	// TrustAnchors reads the admission webhook and APIService CA bundles and
	// the mesh trust roots. All cluster-scoped, so it needs a ClusterRole.
	TrustAnchors bool
	// MeshSigningSecrets additionally reads the mesh objects that hold the
	// signing key beside the certificate. It needs `get` on Secrets containing
	// cluster-wide mTLS CA private keys — read docs/rbac-readonly.yaml before
	// setting it. Requires TrustAnchors.
	MeshSigningSecrets bool

	// MeshAnchors are read in addition to the built-in Linkerd and Istio
	// locations, never instead of them — see anchors().
	MeshAnchors []MeshAnchor

	// now exists so the renewal check has a fixed clock in tests.
	now func() time.Time
}

const (
	inClusterServer = "https://kubernetes.default.svc"
	// Well-known path, not a credential
	inClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec
	inClusterCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// errNotFound distinguishes "this object is not installed here" from "this
// request failed". A cluster without Linkerd is not a cluster with a problem.
var errNotFound = errors.New("not found")

// Name identifies this source in an item's Source field.
func (s *K8sSource) Name() string { return "k8s" }

func (s *K8sSource) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Collect reads everything expiring that this cluster will show us.
//
// Read-only, like every source: expiry-radar never needs write access.
func (s *K8sSource) Collect(ctx context.Context) ([]Item, error) {
	api, err := s.client()
	if err != nil {
		// No client means nothing is readable at all, which is the one case
		// where there are no partial findings to keep.
		return nil, err
	}
	st := &k8sState{
		certs:    map[string]certRef{},
		secrets:  map[string]secretState{},
		secretNS: map[string]bool{},
		anchors:  newCAAccumulator(),
	}
	// The ingress list exists only to give a secret its hosts, class and public
	// flag, so there is nothing to fetch — and no 403 worth warning about —
	// when secrets are not being collected at all.
	if !s.SkipSecrets {
		st.refs, st.ingressErr = s.ingressRefs(ctx, api)
	}

	items, _, err := collectResources(s.resources(ctx, api, st))
	// Drained once, after every trust-anchor class has run, so a CA that two
	// classes both pin carries what both of them knew about it.
	items = append(items, st.anchors.items()...)
	// Renewal evidence is applied after the fact rather than during collection:
	// the secrets and the Certificates that manage them are separate reads, and
	// neither should have to run first for the other to be useful.
	st.annotateRenewal(items, s.clock())
	return items, err
}

// k8sState is what the resource collectors share: the ingress context a secret
// is ranked by, which secrets were actually seen, and the cert-manager
// Certificates that claim to be renewing them.
type k8sState struct {
	refs       map[string]ingressRef
	ingressErr error
	certs      map[string]certRef // keyed ns/secretName
	// secrets holds only the secrets we actually saw, and whether we could read
	// a certificate out of each. Absence from this map means one of two very
	// different things, which is what secretNS/secretsAllNS disambiguate: the
	// secret is not there, or we never looked.
	secrets      map[string]secretState // keyed ns/secretName
	secretNS     map[string]bool        // namespaces whose secret list we read
	secretsAllNS bool                   // the cluster-wide secret list succeeded
	// anchors is shared by all three trust-anchor collectors, so a CA that a
	// webhook and an APIService both pin is one finding rather than two.
	anchors *caAccumulator
}

// secretState is what we managed to get out of a TLS secret. "Unreadable" is
// not "absent": something is there, it is just not something we can date.
type secretState int

const (
	secretParsed secretState = iota + 1
	secretUnreadable
)

// readSecretsIn says whether the secret list covering this namespace was
// actually read. Nothing may infer that a secret is missing without it.
func (st *k8sState) readSecretsIn(namespace string) bool {
	return st.secretsAllNS || st.secretNS[namespace]
}

// k8sResource is one class of thing this source reads, named so a failure can
// say which. The loop is collectUnits in source.go, shared with the AWS source.
type k8sResource = collectUnit

// resourceResult is what one resource class returned.
type resourceResult = unitResult

func (s *K8sSource) resources(ctx context.Context, api *k8sAPI, st *k8sState) []k8sResource {
	return []k8sResource{
		// The ingress list produces no items of its own — it only supplies the
		// hosts, class and public flag a secret is ranked by. Reporting its
		// failure through the same channel keeps one error path instead of a
		// special case, and losing it costs ranking evidence, not findings.
		{"ingresses", s.SkipSecrets, func() ([]Item, error) { return nil, st.ingressErr }},
		{"secrets", s.SkipSecrets, func() ([]Item, error) { return s.tlsSecrets(ctx, api, st) }},
		{"certificates", !s.CertManager, func() ([]Item, error) { return s.certificates(ctx, api, st) }},
		{"webhooks", !s.TrustAnchors, func() ([]Item, error) { return s.webhookCAs(ctx, api, st) }},
		{"apiservices", !s.TrustAnchors, func() ([]Item, error) { return s.apiServiceCAs(ctx, api, st) }},
		{"mesh", !s.TrustAnchors, func() ([]Item, error) { return s.meshAnchors(ctx, api, st) }},
	}
}

func collectResources(rs []k8sResource) ([]Item, []resourceResult, error) {
	return collectUnits(rs)
}

// k8sAPI is the authenticated read side of the API server: one client, one base
// URL, one bearer token, and the single GET everything here is built from.
type k8sAPI struct {
	client *http.Client
	base   string
	token  string
}

func (a *k8sAPI) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path, nil)
	if err != nil {
		return err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	//nolint:errcheck // the body is read or abandoned either way; a failed
	// close only costs a pooled connection.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf(
			"GET %s: forbidden — expiry-radar needs read access to this resource (see docs/rbac-readonly.yaml)",
			path)
	}
	// A 404 on a named object means it is not installed, and on a list endpoint
	// means the CRD is not installed. Both are answers, not failures.
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("GET %s: %w", path, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// listEach GETs every path and hands each item to fn, keeping going when one
// path fails. One forbidden namespace must not lose the other namespaces'
// findings — the same rule collectResources enforces one level up, applied
// where the namespace fan-out actually happens.
//
// A 404 is a failure here, and that is the whole point. Only the endpoints
// where absence is a genuine answer about the cluster — a CRD that is not
// installed — may swallow one, and those call listEachIfPresent instead.
// Everywhere else a 404 means the request went somewhere unintended, usually a
// k8s.server carrying a path prefix, and swallowing it turns a misconfigured
// run into an empty report that exits 0. A report that quietly lost a source
// reads exactly like a clean estate, which is the failure this tool exists to
// prevent.
func listEach[T any](ctx context.Context, api *k8sAPI, paths []string, fn func(T)) error {
	return listPaths(ctx, api, paths, fn, false)
}

// listEachIfPresent treats a 404 as "this is not installed here" rather than as
// an error. Only for optional API groups.
func listEachIfPresent[T any](ctx context.Context, api *k8sAPI, paths []string, fn func(T)) error {
	return listPaths(ctx, api, paths, fn, true)
}

func listPaths[T any](ctx context.Context, api *k8sAPI, paths []string, fn func(T), optional bool) error {
	var warnings []string
	for _, p := range paths {
		var list struct {
			Items []T `json:"items"`
		}
		if err := api.get(ctx, p, &list); err != nil {
			if !optional || !errors.Is(err, errNotFound) {
				warnings = append(warnings, err.Error())
			}
			continue
		}
		for _, it := range list.Items {
			fn(it)
		}
	}
	if len(warnings) > 0 {
		return fmt.Errorf("%s", strings.Join(warnings, "; "))
	}
	return nil
}

// ingressRef records what an ingress says about a secret it uses.
type ingressRef struct {
	Hosts   []string
	Class   string
	Ingress string
}

type ingressItem struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		IngressClassName string `json:"ingressClassName"`
		TLS              []struct {
			Hosts      []string `json:"hosts"`
			SecretName string   `json:"secretName"`
		} `json:"tls"`
	} `json:"spec"`
}

func (s *K8sSource) ingressRefs(ctx context.Context, api *k8sAPI) (map[string]ingressRef, error) {
	refs := map[string]ingressRef{}
	err := listEach(ctx, api, s.paths("/apis/networking.k8s.io/v1", "ingresses"), func(ing ingressItem) {
		class := ing.Spec.IngressClassName
		if class == "" {
			class = ing.Metadata.Annotations["kubernetes.io/ingress.class"]
		}
		for _, t := range ing.Spec.TLS {
			if t.SecretName == "" {
				continue
			}
			key := ing.Metadata.Namespace + "/" + t.SecretName
			ref := refs[key]
			ref.Hosts = appendUnique(ref.Hosts, t.Hosts...)
			if ref.Class == "" {
				ref.Class = class
			}
			if ref.Ingress == "" {
				ref.Ingress = ing.Metadata.Name
			}
			refs[key] = ref
		}
	})
	return refs, err
}

type secretItem struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string][]byte `json:"data"` // encoding/json base64-decodes []byte for us
}

func (s *K8sSource) tlsSecrets(ctx context.Context, api *k8sAPI, st *k8sState) ([]Item, error) {
	var items []Item
	var warnings []string

	for _, sc := range s.scopes("/api/v1", "secrets?fieldSelector=type%3Dkubernetes.io%2Ftls") {
		var list struct {
			Items []secretItem `json:"items"`
		}
		if err := api.get(ctx, sc.Path, &list); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		// Only now may anything conclude that a secret in this namespace is
		// missing rather than merely unread.
		if sc.Namespace == "" {
			st.secretsAllNS = true
		} else {
			st.secretNS[sc.Namespace] = true
		}

		for _, sec := range list.Items {
			key := sec.Metadata.Namespace + "/" + sec.Metadata.Name

			cert, err := firstCert(sec.Data["tls.crt"])
			if err != nil {
				// A malformed secret is a different problem and must not stop
				// the scan — but it is still a secret that exists, and saying
				// so is what stops the cert-manager collector calling it
				// never-issued.
				st.secrets[key] = secretUnreadable
				continue
			}
			st.secrets[key] = secretParsed
			ref := st.refs[key]

			labels := map[string]string{}
			labels = label(labels, LabelHosts, strings.Join(hostsOf(ref, cert), ","))
			labels = label(labels, LabelIssuer, cert.Issuer.CommonName)
			labels = label(labels, LabelIngressClass, ref.Class)
			if ref.Ingress != "" {
				labels["ingress"] = ref.Ingress
				labels[LabelPublic] = "true" // referenced by an ingress = user-facing
			}

			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      key,
				Expires:   cert.NotAfter,
				Source:    "k8s:secret",
				Namespace: sec.Metadata.Namespace,
				Labels:    labels,
			})
		}
	}
	return items, joinErrs(warnings)
}

func hostsOf(ref ingressRef, cert *x509.Certificate) []string {
	if len(ref.Hosts) > 0 {
		return ref.Hosts
	}
	return cert.DNSNames
}

// k8sScope is one list request and the namespace it covers. An empty Namespace
// means the request covers every namespace at once.
type k8sScope struct {
	Path      string
	Namespace string
}

// scopes expands the configured namespaces into API paths. Cluster-wide listing
// needs a ClusterRole; per-namespace listing works with a plain Role, which is
// the posture most security teams will actually approve.
//
// The namespace travels with the path because a collector has to know which
// namespaces it actually managed to read before it can say anything is missing
// from one.
func (s *K8sSource) scopes(apiRoot, resource string) []k8sScope {
	if len(s.Namespaces) == 0 {
		return []k8sScope{{Path: apiRoot + "/" + resource}}
	}
	out := make([]k8sScope, 0, len(s.Namespaces))
	for _, ns := range s.Namespaces {
		out = append(out, k8sScope{
			Path:      apiRoot + "/namespaces/" + url.PathEscape(ns) + "/" + resource,
			Namespace: ns,
		})
	}
	return out
}

func (s *K8sSource) paths(apiRoot, resource string) []string {
	sc := s.scopes(apiRoot, resource)
	out := make([]string, 0, len(sc))
	for _, c := range sc {
		out = append(out, c.Path)
	}
	return out
}

func (s *K8sSource) client() (*k8sAPI, error) {
	base := strings.TrimSuffix(s.Server, "/")
	token := s.Token
	caFile := s.CAFile

	if base == "" {
		base = inClusterServer
		if token == "" {
			b, err := os.ReadFile(inClusterTokenFile)
			if err != nil {
				return nil, fmt.Errorf("no --kube-server given and no in-cluster token: %w", err)
			}
			token = strings.TrimSpace(string(b))
		}
		if caFile == "" {
			caFile = inClusterCAFile
		}
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12,
		InsecureSkipVerify: s.Insecure} //nolint:gosec // opt-in, documented
	if caFile != "" && !s.Insecure {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading cluster CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("cluster CA %s contains no certificates", caFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &k8sAPI{
		client: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
		base:  base,
		token: token,
	}, nil
}

// firstCert parses the leaf: the first CERTIFICATE block, and its parse error
// if it has one. Deliberately strict, and deliberately not allCerts()[0] — a
// secret whose leaf is corrupt but whose chain is intact must be skipped, not
// reported with an intermediate's expiry standing in for the leaf's.
func firstCert(pemBytes []byte) (*x509.Certificate, error) {
	for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
	return nil, fmt.Errorf("no CERTIFICATE block")
}

// allCerts parses every certificate in a PEM blob, skipping any it cannot read.
// A CA bundle is a chain, and reporting only the first member hides the one
// that expires first. Lenient because one unreadable member of a bundle should
// not cost the rest — the opposite of what firstCert needs.
func allCerts(pemBytes []byte) []*x509.Certificate {
	var out []*x509.Certificate
	for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}
