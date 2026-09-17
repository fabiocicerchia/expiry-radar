package source

import (
	"context"
	"crypto/x509"
	"errors"
	"net/url"
	"strings"
)

// The trust anchors a cluster validates against, none of which anybody watches.
//
// An admission webhook CA is the sharpest case: when it expires the API server
// stops admitting anything, and the failure looks like the cluster being broken
// rather than like a certificate. The aggregation layer's caBundle takes out
// metrics-server and everything else served through it. A mesh trust anchor
// takes out every mTLS handshake at once. All three are KindTrustAnchor, which
// ranks above a domain: nothing behind them fails gracefully.

const (
	admissionAPI      = "/apis/admissionregistration.k8s.io/v1"
	apiregistrationV1 = "/apis/apiregistration.k8s.io/v1"
)

type webhookConfigItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Webhooks []struct {
		Name         string `json:"name"`
		ClientConfig struct {
			CABundle []byte `json:"caBundle"` // base64 in JSON; decoded for us
		} `json:"clientConfig"`
	} `json:"webhooks"`
}

type apiServiceItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		CABundle []byte `json:"caBundle"`
		// A nil Service means the APIService is served by the API server
		// itself. There is no bundle to expire and no remote to trust.
		Service *struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"service"`
	} `json:"spec"`
}

// webhookCAs reads both admission webhook configuration kinds. These are
// cluster-scoped, so they bypass paths() — a namespaced Role cannot list them,
// and the resulting 403 is a warning the operator can silence with
// skipWebhooks rather than a failure that loses the namespaced findings.
func (s *K8sSource) webhookCAs(ctx context.Context, api *k8sAPI) ([]Item, error) {
	acc := newCAAccumulator()
	var errs []string
	for _, kind := range []string{"validatingwebhookconfigurations", "mutatingwebhookconfigurations"} {
		err := listEach(ctx, api, []string{admissionAPI + "/" + kind}, func(w webhookConfigItem) {
			owner := strings.TrimSuffix(kind, "s") + "/" + w.Metadata.Name
			for _, h := range w.Webhooks {
				// An empty caBundle means CA injection, or a webhook served
				// through a CA the cluster already trusts. Nothing to expire.
				for _, c := range allCerts(h.ClientConfig.CABundle) {
					acc.add(c, "k8s:webhook", owner, "", nil)
				}
			}
		})
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	return acc.items(), joinErrs(errs)
}

func (s *K8sSource) apiServiceCAs(ctx context.Context, api *k8sAPI) ([]Item, error) {
	acc := newCAAccumulator()
	err := listEach(ctx, api, []string{apiregistrationV1 + "/apiservices"}, func(a apiServiceItem) {
		if a.Spec.Service == nil {
			return // local, served by the API server itself
		}
		for _, c := range allCerts(a.Spec.CABundle) {
			acc.add(c, "k8s:apiservice", "apiservice/"+a.Metadata.Name, "",
				map[string]string{"service": a.Spec.Service.Namespace + "/" + a.Spec.Service.Name})
		}
	})
	return acc.items(), err
}

// MeshAnchor locates one service-mesh trust anchor. The defaults cover a stock
// Linkerd and Istio; an install that moved them can say so in the config rather
// than wait for a code change.
type MeshAnchor struct {
	Mesh      string   `json:"mesh"`
	Kind      string   `json:"kind"` // "secrets" or "configmaps"
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Keys      []string `json:"keys"`
	Role      string   `json:"role"` // "trust-anchor" or "issuer"
}

// Both Linkerd entries are here on purpose and for different reasons: the trust
// root runs for years, while the issuer runs a year by default and twenty-four
// hours under cert-manager. The issuer is the one that actually bites.
var defaultMeshAnchors = []MeshAnchor{
	{"linkerd", "configmaps", "linkerd", "linkerd-identity-trust-roots", []string{"ca-bundle.crt"}, "trust-anchor"},
	{"linkerd", "secrets", "linkerd", "linkerd-identity-issuer", []string{"crt.pem"}, "issuer"},
	{"istio", "secrets", "istio-system", "cacerts", []string{"ca-cert.pem", "root-cert.pem"}, "trust-anchor"},
	{"istio", "secrets", "istio-system", "istio-ca-secret", []string{"ca-cert.pem"}, "trust-anchor"},
}

func (s *K8sSource) anchors() []MeshAnchor {
	if len(s.MeshAnchors) > 0 {
		return s.MeshAnchors
	}
	return defaultMeshAnchors
}

// meshAnchors fetches each well-known anchor by name rather than listing
// secrets cluster-wide: `get` on four named objects is a permission a security
// team will grant, and `list secrets` across every namespace is not.
//
// A missing object means that mesh is not installed, which is an answer. Only a
// denial or a broken request is worth warning about.
func (s *K8sSource) meshAnchors(ctx context.Context, api *k8sAPI) ([]Item, error) {
	acc := newCAAccumulator()
	var errs []string
	for _, a := range s.anchors() {
		pem, err := s.readAnchor(ctx, api, a)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, a.Mesh+"/"+a.Name+": "+err.Error())
			continue
		}
		for _, c := range allCerts(pem) {
			acc.add(c, "k8s:mesh", a.Mesh+"/"+a.Name, a.Namespace, map[string]string{
				"mesh": a.Mesh,
				"role": a.Role,
			})
		}
	}
	return acc.items(), joinErrs(errs)
}

// readAnchor returns the first key that is present. A ConfigMap holds PEM as
// plain text and a Secret holds it base64-encoded, which encoding/json already
// undoes for []byte — two shapes, so two decodes.
func (s *K8sSource) readAnchor(ctx context.Context, api *k8sAPI, a MeshAnchor) ([]byte, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(a.Namespace) + "/" +
		a.Kind + "/" + url.PathEscape(a.Name)

	if a.Kind == "configmaps" {
		var cm struct {
			Data map[string]string `json:"data"`
		}
		if err := api.get(ctx, path, &cm); err != nil {
			return nil, err
		}
		for _, k := range a.Keys {
			if v := cm.Data[k]; v != "" {
				return []byte(v), nil
			}
		}
		return nil, errNotFound
	}

	var sec struct {
		Data map[string][]byte `json:"data"`
	}
	if err := api.get(ctx, path, &sec); err != nil {
		return nil, err
	}
	for _, k := range a.Keys {
		if v := sec.Data[k]; len(v) > 0 {
			return v, nil
		}
	}
	return nil, errNotFound
}

// caAccumulator dedupes CA certificates across the objects that reference them.
//
// The same bundle is routinely pinned into a dozen webhooks and several
// configurations — cert-manager's own is the usual example — and reporting it a
// dozen times buries everything else. Keyed by issuer and serial, the same way
// the TLS chain collector dedupes intermediates.
type caAccumulator struct {
	order []string
	byKey map[string]*Item
}

func newCAAccumulator() *caAccumulator {
	return &caAccumulator{byKey: map[string]*Item{}}
}

func (a *caAccumulator) add(c *x509.Certificate, src, owner, namespace string, extra map[string]string) {
	key := c.Issuer.CommonName + "/" + c.SerialNumber.String()
	if existing, ok := a.byKey[key]; ok {
		// Already reported; record that this object relies on it too.
		used := appendUnique(splitList(existing.Labels["used-by"]), owner)
		existing.Labels["used-by"] = strings.Join(used, ",")
		return
	}

	labels := map[string]string{}
	labels = label(labels, LabelIssuer, c.Issuer.CommonName)
	labels = label(labels, LabelSerial, c.SerialNumber.String())
	labels = label(labels, LabelHosts, strings.Join(c.DNSNames, ","))
	labels = label(labels, "subject", c.Subject.CommonName)
	labels["used-by"] = owner
	for k, v := range extra {
		labels = label(labels, k, v)
	}

	it := &Item{
		Kind:      KindTrustAnchor,
		Name:      owner,
		Expires:   c.NotAfter,
		Source:    src,
		Namespace: namespace,
		Labels:    labels,
	}
	a.byKey[key] = it
	a.order = append(a.order, key)
}

func (a *caAccumulator) items() []Item {
	out := make([]Item, 0, len(a.order))
	for _, k := range a.order {
		out = append(out, *a.byKey[k])
	}
	return out
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func joinErrs(errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}
