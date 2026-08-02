package source

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VaultSource reports what expires in Vault: the token expiry_radar itself
// holds, and the certificates issued by any PKI mounts it is pointed at.
//
// Deliberately NOT implemented: enumerating dynamic leases via
// `sys/leases/lookup`. That endpoint is a PUT and needs the `update` capability
// plus `sudo` — which would break the read-only promise this tool is sold on.
// A PKI mount answers the same question ("what stops working, when") with GET
// and LIST only. If lease enumeration is ever needed, it should be an explicit,
// separately-documented opt-in, not the default posture.
type VaultSource struct {
	Addr      string   // https://vault.example:8200 (VAULT_ADDR)
	Token     string   // VAULT_TOKEN
	Namespace string   // Vault Enterprise namespace, if any
	PKIMounts []string // e.g. ["pki", "pki_int"]
	MaxCerts  int      // per mount; 0 means the default below
	Timeout   time.Duration
}

const defaultMaxCerts = 500

func (s *VaultSource) Name() string { return "vault" }

func (s *VaultSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Addr == "" || s.Token == "" {
		return nil, fmt.Errorf("vault source needs an address and a token")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	var items []Item
	var warnings []string

	if item, err := s.tokenItem(ctx, client); err != nil {
		warnings = append(warnings, "token lookup-self: "+err.Error())
	} else if item != nil {
		items = append(items, *item)
	}

	for _, mount := range s.PKIMounts {
		got, truncated, err := s.pkiCerts(ctx, client, mount)
		if err != nil {
			warnings = append(warnings, mount+": "+err.Error())
			continue
		}
		items = append(items, got...)
		if truncated > 0 {
			// Never let a cap look like a clean result.
			warnings = append(warnings, fmt.Sprintf("%s: stopped after %d certificates, %d not read", mount, s.maxCerts(), truncated))
		}
	}

	if len(warnings) > 0 {
		return items, fmt.Errorf("%s", strings.Join(warnings, "; "))
	}
	return items, nil
}

func (s *VaultSource) maxCerts() int {
	if s.MaxCerts > 0 {
		return s.MaxCerts
	}
	return defaultMaxCerts
}

func (s *VaultSource) do(ctx context.Context, client *http.Client, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(s.Addr, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", s.Token)
	if s.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.Namespace)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s %s: permission denied (a read-only policy needs read+list on this path)", method, path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *VaultSource) tokenItem(ctx context.Context, client *http.Client) (*Item, error) {
	var body struct {
		Data struct {
			DisplayName string `json:"display_name"`
			ExpireTime  string `json:"expire_time"`
		} `json:"data"`
	}
	if err := s.do(ctx, client, http.MethodGet, "/v1/auth/token/lookup-self", &body); err != nil {
		return nil, err
	}
	if body.Data.ExpireTime == "" {
		return nil, nil // a root or periodic token does not expire; nothing to report
	}
	expires, err := time.Parse(time.RFC3339, body.Data.ExpireTime)
	if err != nil {
		return nil, err
	}
	name := body.Data.DisplayName
	if name == "" {
		name = "vault token"
	}
	return &Item{
		Kind:    KindVaultLease,
		Name:    name,
		Expires: expires,
		Source:  "vault:token",
		// When this expires, every other Vault reading in this report stops
		// working — which is exactly what blast radius should reflect.
		Labels: map[string]string{LabelBlastRadius: "0.9"},
	}, nil
}

func (s *VaultSource) pkiCerts(ctx context.Context, client *http.Client, mount string) ([]Item, int, error) {
	var list struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	// LIST is Vault's spelling of a read that enumerates.
	if err := s.do(ctx, client, "LIST", "/v1/"+url.PathEscape(mount)+"/certs", &list); err != nil {
		return nil, 0, err
	}

	serials := list.Data.Keys
	truncated := 0
	if len(serials) > s.maxCerts() {
		truncated = len(serials) - s.maxCerts()
		serials = serials[:s.maxCerts()]
	}

	var items []Item
	for _, serial := range serials {
		var body struct {
			Data struct {
				Certificate string `json:"certificate"`
			} `json:"data"`
		}
		if err := s.do(ctx, client, http.MethodGet, "/v1/"+url.PathEscape(mount)+"/cert/"+url.PathEscape(serial), &body); err != nil {
			continue
		}
		cert, err := parsePEMCert(body.Data.Certificate)
		if err != nil {
			continue
		}
		kind := KindTLSCert
		if cert.IsCA {
			kind = KindIntermediate
		}
		items = append(items, Item{
			Kind:      kind,
			Name:      certName(cert, serial),
			Expires:   cert.NotAfter,
			Source:    "vault:" + mount,
			Namespace: s.Namespace,
			Labels: map[string]string{
				LabelSerial: serial,
				LabelHosts:  strings.Join(cert.DNSNames, ","),
				LabelIssuer: cert.Issuer.CommonName,
			},
		})
	}
	return items, truncated, nil
}

func parsePEMCert(s string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("not PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

func certName(cert *x509.Certificate, fallback string) string {
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return fallback
}
