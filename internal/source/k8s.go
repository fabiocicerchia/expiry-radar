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

	// Skips mirror the AWS source: a resource class an operator has decided not
	// to grant is skipped outright rather than collected and denied, so the run
	// is quiet instead of warning about a permission nobody intends to give.
	SkipSecrets     bool
	SkipCertManager bool
	SkipWebhooks    bool
	SkipAPIServices bool
	SkipMesh        bool

	// MeshAnchors overrides the well-known Linkerd and Istio locations for a
	// non-default install. Empty uses defaultMeshAnchors.
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
	st := &k8sState{certs: map[string]certRef{}, secretKeys: map[string]bool{}}
	st.refs, st.ingressErr = s.ingressRefs(ctx, api)

	items, _, err := collectResources(s.resources(ctx, api, st))
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
	secretKeys map[string]bool    // keyed ns/secretName
}

// k8sResource is one class of thing this source reads, named so a failure can
// say which. Split out of Collect so the degradation rule — one denied
// permission must not lose the other resources' findings — is testable without
// a cluster, exactly as collectServices does it for AWS.
type k8sResource struct {
	Name    string
	Skipped bool
	Collect func() ([]Item, error)
}

// resourceResult is what one resource class returned. A resource that returned
// nothing is not the same as one that was denied, and not the same as one that
// was skipped: collapsing the three would let a cluster with no cert-manager
// read as a cluster whose cert-manager adapter works.
type resourceResult struct {
	Name    string
	Skipped bool
	Items   int
	Err     error
}

func (s *K8sSource) resources(ctx context.Context, api *k8sAPI, st *k8sState) []k8sResource {
	return []k8sResource{
		// The ingress list produces no items of its own — it only supplies the
		// hosts, class and public flag a secret is ranked by. Reporting its
		// failure through the same channel keeps one error path instead of a
		// special case, and losing it costs ranking evidence, not findings.
		{"ingresses", false, func() ([]Item, error) { return nil, st.ingressErr }},
		{"secrets", s.SkipSecrets, func() ([]Item, error) { return s.tlsSecrets(ctx, api, st) }},
		{"certificates", s.SkipCertManager, func() ([]Item, error) { return s.certificates(ctx, api, st) }},
		{"webhooks", s.SkipWebhooks, func() ([]Item, error) { return s.webhookCAs(ctx, api) }},
		{"apiservices", s.SkipAPIServices, func() ([]Item, error) { return s.apiServiceCAs(ctx, api) }},
		{"mesh", s.SkipMesh, func() ([]Item, error) { return s.meshAnchors(ctx, api) }},
	}
}

func collectResources(rs []k8sResource) ([]Item, []resourceResult, error) {
	var items []Item
	var warnings []string
	results := make([]resourceResult, 0, len(rs))
	for _, r := range rs {
		if r.Skipped {
			results = append(results, resourceResult{Name: r.Name, Skipped: true})
			continue
		}
		got, err := r.Collect()
		if err != nil {
			// One denied resource class must not lose the others' findings. The
			// partial items are returned alongside the error, so a caller that
			// ignores the error is not silently throwing away what worked.
			warnings = append(warnings, r.Name+": "+err.Error())
			results = append(results, resourceResult{Name: r.Name, Items: len(got), Err: err})
			items = append(items, got...)
			continue
		}
		results = append(results, resourceResult{Name: r.Name, Items: len(got)})
		items = append(items, got...)
	}
	if len(warnings) > 0 {
		return items, results, fmt.Errorf("%s", strings.Join(warnings, "; "))
	}
	return items, results, nil
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
// A 404 is not a failure: on a list endpoint it means the CRD is not installed,
// and on a namespaced path it means the namespace is not there. Both are
// answers about the cluster, and a cluster without cert-manager should not
// warn on every run.
func listEach[T any](ctx context.Context, api *k8sAPI, paths []string, fn func(T)) error {
	var warnings []string
	for _, p := range paths {
		var list struct {
			Items []T `json:"items"`
		}
		if err := api.get(ctx, p, &list); err != nil {
			if !errors.Is(err, errNotFound) {
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
	err := listEach(ctx, api,
		s.paths("/api/v1", "secrets?fieldSelector=type%3Dkubernetes.io%2Ftls"),
		func(sec secretItem) {
			key := sec.Metadata.Namespace + "/" + sec.Metadata.Name
			// Record the secret even when its contents are unusable: the
			// cert-manager collector uses this to tell "not issued yet" from
			// "issued, and reported over there".
			st.secretKeys[key] = true

			cert, err := firstCert(sec.Data["tls.crt"])
			if err != nil {
				return // a malformed secret is a different problem; do not stop the scan
			}
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
		})
	return items, err
}

func hostsOf(ref ingressRef, cert *x509.Certificate) []string {
	if len(ref.Hosts) > 0 {
		return ref.Hosts
	}
	return cert.DNSNames
}

// paths expands the configured namespaces into API paths. Cluster-wide listing
// needs a ClusterRole; per-namespace listing works with a plain Role, which is
// the posture most security teams will actually approve.
func (s *K8sSource) paths(apiRoot, resource string) []string {
	if len(s.Namespaces) == 0 {
		return []string{apiRoot + "/" + resource}
	}
	out := make([]string, 0, len(s.Namespaces))
	for _, ns := range s.Namespaces {
		out = append(out, apiRoot+"/namespaces/"+url.PathEscape(ns)+"/"+resource)
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

func firstCert(pemBytes []byte) (*x509.Certificate, error) {
	certs := allCerts(pemBytes)
	if len(certs) == 0 {
		return nil, fmt.Errorf("no CERTIFICATE block")
	}
	return certs[0], nil
}

// allCerts parses every certificate in a PEM blob. A CA bundle is a chain, and
// reporting only the first member hides the one that expires first.
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
