package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Endpoint is one host to probe over TLS.
type Endpoint struct {
	Host       string            `json:"host"`       // host or host:port (443 assumed)
	ServerName string            `json:"serverName"` // SNI override, when it differs from Host
	Labels     map[string]string `json:"labels"`     // operator hints for blast-radius ranking
}

// TLSSource dials each endpoint and reports the leaf certificate plus every
// intermediate in the presented chain. Intermediates are the point: nobody
// tracks them, and one expiring takes out every host it signed.
type TLSSource struct {
	Endpoints []Endpoint
	Timeout   time.Duration
}

func (s *TLSSource) Name() string { return "tls:endpoint" }

const tlsProbeConcurrency = 8

func (s *TLSSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	var (
		mu    sync.Mutex
		items []Item
		errs  []string
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, tlsProbeConcurrency)

	for _, ep := range s.Endpoints {
		wg.Add(1)
		go func(ep Endpoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			got, err := probe(ctx, ep, timeout)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", ep.Host, err))
				return
			}
			items = append(items, got...)
		}(ep)
	}
	wg.Wait()

	items = mergeIntermediates(items)
	sort.Slice(items, func(i, j int) bool { return items[i].Expires.Before(items[j].Expires) })

	if len(errs) > 0 && len(items) == 0 {
		return nil, fmt.Errorf("all endpoints failed: %s", strings.Join(errs, "; "))
	}
	if len(errs) > 0 {
		// Partial results beat no results: an unreachable host must not hide the
		// certs we did read.
		return items, fmt.Errorf("%d endpoint(s) failed: %s", len(errs), strings.Join(errs, "; "))
	}
	return items, nil
}

func probe(ctx context.Context, ep Endpoint, timeout time.Duration) ([]Item, error) {
	addr := ep.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	host, _, _ := net.SplitHostPort(addr)
	serverName := ep.ServerName
	if serverName == "" {
		serverName = host
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := &tls.Dialer{
		NetDialer: &net.Dialer{},
		// InsecureSkipVerify is deliberate and required: an already-expired
		// certificate is exactly what this tool exists to find, and verification
		// would reject the handshake before we could read it. Nothing is sent
		// over this connection and no trust decision is made from it.
		Config: &tls.Config{InsecureSkipVerify: true, ServerName: serverName}, //nolint:gosec // inspection only
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no certificates presented")
	}

	var items []Item
	for i, cert := range state.PeerCertificates {
		if i == 0 {
			items = append(items, leafItem(ep, host, cert))
			continue
		}
		if isSelfSigned(cert) {
			continue // a root in the chain is the trust store's problem, not an expiry to chase
		}
		items = append(items, intermediateItem(host, cert))
	}
	return items, nil
}

func leafItem(ep Endpoint, host string, cert *x509.Certificate) Item {
	labels := map[string]string{}
	for k, v := range ep.Labels {
		labels[k] = v
	}
	labels = label(labels, LabelHosts, strings.Join(cert.DNSNames, ","))
	labels = label(labels, LabelIssuer, cert.Issuer.CommonName)
	labels = label(labels, LabelSerial, cert.SerialNumber.String())
	if _, ok := labels[LabelPublic]; !ok {
		labels[LabelPublic] = "true" // we reached it from wherever this ran
	}
	return Item{
		Kind:    KindTLSCert,
		Name:    host,
		Expires: cert.NotAfter,
		Source:  "tls:endpoint",
		Labels:  labels,
	}
}

func intermediateItem(host string, cert *x509.Certificate) Item {
	name := cert.Subject.CommonName
	if name == "" {
		name = cert.Subject.String()
	}
	return Item{
		Kind:    KindIntermediate,
		Name:    name,
		Expires: cert.NotAfter,
		Source:  "tls:chain",
		Labels: map[string]string{
			LabelSerial: cert.SerialNumber.String(),
			LabelIssuer: cert.Issuer.CommonName,
			LabelHosts:  host,
		},
	}
}

func isSelfSigned(cert *x509.Certificate) bool {
	return cert.Subject.String() == cert.Issuer.String()
}

// One intermediate signs many hosts. Reporting it once, listing the hosts it
// covers, is both less noise and a better blast-radius signal than N copies.
func mergeIntermediates(items []Item) []Item {
	bySerial := map[string]int{}
	out := items[:0]
	for _, it := range items {
		if it.Kind != KindIntermediate {
			out = append(out, it)
			continue
		}
		key := it.Name + "/" + it.Labels[LabelSerial]
		if idx, seen := bySerial[key]; seen {
			hosts := splitHosts(out[idx].Labels[LabelHosts])
			hosts = appendUnique(hosts, splitHosts(it.Labels[LabelHosts])...)
			sort.Strings(hosts)
			out[idx].Labels[LabelHosts] = strings.Join(hosts, ",")
			continue
		}
		bySerial[key] = len(out)
		out = append(out, it)
	}
	return out
}

func splitHosts(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func appendUnique(dst []string, more ...string) []string {
	for _, m := range more {
		found := false
		for _, d := range dst {
			if d == m {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, m)
		}
	}
	return dst
}
