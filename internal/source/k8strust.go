package source

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
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
// trustAnchors being off rather than a failure that loses the namespaced
// findings.
func (s *K8sSource) webhookCAs(ctx context.Context, api *k8sAPI, st *k8sState) ([]Item, error) {
	acc := st.anchors
	var errs []string
	for _, kind := range []string{"validatingwebhookconfigurations", "mutatingwebhookconfigurations"} {
		err := listEach(ctx, api, []string{admissionAPI + "/" + kind}, func(w webhookConfigItem) {
			owner := strings.TrimSuffix(kind, "s") + "/" + w.Metadata.Name
			for _, h := range w.Webhooks {
				if e := acc.addCABundle(h.ClientConfig.CABundle, "k8s:webhook", owner, "", nil); e != nil {
					errs = append(errs, e.Error())
				}
			}
		})
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	return nil, joinErrs(errs)
}

func (s *K8sSource) apiServiceCAs(ctx context.Context, api *k8sAPI, st *k8sState) ([]Item, error) {
	acc := st.anchors
	var errs []string
	err := listEach(ctx, api, []string{apiregistrationV1 + "/apiservices"}, func(a apiServiceItem) {
		if a.Spec.Service == nil {
			return // local, served by the API server itself
		}
		if e := acc.addCABundle(a.Spec.CABundle, "k8s:apiservice", "apiservice/"+a.Metadata.Name, "",
			map[string]string{"service": a.Spec.Service.Namespace + "/" + a.Spec.Service.Name}); e != nil {
			errs = append(errs, e.Error())
		}
	})
	if err != nil {
		errs = append(errs, err.Error())
	}
	return nil, joinErrs(errs)
}

// MeshAnchorKey is one PEM key inside an anchor object, and what that key
// actually holds. The role is per key, not per object: Istio's `cacerts` holds
// the intermediate under ca-cert.pem and the root under root-cert.pem, and
// labelling both "trust-anchor" would be a lie about one of them.
type MeshAnchorKey struct {
	Key  string `json:"key"`
	Role string `json:"role"` // "trust-anchor" or "issuer"
}

// MeshAnchor locates one service-mesh trust anchor. The defaults cover a stock
// Linkerd and Istio; an install that moved them can add its own in the config
// rather than wait for a code change.
type MeshAnchor struct {
	Mesh      string          `json:"mesh"`
	Kind      string          `json:"kind"` // "secrets" or "configmaps"
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Keys      []MeshAnchorKey `json:"keys"`
}

// Anchor kinds and key roles, as the config may spell them.
const (
	anchorSecrets    = "secrets"
	anchorConfigMaps = "configmaps"
	roleTrustAnchor  = "trust-anchor"
	roleIssuer       = "issuer"
)

// The default anchors are ConfigMaps, and that is the point: a ConfigMap holds
// the certificate and nothing else, so reading one cannot expose a key.
//
// Both meshes publish their trust root that way. Linkerd keeps it in
// linkerd-identity-trust-roots under ca-bundle.crt, and Istio distributes
// istio-ca-root-cert to every namespace with root-cert.pem in it — the same
// root istio-ca-secret holds, without the private key beside it.
var defaultMeshAnchors = []MeshAnchor{
	{"linkerd", anchorConfigMaps, "linkerd", "linkerd-identity-trust-roots",
		[]MeshAnchorKey{{"ca-bundle.crt", roleTrustAnchor}}},
	{"istio", anchorConfigMaps, "istio-system", "istio-ca-root-cert",
		[]MeshAnchorKey{{"root-cert.pem", roleTrustAnchor}}},
}

// signingSecretAnchors carry the signing key alongside the certificate, so
// reading one means reading a cluster-wide mTLS signing key. Behind
// MeshSigningSecrets, never on by default.
//
// What that costs is worth stating plainly: the Linkerd issuer lapses in a year
// by default and in twenty-four hours under cert-manager, which makes it the
// mesh certificate most likely to expire unnoticed, and no ConfigMap exposes it.
//
// linkerd-identity-issuer is a kubernetes.io/tls Secret on current Linkerd, so
// the issuer certificate is under tls.crt. crt.pem is the legacy Opaque-scheme
// name, kept so an older cluster is not silently unreadable.
var signingSecretAnchors = []MeshAnchor{
	{"linkerd", anchorSecrets, "linkerd", "linkerd-identity-issuer",
		[]MeshAnchorKey{{"tls.crt", roleIssuer}, {"crt.pem", roleIssuer}}},
	{"istio", anchorSecrets, "istio-system", "cacerts",
		[]MeshAnchorKey{{"root-cert.pem", roleTrustAnchor}, {"ca-cert.pem", roleIssuer}}},
	{"istio", anchorSecrets, "istio-system", "istio-ca-secret",
		[]MeshAnchorKey{{"ca-cert.pem", roleTrustAnchor}}},
}

// Anchors is exported so a caller can confirm what will actually be watched;
// the config package's test uses it to prove the built-ins survive.
func (s *K8sSource) Anchors() []MeshAnchor { return s.anchors() }

// anchors appends the configured anchors to the built-in ones rather than
// replacing them, the same way -endpoints and -domains add to the config
// instead of overriding it. Silently dropping Linkerd and Istio support because
// somebody named one extra object is not a trade anybody would choose.
func (s *K8sSource) anchors() []MeshAnchor {
	out := make([]MeshAnchor, 0,
		len(defaultMeshAnchors)+len(signingSecretAnchors)+len(s.MeshAnchors))
	out = append(out, defaultMeshAnchors...)
	if s.MeshSigningSecrets {
		out = append(out, signingSecretAnchors...)
	}
	return append(out, s.MeshAnchors...)
}

// ValidateMeshAnchors rejects an anchor that could never match, rather than
// letting it 404 into silence: a misspelt kind would otherwise fall through to
// the Secret branch and simply never report anything.
func ValidateMeshAnchors(anchors []MeshAnchor) error {
	for i, a := range anchors {
		where := fmt.Sprintf("meshAnchors[%d]", i)
		switch {
		case a.Mesh == "":
			return fmt.Errorf("%s: mesh is required", where)
		case a.Namespace == "":
			return fmt.Errorf("%s (%s): namespace is required", where, a.Mesh)
		case a.Name == "":
			return fmt.Errorf("%s (%s): name is required", where, a.Mesh)
		case a.Kind != anchorSecrets && a.Kind != anchorConfigMaps:
			return fmt.Errorf("%s (%s/%s): kind is %q, want %q or %q",
				where, a.Namespace, a.Name, a.Kind, anchorSecrets, anchorConfigMaps)
		case len(a.Keys) == 0:
			return fmt.Errorf("%s (%s/%s): at least one key is required",
				where, a.Namespace, a.Name)
		}
		for _, k := range a.Keys {
			if k.Key == "" {
				return fmt.Errorf("%s (%s/%s): a key name is required", where, a.Namespace, a.Name)
			}
			if k.Role != roleTrustAnchor && k.Role != roleIssuer {
				return fmt.Errorf("%s (%s/%s): key %q has role %q, want %q or %q",
					where, a.Namespace, a.Name, k.Key, k.Role, roleTrustAnchor, roleIssuer)
			}
		}
	}
	return nil
}

// meshAnchors fetches each well-known anchor by name rather than listing
// secrets cluster-wide, which keeps the RBAC ask to four named objects instead
// of every secret in the cluster.
//
// Narrower is not the same as harmless. Kubernetes cannot return part of a
// Secret, so `get` on linkerd-identity-issuer, cacerts or istio-ca-secret
// returns the CA private key beside the certificate — cluster-wide mTLS signing
// keys. Only the certificate is ever parsed and the key is never written
// anywhere, but the grant is real and docs/rbac-readonly.yaml spells it out.
//
// A missing object means that mesh is not installed, which is an answer. Only a
// denial or a broken request is worth warning about.
func (s *K8sSource) meshAnchors(ctx context.Context, api *k8sAPI, st *k8sState) ([]Item, error) {
	acc := st.anchors
	var errs []string

	for _, a := range s.anchors() {
		found, err := s.readAnchor(ctx, api, a)
		if errors.Is(err, errNotFound) {
			continue // this mesh is not installed here
		}
		if err != nil {
			errs = append(errs, a.Mesh+"/"+a.Name+": "+err.Error())
			continue
		}
		// The object is here but none of the keys we know about are. That is a
		// configured anchor going unwatched, not an absent mesh, so it must not
		// disappear the way a missing object does.
		if len(found) == 0 {
			errs = append(errs, fmt.Sprintf("%s/%s: none of its keys are present (%s)",
				a.Mesh, a.Name, strings.Join(keyNames(a), ", ")))
			continue
		}
		for _, f := range found {
			if e := acc.addCABundle(f.pem, "k8s:mesh", a.Mesh+"/"+a.Name+"#"+f.key, a.Namespace,
				map[string]string{"mesh": a.Mesh, "role": f.role, "key": f.key}); e != nil {
				errs = append(errs, e.Error())
			}
		}
	}
	return nil, joinErrs(errs)
}

func keyNames(a MeshAnchor) []string {
	out := make([]string, 0, len(a.Keys))
	for _, k := range a.Keys {
		out = append(out, k.Key)
	}
	return out
}

type anchorPEM struct {
	key  string
	role string
	pem  []byte
}

// readAnchor returns every key that is present, not just the first. Istio's
// cacerts carries the root and the intermediate under different keys, and they
// expire on different clocks — reading one and calling it the trust anchor was
// reporting the wrong certificate under the right name.
//
// A ConfigMap holds PEM as plain text and a Secret holds it base64-encoded,
// which encoding/json already undoes for []byte — two shapes, so two decodes.
func (s *K8sSource) readAnchor(ctx context.Context, api *k8sAPI, a MeshAnchor) ([]anchorPEM, error) {
	// Chosen from a closed set rather than interpolated, so a Kind that never
	// went through ValidateMeshAnchors — a K8sSource built directly rather than
	// from a config file — cannot steer the request at another API path.
	var resource string
	switch a.Kind {
	case anchorConfigMaps, anchorSecrets:
		resource = a.Kind
	default:
		return nil, fmt.Errorf("unknown anchor kind %q", a.Kind)
	}
	path := "/api/v1/namespaces/" + url.PathEscape(a.Namespace) + "/" +
		resource + "/" + url.PathEscape(a.Name)

	data := map[string][]byte{}
	if a.Kind == anchorConfigMaps {
		var cm struct {
			Data map[string]string `json:"data"`
		}
		if err := api.get(ctx, path, &cm); err != nil {
			return nil, err
		}
		for k, v := range cm.Data {
			data[k] = []byte(v)
		}
	} else {
		var sec struct {
			Data map[string][]byte `json:"data"`
		}
		if err := api.get(ctx, path, &sec); err != nil {
			return nil, err
		}
		data = sec.Data
	}

	var out []anchorPEM
	for _, k := range a.Keys {
		if v := data[k.Key]; len(v) > 0 {
			out = append(out, anchorPEM{key: k.Key, role: k.Role, pem: v})
		}
	}
	return out, nil
}

// caAccumulator dedupes CA certificates across every object that references
// them, across all three trust-anchor collectors.
//
// The same bundle is routinely pinned into a dozen webhooks and several
// configurations — cert-manager's own is the usual example — and reporting it a
// dozen times buries everything else. A CA shared between a webhook and an
// APIService is likewise one finding, not two.
//
// Keyed by a hash of the certificate itself. Issuer plus serial is the textbook
// X.509 identity but collides between two hand-made CAs that both left the CN
// empty and both started at serial 1, and mergeIntermediates in tls.go keys on
// the subject CN, which collides more easily still. The bytes cannot collide
// and need no argument.
type caAccumulator struct {
	order []string
	byKey map[string]*Item
}

func newCAAccumulator() *caAccumulator {
	return &caAccumulator{byKey: map[string]*Item{}}
}

// addCABundle records a bundle and says so when it is present but holds nothing
// parseable — raw DER, double-base64, a truncated copy. An anchor that drops
// out of the inventory unannounced is the same failure as one never read, and
// this is the only place all three collectors can share the rule.
func (a *caAccumulator) addCABundle(pemBytes []byte, src, owner, namespace string,
	extra map[string]string) error {
	if len(pemBytes) == 0 {
		return nil // CA injection, or served through a CA the cluster already trusts
	}
	certs := allCerts(pemBytes)
	if len(certs) == 0 {
		return fmt.Errorf("%s: holds no certificate", owner)
	}
	a.addBundle(certs, src, owner, namespace, extra)
	return nil
}

// addBundle records every certificate in one PEM blob. The blob is a chain as
// often as not, and each member needs its own row and its own name — two rows
// called the same thing with different dates is not a report anybody can act
// on, and iCal collapses them outright when the dates agree.
func (a *caAccumulator) addBundle(certs []*x509.Certificate, src, owner, namespace string, extra map[string]string) {
	for _, c := range certs {
		name := owner
		if len(certs) > 1 {
			name = owner + "#" + distinguish(c)
		}
		a.add(c, src, name, owner, namespace, extra)
	}
}

// distinguish names one member of a chain. The subject is what an operator
// recognises; the serial is the fallback for a CA that left it empty.
func distinguish(c *x509.Certificate) string {
	if cn := c.Subject.CommonName; cn != "" {
		return cn
	}
	return c.SerialNumber.String()
}

func (a *caAccumulator) add(c *x509.Certificate, src, name, owner, namespace string, extra map[string]string) {
	sum := sha256.Sum256(c.Raw)
	key := hex.EncodeToString(sum[:])
	if existing, ok := a.byKey[key]; ok {
		// Already reported — but by a collector that may have known less about
		// it. On a stock Istio cluster the sidecar-injector webhook pins the
		// same root as istio-system/cacerts, and webhooks runs first, so
		// without this the item keeps an empty namespace and none of the
		// mesh/role/key labels the mesh collector would have given it.
		used := appendUnique(splitList(existing.Labels["used-by"]), owner)
		existing.Labels["used-by"] = strings.Join(used, ",")
		if existing.Namespace == "" {
			existing.Namespace = namespace
		}
		for k, v := range extra {
			if existing.Labels[k] == "" {
				existing.Labels = label(existing.Labels, k, v)
			}
		}
		return
	}

	labels := map[string]string{}
	labels = label(labels, LabelIssuer, c.Issuer.CommonName)
	labels = label(labels, LabelSerial, c.SerialNumber.String())
	// Deliberately no LabelHosts: a CA's SANs are not hosts it fronts, and
	// feeding them to ranking would earn a trust anchor the wildcard and
	// multi-SAN bonuses meant for a leaf that actually serves those names.
	labels = label(labels, "subject", c.Subject.CommonName)
	labels["used-by"] = owner
	for k, v := range extra {
		labels = label(labels, k, v)
	}

	it := &Item{
		Kind:      KindTrustAnchor,
		Name:      name,
		Expires:   c.NotAfter,
		Source:    src,
		Namespace: namespace,
		Labels:    labels,
	}
	a.byKey[key] = it
	a.order = append(a.order, key)
}

// items returns everything the three collectors found, in discovery order.
//
// Collect drains this once, after every class has run, rather than each class
// returning its own slice: a class that merges into an item an earlier class
// created would otherwise be writing to a map entry whose copy had already been
// handed back. A denied class still loses only its own findings, because a
// class that fails simply adds nothing here.
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
