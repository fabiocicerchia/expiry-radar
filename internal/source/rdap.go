package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DomainSource reads registrar expiry over RDAP — the protocol that replaced
// WHOIS. It is JSON, unauthenticated, and rdap.org redirects to the right
// registry for the TLD, so this needs no registrar API keys and no scraping.
//
// A lapsed domain is the one expiry that takes everything down at once, which
// is why it ranks high by default.
type DomainSource struct {
	Domains   []string
	Bootstrap string // override the RDAP bootstrap service
	IANAWhois string // override the IANA WHOIS referral server (host[:port])
	Timeout   time.Duration
}

const (
	defaultRDAPBootstrap = "https://rdap.org"
	defaultIANAWhois     = "whois.iana.org:43"
)

// errNoRDAP marks "this TLD has no RDAP service", the one case worth retrying
// over WHOIS.
var errNoRDAP = errors.New("no RDAP service for this TLD")

func (s *DomainSource) Name() string { return "domain:rdap" }

func (s *DomainSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	base := s.Bootstrap
	if base == "" {
		base = defaultRDAPBootstrap
	}
	client := &http.Client{Timeout: timeout}

	var items []Item
	var errs []string
	servers := map[string]string{} // TLD -> WHOIS server, so a list of .it domains asks IANA once
	for _, domain := range s.Domains {
		src := "domain:rdap"
		expires, err := s.lookup(ctx, client, base, domain)
		if err != nil {
			// Only when RDAP has nothing for this TLD. A registry that answered
			// but withheld the date has given its answer; port 43 won't do better.
			if !errors.Is(err, errNoRDAP) {
				errs = append(errs, fmt.Sprintf("%s: %v", domain, err))
				continue
			}
			var werr error
			if expires, werr = s.whoisLookup(ctx, timeout, servers, domain); werr != nil {
				errs = append(errs, fmt.Sprintf("%s: %v (WHOIS fallback: %v)", domain, err, werr))
				continue
			}
			src = "domain:whois"
		}
		items = append(items, Item{
			Kind:    KindDomain,
			Name:    domain,
			Expires: expires,
			Source:  src,
			Labels:  map[string]string{LabelPublic: "true"},
		})
	}
	if len(errs) > 0 {
		return items, fmt.Errorf("%d domain(s) failed: %s", len(errs), strings.Join(errs, "; "))
	}
	return items, nil
}

func (s *DomainSource) lookup(ctx context.Context, client *http.Client, base, domain string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/domain/"+url.PathEscape(domain), nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		// Either the domain does not exist or — far more often — the TLD is one
		// of the ~240 with no RDAP service at all. Both are worth a WHOIS try.
		return time.Time{}, fmt.Errorf("RDAP lookup: %s: %w", resp.Status, errNoRDAP)
	}
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("RDAP lookup: %s", resp.Status)
	}
	var body rdapDomain
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, err
	}
	return body.expiration()
}

// whoisLookup is the fallback for TLDs with no RDAP service. IANA's WHOIS
// answers "which server serves .it", that server answers the domain, and the
// date has to be scraped out of free text — every registry formats it
// differently, and .de and .ch withhold it entirely. Hence: fallback, not
// primary.
func (s *DomainSource) whoisLookup(ctx context.Context, timeout time.Duration, servers map[string]string, domain string) (time.Time, error) {
	dot := strings.LastIndex(domain, ".")
	if dot < 0 {
		return time.Time{}, fmt.Errorf("no TLD in %q", domain)
	}
	tld := strings.ToLower(domain[dot+1:])

	server, ok := servers[tld]
	if !ok {
		iana := s.IANAWhois
		if iana == "" {
			iana = defaultIANAWhois
		}
		referral, err := whoisQuery(ctx, timeout, iana, tld)
		if err != nil {
			return time.Time{}, err
		}
		for _, line := range strings.Split(referral, "\n") {
			if v, found := strings.CutPrefix(strings.TrimSpace(line), "whois:"); found {
				server = strings.TrimSpace(v)
				break
			}
		}
		if server == "" {
			return time.Time{}, fmt.Errorf("IANA lists no WHOIS server for .%s", tld)
		}
		servers[tld] = server
	}

	text, err := whoisQuery(ctx, timeout, server, domain)
	if err != nil {
		return time.Time{}, err
	}
	return whoisExpiration(text)
}

func whoisQuery(ctx context.Context, timeout time.Duration, server, query string) (string, error) {
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "43")
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	// ponytail: plain query, no per-registry flags. A few registries want them
	// (.jp wants "/e" for English, DENIC "-T dn") — add per-TLD flags if those
	// ever need to work.
	if _, err := io.WriteString(conn, query+"\r\n"); err != nil {
		return "", err
	}
	body, err := io.ReadAll(io.LimitReader(conn, 1<<20))
	return string(body), err
}

// Matches "Expire Date:", "Registry Expiry Date:", "Expiry date:", "paid-till:",
// "Renewal date:", "valid until:" — the labels registries actually use.
var whoisExpiryLine = regexp.MustCompile(`(?i)^[a-z ]*(expir\w*|paid-till|renewal date|valid until)[a-z ()]*:\s*(\S+)`)

func whoisExpiration(text string) (time.Time, error) {
	for _, line := range strings.Split(text, "\n") {
		m := whoisExpiryLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02", "02-Jan-2006", "2006/01/02", "02.01.2006"} {
			if t, err := time.Parse(layout, m[2]); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no parseable expiry in WHOIS (.de and .ch never publish one)")
}

type rdapDomain struct {
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
}

func (d rdapDomain) expiration() (time.Time, error) {
	for _, e := range d.Events {
		if e.Action != "expiration" {
			continue
		}
		// Registries are inconsistent about the timezone suffix, so try the
		// strict form first and fall back to the common sloppy one.
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, e.Date); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unparsable expiration date %q", e.Date)
	}
	return time.Time{}, fmt.Errorf("no expiration event (some registries hide it from anonymous queries)")
}
