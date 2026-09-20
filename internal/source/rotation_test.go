package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rotationServer(body any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// The point of the whole source: these keys have no expiry, so the deadline is
// the operator's policy — and the report must never imply the provider said it.
func TestARotationDeadlineSaysItIsAPolicyNotAnIssuerDate(t *testing.T) {
	created := time.Now().Add(-60 * 24 * time.Hour)
	srv := rotationServer(map[string]any{"data": []any{
		map[string]any{"id": "k1", "name": "prod-key", "status": "active",
			"created_at": created.Format(time.RFC3339), "workspace_id": "w1"},
	}})
	defer srv.Close()

	s := &RotationSource{
		BaseURLs:  map[string]string{"anthropic": srv.URL},
		Providers: []RotationProvider{{Name: "anthropic", Token: "admin", MaxKeyAgeDays: 90}},
	}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the key: %+v", items)
	}
	it := items[0]
	if it.Labels["deadline"] != "rotation policy" {
		t.Errorf("deadline = %q; the report must not imply the provider stated this date",
			it.Labels["deadline"])
	}
	if it.Labels["policy.days"] != "90" || it.Labels["created"] == "" {
		t.Errorf("the labels must show where the date came from: %v", it.Labels)
	}
	// created + 90d, so about 30 days out from a 60-day-old key.
	if d := time.Until(it.Expires).Hours() / 24; d < 29 || d > 31 {
		t.Errorf("deadline is created + maxKeyAgeDays, got %.0f days out", d)
	}
}

// Docker Hub grew expiring tokens later than it grew tokens, so both shapes
// are live and the label has to say which one a row is.
func TestAnIssuerStatedExpiryIsNotLabelledAPolicy(t *testing.T) {
	created := time.Now().Add(-10 * 24 * time.Hour)
	stated := time.Now().Add(40 * 24 * time.Hour)
	srv := rotationServer(map[string]any{"results": []any{
		map[string]any{"uuid": "u1", "token_label": "bounded", "is_active": true,
			"created_at": created.Format(time.RFC3339),
			"expires_at": stated.Format(time.RFC3339)},
		map[string]any{"uuid": "u2", "token_label": "forever", "is_active": true,
			"created_at": created.Format(time.RFC3339)},
	}})
	defer srv.Close()

	s := &RotationSource{
		BaseURLs:  map[string]string{"dockerhub": srv.URL},
		Providers: []RotationProvider{{Name: "dockerhub", Token: "t", MaxKeyAgeDays: 30}},
	}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	bounded, _ := itemNamed(items, "bounded")
	forever, _ := itemNamed(items, "forever")
	if bounded.Labels["deadline"] != "issuer" {
		t.Errorf("a token that states its own expiry is an issuer date, got %q", bounded.Labels["deadline"])
	}
	if !bounded.Expires.Equal(stated.Truncate(time.Second)) && bounded.Expires.Unix() != stated.Unix() {
		t.Errorf("the issuer's date must be used as-is, got %v", bounded.Expires)
	}
	if forever.Labels["deadline"] != "rotation policy" {
		t.Errorf("a token with no expiry falls back to policy, got %q", forever.Labels["deadline"])
	}
}

// OpenAI stamps in Unix seconds rather than RFC 3339, which is the one real
// difference between the three adapters.
func TestOpenAIUnixTimestampsAreRead(t *testing.T) {
	created := time.Now().Add(-100 * 24 * time.Hour)
	srv := rotationServer(map[string]any{"data": []any{
		map[string]any{"id": "k1", "name": "used", "created_at": created.Unix(),
			"last_used_at": time.Now().Add(-24 * time.Hour).Unix()},
		map[string]any{"id": "k2", "name": "never-used", "created_at": created.Unix()},
	}})
	defer srv.Close()

	s := &RotationSource{
		BaseURLs:  map[string]string{"openai": srv.URL},
		Providers: []RotationProvider{{Name: "openai", Token: "admin", MaxKeyAgeDays: 90}},
	}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	used, _ := itemNamed(items, "used")
	if used.Expires.IsZero() || used.Labels["last-used"] == "" {
		t.Errorf("the Unix timestamps should have parsed: %+v", used)
	}
	never, _ := itemNamed(items, "never-used")
	if never.Labels[LabelInUse] != "false" {
		t.Error("a key never used and never expiring is one to delete, not rotate")
	}
}

func TestARotationProviderWithNoPolicyIsRejected(t *testing.T) {
	err := ValidateRotation([]RotationProvider{{Name: "anthropic"}})
	if err == nil || !strings.Contains(err.Error(), "maxKeyAgeDays") {
		t.Fatalf("a deadline nobody chose is not a policy, got %v", err)
	}
	if err := ValidateRotation([]RotationProvider{{Name: "notaprovider", MaxKeyAgeDays: 1}}); err == nil ||
		!strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("an unknown provider must be rejected at load, got %v", err)
	}
	if err := ValidateRotation([]RotationProvider{{Name: "openai", MaxKeyAgeDays: 30}}); err != nil {
		t.Fatalf("a valid provider must load: %v", err)
	}
}

func TestOneUnreadableRotationProviderKeepsTheOthers(t *testing.T) {
	created := time.Now().Add(-5 * 24 * time.Hour)
	ok := rotationServer(map[string]any{"data": []any{
		map[string]any{"id": "k1", "name": "fine", "status": "active",
			"created_at": created.Format(time.RFC3339)},
	}})
	defer ok.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer denied.Close()

	s := &RotationSource{
		BaseURLs: map[string]string{"anthropic": ok.URL, "openai": denied.URL},
		Providers: []RotationProvider{
			{Name: "anthropic", Token: "a", MaxKeyAgeDays: 90},
			{Name: "openai", Token: "o", MaxKeyAgeDays: 90},
		},
	}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("the denied provider must still be reported")
	}
	if !strings.Contains(err.Error(), "admin credential") {
		t.Errorf("the error should say what kind of credential is needed, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("one denied provider lost the other's keys: %+v", items)
	}
}

func TestAnInactiveKeyIsNotADeadline(t *testing.T) {
	created := time.Now().Add(-200 * 24 * time.Hour)
	srv := rotationServer(map[string]any{"data": []any{
		map[string]any{"id": "k1", "name": "retired", "status": "inactive",
			"created_at": created.Format(time.RFC3339)},
	}})
	defer srv.Close()

	s := &RotationSource{
		BaseURLs:  map[string]string{"anthropic": srv.URL},
		Providers: []RotationProvider{{Name: "anthropic", Token: "a", MaxKeyAgeDays: 90}},
	}
	items, _ := s.Collect(context.Background())
	if len(items) != 1 || items[0].Labels[LabelInUse] != "false" {
		t.Fatalf("an inactive key has already stopped working: %+v", items)
	}
}
