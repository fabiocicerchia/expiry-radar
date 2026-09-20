package source

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NamecheapSource reports registered domains and resold SSL certificates.
//
// Two things make this source unlike the others. It speaks XML over a single
// query endpoint rather than REST, and it requires the calling host's public IP
// to be allowlisted in the Namecheap account first — a 1011150 error means the
// credentials are fine and the machine is not. That is a deployment constraint,
// not a bug, and it is worth knowing before wiring this into CI.
//
// Its value over the credential-free `domains` source is not the date. RDAP
// already reports registry expiry for any domain. It is `AutoRenew`: the
// registrar is the only party that knows whether the renewal is actually going
// to happen, which is the difference between a date and a deadline.
type NamecheapSource struct {
	APIUser  string
	UserName string
	// APIKey never comes from the config file; Load fills it from
	// $NAMECHEAP_API_KEY.
	APIKey string
	// ClientIP must be the allowlisted public IP of the machine running this.
	ClientIP string
	// Sandbox points at api.sandbox.namecheap.com, which has its own accounts
	// and its own allowlist.
	Sandbox bool
	BaseURL string // test seam; overrides both hosts
	Timeout time.Duration

	SkipDomains bool
	SkipSSL     bool
}

const (
	namecheapAPI        = "https://api.namecheap.com/xml.response"
	namecheapSandboxAPI = "https://api.sandbox.namecheap.com/xml.response"
	// Namecheap prints dates as MM/DD/YYYY, with no timezone. Treated as the
	// start of that day in UTC, which errs towards warning early — the same
	// choice manual items make for a bare date.
	namecheapDate = "01/02/2006"
)

// Name identifies this source in an item's Source field.
func (s *NamecheapSource) Name() string { return "namecheap" }

// Collect reads the account's domains and SSL certificates.
func (s *NamecheapSource) Collect(ctx context.Context) ([]Item, error) {
	if s.APIKey == "" {
		return nil, fmt.Errorf("namecheap source is enabled but $NAMECHEAP_API_KEY is not set")
	}
	if s.APIUser == "" || s.UserName == "" || s.ClientIP == "" {
		return nil, fmt.Errorf(
			"namecheap source needs apiUser, userName and clientIp — clientIp must be the allowlisted public IP of this machine")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"domains", s.SkipDomains, func() ([]Item, error) { return s.domains(ctx, client) }},
		{"ssl", s.SkipSSL, func() ([]Item, error) { return s.sslCertificates(ctx, client) }},
	})
	return items, err
}

// ncResponse is the envelope every command returns. Status is an attribute, and
// an error response is still HTTP 200, so the attribute is the only thing that
// says whether the call worked.
type ncResponse struct {
	Status string `xml:"Status,attr"`
	Errors struct {
		Error []struct {
			Number string `xml:"Number,attr"`
			Text   string `xml:",chardata"`
		} `xml:"Error"`
	} `xml:"Errors"`
	CommandResponse struct {
		DomainGetListResult struct {
			Domain []ncDomain `xml:"Domain"`
		} `xml:"DomainGetListResult"`
		SSLListResult struct {
			SSL []ncSSL `xml:"SSL"`
		} `xml:"SSLListResult"`
	} `xml:"CommandResponse"`
}

type ncDomain struct {
	Name      string `xml:"Name,attr"`
	Expires   string `xml:"Expires,attr"`
	IsExpired string `xml:"IsExpired,attr"`
	IsLocked  string `xml:"IsLocked,attr"`
	AutoRenew string `xml:"AutoRenew,attr"`
}

type ncSSL struct {
	CertificateID string `xml:"CertificateID,attr"`
	HostName      string `xml:"HostName,attr"`
	SSLType       string `xml:"SSLType,attr"`
	ExpireDate    string `xml:"ExpireDate,attr"`
	Status        string `xml:"Status,attr"`
}

func (s *NamecheapSource) call(ctx context.Context, client *http.Client, command string) (*ncResponse, error) {
	base := s.BaseURL
	if base == "" {
		base = namecheapAPI
		if s.Sandbox {
			base = namecheapSandboxAPI
		}
	}
	q := url.Values{}
	q.Set("ApiUser", s.APIUser)
	q.Set("ApiKey", s.APIKey)
	q.Set("UserName", s.UserName)
	q.Set("ClientIp", s.ClientIP)
	q.Set("Command", command)
	q.Set("PageSize", "100")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", command, resp.Status)
	}

	var body ncResponse
	if err := xml.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s: %w", command, err)
	}
	// An error here is HTTP 200 with Status="ERROR", so the status code alone
	// would read every failure as success.
	if !strings.EqualFold(body.Status, "OK") {
		return nil, fmt.Errorf("%s: %s", command, ncError(body))
	}
	return &body, nil
}

// ncError turns the error list into something actionable, and calls out the
// allowlist case by number because it is the one that looks like bad
// credentials and is not.
func ncError(body ncResponse) string {
	if len(body.Errors.Error) == 0 {
		return "the API reported an error with no detail"
	}
	msgs := make([]string, 0, len(body.Errors.Error))
	for _, e := range body.Errors.Error {
		msg := e.Number + " " + strings.TrimSpace(e.Text)
		if e.Number == "1011150" {
			msg += " (this machine's public IP is not allowlisted in the Namecheap account)"
		}
		msgs = append(msgs, msg)
	}
	return strings.Join(msgs, "; ")
}

func (s *NamecheapSource) domains(ctx context.Context, client *http.Client) ([]Item, error) {
	body, err := s.call(ctx, client, "namecheap.domains.getList")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, d := range body.CommandResponse.DomainGetListResult.Domain {
		expires, ok := ncTime(d.Expires)
		if !ok || d.Name == "" {
			continue
		}
		labels := map[string]string{LabelPublic: "true"}
		// The whole reason to hold a registrar credential: RDAP knows the
		// date, only the registrar knows whether anybody will act on it.
		if strings.EqualFold(d.AutoRenew, "true") {
			labels[LabelRenewal] = RenewalManaged
		}
		if strings.EqualFold(d.IsExpired, "true") {
			labels = label(labels, "expired", "true")
		}
		if strings.EqualFold(d.IsLocked, "true") {
			labels = label(labels, "transfer-lock", "true")
		}
		items = append(items, Item{
			Kind:      KindDomain,
			Name:      d.Name,
			Expires:   expires,
			Source:    "namecheap:domain",
			Namespace: d.Name,
			Labels:    labels,
		})
	}
	return items, nil
}

func (s *NamecheapSource) sslCertificates(ctx context.Context, client *http.Client) ([]Item, error) {
	body, err := s.call(ctx, client, "namecheap.ssl.getList")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, c := range body.CommandResponse.SSLListResult.SSL {
		expires, ok := ncTime(c.ExpireDate)
		if !ok {
			continue
		}
		name := c.HostName
		if name == "" {
			name = "ssl/" + c.CertificateID
		}
		labels := map[string]string{LabelPublic: "true"}
		labels = label(labels, LabelHosts, c.HostName)
		labels = label(labels, "ssl-type", c.SSLType)
		labels = label(labels, "status", c.Status)
		// A certificate that was bought but never issued is not serving
		// anything, so its date is not a deadline.
		if !strings.EqualFold(c.Status, "active") {
			labels[LabelInUse] = "false"
		}
		items = append(items, Item{
			Kind:    KindTLSCert,
			Name:    name,
			Expires: expires,
			Source:  "namecheap:ssl",
			Labels:  labels,
		})
	}
	return items, nil
}

func ncTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(namecheapDate, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
