package source

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FederationSource reads SAML and OIDC identity-provider metadata.
//
// The best value-per-line in the tool, and the only source besides `tls` and
// `domains` that needs no credential at all: IdP metadata is published to be
// fetched. It reports two different deadlines from one document.
//
//   - The **signing certificates** embedded in the metadata. When one lapses,
//     every federated login through that provider stops at the same moment,
//     and it does not present as a certificate problem to anybody looking at
//     the application side. These are trust anchors.
//   - The **metadata's own `validUntil`**, which relying parties are entitled
//     to refuse after. A federation that lets its metadata go stale breaks the
//     same way, one relying party at a time.
//
// Nobody owns either date. That is the whole reason to watch them.
type FederationSource struct {
	Providers []FederationProvider
	Timeout   time.Duration
}

// FederationProvider is one metadata document to fetch.
type FederationProvider struct {
	// Name is what shows up in the report; the URL is rarely recognisable.
	Name string `json:"name"`
	// URL is the metadata endpoint. Any IdP publishes one.
	URL string `json:"url"`
	// Labels are operator hints, merged into every item from this provider.
	Labels map[string]string `json:"labels"`
}

// Name identifies this source in an item's Source field.
func (s *FederationSource) Name() string { return "federation" }

// ValidateFederation rejects a provider that could never be fetched.
func ValidateFederation(providers []FederationProvider) error {
	for i, p := range providers {
		if p.URL == "" {
			return fmt.Errorf("federation[%d]: url is required", i)
		}
		if !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
			return fmt.Errorf("federation[%d] (%s): url must be http or https", i, p.URL)
		}
	}
	return nil
}

// Collect fetches and parses each provider's metadata.
func (s *FederationSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	units := make([]collectUnit, 0, len(s.Providers))
	for _, p := range s.Providers {
		name := p.Name
		if name == "" {
			name = p.URL
		}
		units = append(units, collectUnit{
			Name:    name,
			Collect: func() ([]Item, error) { return s.provider(ctx, client, p, name) },
		})
	}
	items, _, err := collectUnits(units)
	return items, err
}

// samlRole is an IDPSSODescriptor or SPSSODescriptor: the part of the document
// that carries keys.
type samlRole struct {
	KeyDescriptors []struct {
		Use   string   `xml:"use,attr"`
		Certs []string `xml:"KeyInfo>X509Data>X509Certificate"`
	} `xml:"KeyDescriptor"`
}

type samlEntity struct {
	EntityID   string     `xml:"entityID,attr"`
	ValidUntil string     `xml:"validUntil,attr"`
	IDP        []samlRole `xml:"IDPSSODescriptor"`
	SP         []samlRole `xml:"SPSSODescriptor"`
}

// samlDocument covers both shapes a metadata endpoint serves: a single
// EntityDescriptor, or an EntitiesDescriptor wrapping many of them, which is
// what a federation rather than a single provider publishes.
type samlDocument struct {
	EntityID   string       `xml:"entityID,attr"`
	ValidUntil string       `xml:"validUntil,attr"`
	IDP        []samlRole   `xml:"IDPSSODescriptor"`
	SP         []samlRole   `xml:"SPSSODescriptor"`
	Entities   []samlEntity `xml:"EntityDescriptor"`
}

func (s *FederationSource) provider(ctx context.Context, client *http.Client,
	p FederationProvider, name string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/samlmetadata+xml, application/xml, text/xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", p.URL, resp.Status)
	}
	// Bounded: metadata is a document, and an endpoint that streams forever is
	// not one this tool should follow.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var doc samlDocument
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: metadata is not parseable SAML XML: %w", p.URL, err)
	}

	entities := doc.Entities
	if len(entities) == 0 {
		// A single EntityDescriptor at the root.
		entities = []samlEntity{{
			EntityID: doc.EntityID, ValidUntil: doc.ValidUntil,
			IDP: doc.IDP, SP: doc.SP,
		}}
	} else if doc.ValidUntil != "" {
		// An aggregate carries validUntil on the EntitiesDescriptor itself,
		// and real federation metadata usually does. Reading it only from the
		// entities would lose one of the two deadlines this source exists for
		// — silently, because the certificates still produce rows.
		for i := range entities {
			if entities[i].ValidUntil == "" {
				entities[i].ValidUntil = doc.ValidUntil
			}
		}
	}

	var items []Item
	for _, e := range entities {
		// With more than one entity in the document, the provider name alone
		// does not identify a row — and two entities whose certificates share
		// an expiry would collide on their iCal UID as well as on screen.
		entityName := name
		if len(entities) > 1 && e.EntityID != "" {
			entityName = name + " [" + e.EntityID + "]"
		}
		items = append(items, federationItems(e, p, entityName)...)
	}
	if len(items) == 0 {
		// A document that parsed but held no dates is not a clean result; it
		// usually means the URL serves something other than metadata.
		return nil, fmt.Errorf("%s: no signing certificates or validUntil found — is this a metadata URL?", p.URL)
	}
	return items, nil
}

func federationItems(e samlEntity, p FederationProvider, name string) []Item {
	base := map[string]string{}
	for k, v := range p.Labels {
		base = label(base, k, v)
	}
	base = label(base, "entity-id", e.EntityID)
	// Federated login is by definition reachable by the people who use it.
	base[LabelPublic] = "true"

	var items []Item

	// The metadata's own expiry. Relying parties may refuse it after this.
	if until, ok := cfTime(e.ValidUntil); ok {
		labels := copyLabels(base)
		labels["document"] = "metadata"
		items = append(items, Item{
			Kind:      KindTrustAnchor,
			Name:      name + " metadata",
			Expires:   until,
			Source:    "federation:metadata",
			Namespace: name,
			Labels:    labels,
		})
	}

	seen := map[string]bool{}
	for _, role := range append(append([]samlRole{}, e.IDP...), e.SP...) {
		for _, kd := range role.KeyDescriptors {
			// An encryption key lapsing is a different, smaller problem than a
			// signing key lapsing; both are reported, and the use says which.
			use := kd.Use
			if use == "" {
				use = "signing and encryption"
			}
			for _, raw := range kd.Certs {
				cert, ok := parseB64Cert(raw)
				if !ok {
					continue
				}
				key := cert.Issuer.CommonName + "/" + cert.SerialNumber.String()
				if seen[key] {
					continue // the same certificate is routinely listed twice
				}
				seen[key] = true

				labels := copyLabels(base)
				labels["use"] = use
				labels = label(labels, LabelIssuer, cert.Issuer.CommonName)
				labels = label(labels, "subject", cert.Subject.CommonName)
				items = append(items, Item{
					Kind:      KindTrustAnchor,
					Name:      name + " " + use + " certificate",
					Expires:   cert.NotAfter,
					Source:    "federation:certificate",
					Namespace: name,
					Labels:    labels,
				})
			}
		}
	}
	return items
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// parseB64Cert reads the base64 DER that SAML metadata embeds — not PEM, and
// routinely wrapped across lines with whitespace that has to come out first.
func parseB64Cert(raw string) (*x509.Certificate, bool) {
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, raw)
	if clean == "" {
		return nil, false
	}
	der, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, false
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, false
	}
	return cert, true
}
