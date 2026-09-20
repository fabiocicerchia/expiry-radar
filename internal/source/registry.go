package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HarborSource reports robot account expiry.
//
// Harbor is the Docker Hub competitor worth watching, because unlike Docker Hub
// its robot accounts carry a real expiry and are routinely created once for a
// pipeline and then forgotten. When one lapses, deploys stop pulling images and
// the error surfaces in the cluster rather than anywhere near the registry.
type HarborSource struct {
	// BaseURL is the Harbor instance, e.g. https://registry.example.com.
	BaseURL string
	// Username and Password are basic-auth; Password never comes from the
	// config file and Load fills it from $HARBOR_PASSWORD.
	Username string
	Password string
	Timeout  time.Duration
}

// Name identifies this source in an item's Source field.
func (s *HarborSource) Name() string { return "harbor" }

// Collect reads the instance's robot accounts.
func (s *HarborSource) Collect(ctx context.Context) ([]Item, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("harbor source needs baseUrl, e.g. https://registry.example.com")
	}
	if s.Username == "" || s.Password == "" {
		return nil, fmt.Errorf("harbor source needs username and $HARBOR_PASSWORD")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	type harborRobot struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		// Unix seconds. Harbor uses -1 for "never expires", which is not a
		// deadline and must not be read as one in 1969.
		ExpiresAt int64  `json:"expires_at"`
		Disable   bool   `json:"disable"`
		Level     string `json:"level"`
	}

	const pageSize = 100
	client := &http.Client{Timeout: timeout}
	headers := map[string]string{"Authorization": basicAuth(s.Username, s.Password)}

	var robots []harborRobot
	var truncated error
	// Paged until a short page: an instance with more robots than one page
	// would otherwise report the first hundred as though that were all of them.
	for page := 1; page <= 40; page++ {
		var batch []harborRobot
		u := fmt.Sprintf("%s/api/v2.0/robots?page=%d&page_size=%d",
			strings.TrimSuffix(s.BaseURL, "/"), page, pageSize)
		if err := getJSON(ctx, client, u, headers,
			"listing robot accounts needs a Harbor administrator", &batch); err != nil {
			return nil, err
		}
		robots = append(robots, batch...)
		if len(batch) < pageSize {
			break
		}
		if page == 40 {
			truncated = fmt.Errorf("stopped after %d pages; the rest were not read", page)
		}
	}

	var items []Item
	for _, r := range robots {
		if r.ExpiresAt <= 0 {
			continue // -1 is Harbor's "never"; 0 is unset
		}
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("robot-%d", r.ID)
		}
		labels := map[string]string{}
		labels = label(labels, "level", r.Level)
		labels = label(labels, "description", r.Description)
		// A disabled robot has already stopped pulling.
		if r.Disable {
			labels[LabelInUse] = "false"
		}
		// A system-level robot reaches every project on the instance.
		if strings.EqualFold(r.Level, "system") {
			labels = label(labels, LabelBlastRadius, "0.75")
		}
		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    name,
			Expires: time.Unix(r.ExpiresAt, 0).UTC(),
			Source:  "harbor:robot",
			Labels:  labels,
		})
	}
	return items, truncated
}

// JFrogSource reports Artifactory access tokens.
//
// The other registry credential that genuinely expires. Like Harbor's robots,
// these are created for a pipeline and then nobody looks at them again.
type JFrogSource struct {
	// BaseURL is the platform, e.g. https://acme.jfrog.io.
	BaseURL string
	// Token never comes from the config file; Load fills it from
	// $JFROG_ACCESS_TOKEN.
	Token   string
	Timeout time.Duration
}

// Name identifies this source in an item's Source field.
func (s *JFrogSource) Name() string { return "jfrog" }

// Collect reads the platform's access tokens.
func (s *JFrogSource) Collect(ctx context.Context) ([]Item, error) {
	if s.BaseURL == "" {
		return nil, fmt.Errorf("jfrog source needs baseUrl, e.g. https://acme.jfrog.io")
	}
	if s.Token == "" {
		return nil, fmt.Errorf("jfrog source is enabled but $JFROG_ACCESS_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	var body struct {
		Tokens []struct {
			TokenID     string `json:"token_id"`
			Subject     string `json:"subject"`
			Description string `json:"description"`
			// Unix seconds; absent means a non-expiring token.
			Expiry   int64  `json:"expiry"`
			IssuedAt int64  `json:"issued_at"`
			Scope    string `json:"scope"`
		} `json:"tokens"`
	}
	u := strings.TrimSuffix(s.BaseURL, "/") + "/access/api/v1/tokens"
	err := getJSON(ctx, &http.Client{Timeout: timeout}, u,
		map[string]string{"Authorization": "Bearer " + s.Token},
		"listing tokens needs an admin access token", &body)
	if err != nil {
		return nil, err
	}

	var items []Item
	for _, t := range body.Tokens {
		if t.Expiry <= 0 {
			continue // a non-expiring token has no deadline to miss
		}
		name := t.Description
		if name == "" {
			name = t.Subject
		}
		if name == "" {
			name = t.TokenID
		}
		labels := map[string]string{}
		labels = label(labels, "scope", t.Scope)
		labels = label(labels, "subject", t.Subject)
		if t.IssuedAt > 0 {
			labels = label(labels, "created", time.Unix(t.IssuedAt, 0).UTC().Format(time.RFC3339))
		}
		// An admin-scoped token can do anything the platform can.
		if strings.Contains(strings.ToLower(t.Scope), "admin") {
			labels = label(labels, LabelBlastRadius, "0.85")
		}
		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    name,
			Expires: time.Unix(t.Expiry, 0).UTC(),
			Source:  "jfrog:token",
			Labels:  labels,
		})
	}
	return items, nil
}
