package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// AzureSource reports what expires in Entra ID and Key Vault.
//
// This is the biggest gap in most estates. Every app registration and service
// principal carries client secrets and certificates that expire on their own
// schedule, nothing surfaces them until an integration stops authenticating,
// and Microsoft's own answer is a PowerShell script you are expected to run by
// hand — which is a fair admission that there is no good one.
//
// Two token audiences are needed and there is no way around it: Microsoft Graph
// and Key Vault are separate resources with separate scopes, so this holds one
// token per audience and acquires them independently.
type AzureSource struct {
	TenantID string
	ClientID string
	// ClientSecret never comes from the config file; Load fills it from
	// $AZURE_CLIENT_SECRET.
	ClientSecret string
	// Vaults are Key Vault names ("acme-prod"), not URLs. Empty skips Key
	// Vault: there is no cross-subscription listing without ARM permissions
	// this source deliberately does not ask for.
	Vaults  []string
	Timeout time.Duration

	// Endpoints overrides the login, Graph and vault hosts. A test seam.
	Endpoints map[string]string

	SkipApplications bool
	SkipPrincipals   bool
	SkipVaults       bool

	tokens map[string]azureToken
}

type azureToken struct {
	value string
	till  time.Time
}

const (
	azureLogin       = "https://login.microsoftonline.com"
	azureGraph       = "https://graph.microsoft.com"
	azureGraphScope  = "https://graph.microsoft.com/.default"
	azureVaultScope  = "https://vault.azure.net/.default"
	azureVaultAPIVer = "7.4"
	// A cap, not a limit on what is reported: hitting it is a truncated read
	// and says so rather than passing for a complete one.
	azureMaxPages = 50
)

// Name identifies this source in an item's Source field.
func (s *AzureSource) Name() string { return "azure" }

// Collect reads app registrations, service principals and Key Vault.
func (s *AzureSource) Collect(ctx context.Context) ([]Item, error) {
	if s.TenantID == "" || s.ClientID == "" || s.ClientSecret == "" {
		return nil, fmt.Errorf(
			"azure source needs tenantId, clientId and $AZURE_CLIENT_SECRET")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"applications", s.SkipApplications,
			func() ([]Item, error) { return s.credentials(ctx, client, "applications") }},
		{"serviceprincipals", s.SkipPrincipals,
			func() ([]Item, error) { return s.credentials(ctx, client, "servicePrincipals") }},
		{"keyvault", s.SkipVaults || len(s.Vaults) == 0,
			func() ([]Item, error) { return s.keyVaults(ctx, client) }},
	})
	return items, err
}

func (s *AzureSource) endpoint(name, fallback string) string {
	if u, ok := s.Endpoints[name]; ok && u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return fallback
}

// token performs the client-credentials flow for one audience and caches the
// result. Graph and Key Vault are different resources, so "one token" is not a
// thing that exists here.
func (s *AzureSource) token(ctx context.Context, client *http.Client, scope string) (string, error) {
	if s.tokens == nil {
		s.tokens = map[string]azureToken{}
	}
	//nolint:forbidigo // FC-GEN-055: an OAuth token's life is the provider's real clock, not the report's.
	if t, ok := s.tokens[scope]; ok && time.Now().Before(t.till) {
		return t.value, nil
	}

	form := neturl.Values{}
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", s.ClientSecret)
	form.Set("scope", scope)
	form.Set("grant_type", "client_credentials")

	u := s.endpoint("login", azureLogin) + "/" + neturl.PathEscape(s.TenantID) + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		// The secret is in the body, not the URL, but wrap anyway so no
		// caller downstream learns to print raw transport errors.
		return "", fmt.Errorf("azure token request failed: %w", err)
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("azure token for %s: %s — check tenantId, clientId and the client secret",
			scope, resp.Status)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("azure token for %s: no access token returned", scope)
	}
	s.tokens[scope] = azureToken{
		value: out.AccessToken,
		// Renew a minute early: a token expiring mid-scan fails half the reads.
		//nolint:forbidigo // FC-GEN-055: an OAuth token's life is the provider's real clock, not the report's.
		till: time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - time.Minute),
	}
	return out.AccessToken, nil
}

// sameHost reports whether a server-supplied continuation URL points at the
// host we started from. Graph and Key Vault paginate with absolute nextLinks,
// and re-attaching a bearer token to whatever URL a response hands back is a
// habit worth not having, even when the response is authentic.
func sameHost(next, base string) bool {
	n, err := neturl.Parse(next)
	if err != nil {
		return false
	}
	b, err := neturl.Parse(base)
	if err != nil {
		return false
	}
	return strings.EqualFold(n.Host, b.Host) && strings.EqualFold(n.Scheme, b.Scheme)
}

func azureGet(ctx context.Context, s *AzureSource, client *http.Client,
	url, scope string, out any) error {
	tok, err := s.token(ctx, client, scope)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s: %s — the app registration is missing a read role for this resource",
			url, resp.Status)
	default:
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type azureCredential struct {
	KeyID       string `json:"keyId"`
	DisplayName string `json:"displayName"`
	EndDateTime string `json:"endDateTime"`
	Hint        string `json:"hint"`
	Usage       string `json:"usage"`
	Type        string `json:"type"`
}

type azureDirectoryObject struct {
	ID                  string            `json:"id"`
	AppID               string            `json:"appId"`
	DisplayName         string            `json:"displayName"`
	PasswordCredentials []azureCredential `json:"passwordCredentials"`
	KeyCredentials      []azureCredential `json:"keyCredentials"`
}

