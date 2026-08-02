package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// K8sSource reads TLS secrets and the ingresses that reference them.
//
// It talks to the API server directly over HTTPS with net/http rather than
// pulling in client-go: two GETs against two stable, versioned endpoints do not
// justify that dependency tree. Both verbs used are `list` — see
// docs/rbac-readonly.yaml for the exact Role.
//
// Certificate expiry comes from the secret's own tls.crt, not from the ingress:
// the ingress only says which secret is public and under which class, which is
// what the blast-radius ranking needs.
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
}

const (
	inClusterServer    = "https://kubernetes.default.svc"
	inClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // well-known path, not a credential
	inClusterCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

func (s *K8sSource) Name() string { return "k8s" }

func (s *K8sSource) Collect(ctx context.Context) ([]Item, error) {
	client, base, token, err := s.client()
	if err != nil {
		return nil, err
	}

	get := func(path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("GET %s: forbidden — expiry-radar needs list on ingresses and secrets (see docs/rbac-readonly.yaml)", path)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	refs, err := s.ingressRefs(get)
	if err != nil {
		return nil, err
	}
	return s.tlsSecrets(get, refs)
}

// ingressRef records what an ingress says about a secret it uses.
type ingressRef struct {
	Hosts   []string
	Class   string
	Ingress string
}

type ingressList struct {
	Items []struct {
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
	} `json:"items"`
}

func (s *K8sSource) ingressRefs(get func(string, any) error) (map[string]ingressRef, error) {
	refs := map[string]ingressRef{}
	for _, path := range s.paths("/apis/networking.k8s.io/v1", "ingresses") {
		var list ingressList
		if err := get(path, &list); err != nil {
			return nil, err
		}
		for _, ing := range list.Items {
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
		}
	}
	return refs, nil
}

type secretList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Type string            `json:"type"`
		Data map[string][]byte `json:"data"` // encoding/json base64-decodes []byte for us
	} `json:"items"`
}

func (s *K8sSource) tlsSecrets(get func(string, any) error, refs map[string]ingressRef) ([]Item, error) {
	var items []Item
	for _, path := range s.paths("/api/v1", "secrets?fieldSelector=type%3Dkubernetes.io%2Ftls") {
		var list secretList
		if err := get(path, &list); err != nil {
			return nil, err
		}
		for _, sec := range list.Items {
			cert, err := firstCert(sec.Data["tls.crt"])
			if err != nil {
				continue // a malformed secret is a different problem; do not stop the scan
			}
			key := sec.Metadata.Namespace + "/" + sec.Metadata.Name
			ref := refs[key]

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
	return items, nil
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

func (s *K8sSource) client() (*http.Client, string, string, error) {
	base := strings.TrimSuffix(s.Server, "/")
	token := s.Token
	caFile := s.CAFile

	if base == "" {
		base = inClusterServer
		if token == "" {
			b, err := os.ReadFile(inClusterTokenFile)
			if err != nil {
				return nil, "", "", fmt.Errorf("no --kube-server given and no in-cluster token: %w", err)
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
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: s.Insecure} //nolint:gosec // opt-in, documented
	if caFile != "" && !s.Insecure {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, "", "", fmt.Errorf("reading cluster CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, "", "", fmt.Errorf("cluster CA %s contains no certificates", caFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, base, token, nil
}

func firstCert(pemBytes []byte) (*x509.Certificate, error) {
	for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
	return nil, fmt.Errorf("no CERTIFICATE block")
}
