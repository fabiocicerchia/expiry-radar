package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	cloudflareAPI      = "https://api.cloudflare.com/client/v4"
	cloudflarePageSize = 50
	// A page cap, not a result cap: pagination that never terminates is a bug
	// somewhere, and looping forever is a worse failure than reporting less.
	cloudflareMaxPages = 40
)

// cfResult is the envelope every Cloudflare endpoint returns. `success` is
// checked rather than the status code alone: the API answers 200 with
// success=false often enough that trusting the code would swallow errors.
type cfResult[T any] struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     []T `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// cfGet reads every page of a list endpoint. A free function rather than a
// method because Go does not allow type parameters on methods.
func cfGet[T any](ctx context.Context, s *CloudflareSource, client *http.Client, path string) ([]T, error) {
	base := s.BaseURL
	if base == "" {
		base = cloudflareAPI
	}
	base = strings.TrimSuffix(base, "/")

	var out []T
	for page := 1; page <= cloudflareMaxPages; page++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u := base + path + sep +
			"page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(cloudflarePageSize)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		var body cfResult[T]
		decErr := json.NewDecoder(resp.Body).Decode(&body)
		//nolint:errcheck // the body is read or abandoned either way.
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return out, fmt.Errorf("GET %s: %s — the API token is missing a read permission for this resource",
				path, resp.Status)
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		if decErr != nil {
			return out, fmt.Errorf("GET %s: %w", path, decErr)
		}
		if !body.Success {
			return out, fmt.Errorf("GET %s: %s", path, cfErrors(body.Errors))
		}

		out = append(out, body.Result...)
		if body.ResultInfo.TotalPages <= page || len(body.Result) == 0 {
			return out, nil
		}
	}
	return out, fmt.Errorf("GET %s: stopped after %d pages", path, cloudflareMaxPages)
}

func cfErrors(errs []struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) string {
	if len(errs) == 0 {
		return "the API reported failure with no error detail"
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
	}
	return strings.Join(msgs, "; ")
}
