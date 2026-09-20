package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func namecheapServer(bodies map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		if b, ok := bodies[r.URL.Query().Get("Command")]; ok {
			_, _ = w.Write([]byte(b))
			return
		}
		_, _ = w.Write([]byte(`<ApiResponse Status="OK"><CommandResponse/></ApiResponse>`))
	}))
}

func ncSoon() string { return time.Now().Add(60 * 24 * time.Hour).Format("01/02/2006") }

func ncSource(url string) *NamecheapSource {
	return &NamecheapSource{APIUser: "u", UserName: "u", APIKey: "k", ClientIP: "1.2.3.4", BaseURL: url}
}

// RDAP already knows the date. AutoRenew is the only thing a registrar
// credential buys, so it had better be read correctly.
func TestNamecheapAutoRenewIsWhatTheCredentialBuys(t *testing.T) {
	srv := namecheapServer(map[string]string{
		"namecheap.domains.getList": `<ApiResponse Status="OK"><CommandResponse><DomainGetListResult>
<Domain Name="auto.example" Expires="` + ncSoon() + `" AutoRenew="true" IsExpired="false" IsLocked="true"/>
<Domain Name="manual.example" Expires="` + ncSoon() + `" AutoRenew="false" IsExpired="false"/>
</DomainGetListResult></CommandResponse></ApiResponse>`,
	})
	defer srv.Close()

	items, err := ncSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	auto, ok := itemNamed(items, "auto.example")
	if !ok {
		t.Fatalf("want both domains: %+v", items)
	}
	manual, _ := itemNamed(items, "manual.example")
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("auto-renew on means somebody else is meeting this date")
	}
	if manual.Labels[LabelRenewal] == RenewalManaged {
		t.Error("auto-renew off is exactly the domain that lapses")
	}
	if auto.Labels["transfer-lock"] != "true" {
		t.Errorf("the lock state should be recorded: %v", auto.Labels)
	}
	// MM/DD/YYYY, not DD/MM/YYYY — getting this backwards silently shifts
	// every date by up to eleven months.
	if d := time.Until(auto.Expires).Hours() / 24; d < 58 || d > 61 {
		t.Errorf("date parsed as %v, about %.0f days out", auto.Expires, d)
	}
}

// Namecheap answers HTTP 200 for failures, so the status code alone would read
// every error as an empty account.
func TestNamecheapErrorsArriveAsHTTP200AndMustNotReadAsEmpty(t *testing.T) {
	srv := namecheapServer(map[string]string{
		"namecheap.domains.getList": `<ApiResponse Status="ERROR"><Errors>
<Error Number="1011102">API Key is invalid or API access has not been enabled</Error>
</Errors></ApiResponse>`,
	})
	defer srv.Close()

	_, err := ncSource(srv.URL).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1011102") {
		t.Fatalf("a 200 carrying Status=ERROR must not read as success, got %v", err)
	}
}

// 1011150 looks like bad credentials and is not — it is the machine's IP.
// Saying so is the difference between a five-minute fix and an afternoon.
func TestNamecheapExplainsTheAllowlistError(t *testing.T) {
	srv := namecheapServer(map[string]string{
		"namecheap.domains.getList": `<ApiResponse Status="ERROR"><Errors>
<Error Number="1011150">Parameter RequestIP is invalid</Error></Errors></ApiResponse>`,
	})
	defer srv.Close()

	_, err := ncSource(srv.URL).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "allowlisted") {
		t.Fatalf("the allowlist case must say what it actually is, got %v", err)
	}
}

func TestNamecheapUnissuedCertificatesAreNotDeadlines(t *testing.T) {
	srv := namecheapServer(map[string]string{
		"namecheap.ssl.getList": `<ApiResponse Status="OK"><CommandResponse><SSLListResult>
<SSL CertificateID="1" HostName="shop.example.com" SSLType="PositiveSSL" Status="active" ExpireDate="` + ncSoon() + `"/>
<SSL CertificateID="2" HostName="" SSLType="PositiveSSL" Status="newpurchase" ExpireDate="` + ncSoon() + `"/>
</SSLListResult></CommandResponse></ApiResponse>`,
	})
	defer srv.Close()

	s := ncSource(srv.URL)
	s.SkipDomains = true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	live, _ := itemNamed(items, "shop.example.com")
	if live.Labels[LabelInUse] == "false" {
		t.Error("an active certificate is serving something")
	}
	unissued, _ := itemNamed(items, "ssl/2")
	if unissued.Labels[LabelInUse] != "false" {
		t.Error("a certificate bought but never issued is not serving anything")
	}
}

func TestNamecheapNeedsItsAllowlistedIPBeforeRunning(t *testing.T) {
	s := &NamecheapSource{APIUser: "u", UserName: "u", APIKey: "k"}
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "clientIp") {
		t.Fatalf("want the constraint named up front, got %v", err)
	}
}
