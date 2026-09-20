package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// getJSON is the shape most REST provider sources share: one authenticated
// GET, a status check that turns a denial into a sentence naming what the
// credential is missing, and a JSON decode.
//
// The sources that do not use it differ for a reason rather than by accident:
// Cloudflare has to check a `success` field because it returns 200 on failure,
// GitLab paginates by header and answers 404 for "not visible to you", GitHub
// carries its own API-version header, and Namecheap is XML. Everything else
// should come through here so there is one place the status handling lives.
func getJSON(ctx context.Context, client *http.Client, url string,
	headers map[string]string, deniedHint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Wrapped rather than returned raw: *url.Error prints the URL, and a
		// source that ever puts a credential in one should not be one edit
		// away from printing it.
		return fmt.Errorf("GET %s: %w", url, err)
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("GET %s: %s — %s", url, resp.Status, deniedHint)
	default:
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

// basicAuth builds the header value. Written out rather than using
// http.Request.SetBasicAuth so the credential goes through the same headers map
// every other source uses, and never near a URL.
func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}