// credentials reads applications or servicePrincipals. Both carry the same two
// credential collections, so one function covers them.
//
// A service principal's keyCredentials are where a SAML signing certificate
// lives, which is why principals are read as well as applications: when that
// one lapses, every sign-in through the app stops at once.
func (s *AzureSource) credentials(ctx context.Context, client *http.Client, kind string) ([]Item, error) {
	base := s.endpoint("graph", azureGraph)
	u := fmt.Sprintf(
		"%s/v1.0/%s?$select=id,appId,displayName,passwordCredentials,keyCredentials&$top=999",
		base, kind)

	var items []Item
	var warnings []string
	page := 0
	for ; page < azureMaxPages && u != ""; page++ {
		var body struct {
			Value    []azureDirectoryObject `json:"value"`
			NextLink string                 `json:"@odata.nextLink"`
		}
		if err := azureGet(ctx, s, client, u, azureGraphScope, &body); err != nil {
			warnings = append(warnings, err.Error())
			break
		}
		for _, obj := range body.Value {
			items = append(items, azureCredentialItems(obj, kind)...)
		}
		if body.NextLink != "" && !sameHost(body.NextLink, base) {
			warnings = append(warnings, kind+": the API returned a continuation URL on another host")
			break
		}
		u = body.NextLink
	}
	if u != "" && page >= azureMaxPages {
		warnings = append(warnings, kind+": stopped after "+strconv.Itoa(azureMaxPages)+
			" pages; the rest were not read")
	}
	return items, joinErrs(warnings)
}

func azureCredentialItems(obj azureDirectoryObject, kind string) []Item {
	name := obj.DisplayName
	if name == "" {
		name = obj.AppID
	}
	src := "azure:app"
	if kind == "servicePrincipals" {
		src = "azure:serviceprincipal"
	}

	var items []Item
	add := func(c azureCredential, credKind Kind, what string) {
		expires, ok := cfTime(c.EndDateTime)
		if !ok {
			return
		}
		credName := c.DisplayName
		if credName == "" {
			credName = c.KeyID
		}
		labels := map[string]string{"credential": what}
		labels = label(labels, "app-id", obj.AppID)
		labels = label(labels, "usage", c.Usage)
		// Graph's `hint` is the first characters of the client secret.
		// Microsoft publishes it as non-sensitive and three characters is not
		// a usable credential, but a tool whose whole promise is that it never
		// handles secrets should not put a prefix of one into an HTML report.
		// A SAML signing certificate is not one integration breaking, it is
		// every sign-in through the app stopping at once.
		if strings.EqualFold(c.Usage, "Verify") && kind == "servicePrincipals" {
			credKind = KindTrustAnchor
		}
		items = append(items, Item{
			Kind:      credKind,
			Name:      name + "/" + credName,
			Expires:   expires,
			Source:    src,
			Namespace: name,
			Labels:    labels,
		})
	}

	for _, c := range obj.PasswordCredentials {
		add(c, KindSecret, "client secret")
	}
	for _, c := range obj.KeyCredentials {
		add(c, KindTLSCert, "certificate")
	}
	return items
}

type azureVaultItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Expires int64 `json:"exp"` // Unix seconds, not RFC 3339
		Enabled *bool `json:"enabled"`
	} `json:"attributes"`
}

// keyVaults reads certificates, secrets and keys from each named vault.
//
// Vaults are named rather than discovered on purpose: enumerating them needs
// ARM subscription permissions, and a source whose whole promise is read-only
// data-plane access should not be asking for the control plane.
func (s *AzureSource) keyVaults(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string

	for _, vault := range s.Vaults {
		base := s.endpoint("vault", "https://"+vault+".vault.azure.net")
		for _, what := range []string{"certificates", "secrets", "keys"} {
			u := fmt.Sprintf("%s/%s?api-version=%s&maxresults=25", base, what, azureVaultAPIVer)
			page := 0
			for ; page < azureMaxPages && u != ""; page++ {
				var body struct {
					Value    []azureVaultItem `json:"value"`
					NextLink string           `json:"nextLink"`
				}
				if err := azureGet(ctx, s, client, u, azureVaultScope, &body); err != nil {
					warnings = append(warnings, vault+"/"+what+": "+err.Error())
					break
				}
				if body.NextLink != "" && !sameHost(body.NextLink, base) {
					warnings = append(warnings,
						vault+"/"+what+": the API returned a continuation URL on another host")
					break
				}
				for _, v := range body.Value {
					if v.Attributes.Expires <= 0 {
						continue // no expiry set means no deadline to miss
					}
					labels := map[string]string{"vault": vault}
					// A disabled object is not serving anything.
					if v.Attributes.Enabled != nil && !*v.Attributes.Enabled {
						labels[LabelInUse] = "false"
					}
					items = append(items, Item{
						Kind:      azureVaultKind(what),
						Name:      vault + "/" + shortGCPName(v.ID),
						Expires:   time.Unix(v.Attributes.Expires, 0).UTC(),
						Source:    "azure:keyvault",
						Namespace: vault,
						Labels:    labels,
					})
				}
				u = body.NextLink
			}
			if u != "" && page >= azureMaxPages {
				warnings = append(warnings, vault+"/"+what+": stopped after "+
					strconv.Itoa(azureMaxPages)+" pages; the rest were not read")
			}
		}
	}
	return items, joinErrs(warnings)
}

func azureVaultKind(what string) Kind {
	if what == "certificates" {
		return KindTLSCert
	}
	return KindSecret
}
