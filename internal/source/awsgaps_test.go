package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The four new services are separate units behind the same seam as the
// original three, so an account that denies one still reports the others.
func TestTheNewAWSServicesAreTheirOwnUnits(t *testing.T) {
	s := &AWSSource{SkipPCA: true}
	names := map[string]bool{}
	skipped := map[string]bool{}
	for _, u := range s.services(context.Background(), awsConfigFor("http://127.0.0.1:1"), "acct") {
		names[u.Name] = true
		skipped[u.Name] = u.Skipped
	}
	for _, want := range []string{"acm", "iam", "secretsmanager", "rds", "acm-pca", "iam-certs", "route53domains"} {
		if !names[want] {
			t.Errorf("%s is not collected", want)
		}
	}
	if !skipped["acm-pca"] {
		t.Error("skipPCA should skip only acm-pca")
	}
	if skipped["rds"] || skipped["route53domains"] {
		t.Error("skipping one service must not skip the others")
	}
}

// route53domains speaks JSON 1.1, so the fake can answer it directly.
func TestRoute53DomainsAutoRenewIsDeRanked(t *testing.T) {
	expiry := time.Now().Add(45 * 24 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Domains": []any{
				map[string]any{"DomainName": "auto.example", "Expiry": expiry.Unix(), "AutoRenew": true},
				map[string]any{"DomainName": "manual.example", "Expiry": expiry.Unix(), "AutoRenew": false},
			},
		})
	}))
	defer srv.Close()

	s := &AWSSource{}
	items, err := s.route53Domains(context.Background(), awsConfigFor(srv.URL), "acct")
	if err != nil {
		t.Fatalf("route53domains: %v", err)
	}
	auto, ok := itemNamed(items, "auto.example")
	if !ok {
		t.Fatalf("want both domains, got %+v", items)
	}
	manual, _ := itemNamed(items, "manual.example")
	if auto.Kind != KindDomain {
		t.Errorf("kind = %q, want %q", auto.Kind, KindDomain)
	}
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("auto-renew is what a registrar knows that RDAP does not")
	}
	if manual.Labels[LabelRenewal] == RenewalManaged {
		t.Error("a domain nobody renews must keep its full blast radius")
	}
}

// An expired private CA invalidates everything it ever signed at once, which
// is the definition of the trust_anchor kind.
func TestPrivateCAsAreTrustAnchors(t *testing.T) {
	notAfter := time.Now().Add(120 * 24 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"CertificateAuthorities": []any{
				map[string]any{
					"Arn": "arn:aws:acm-pca:eu-west-1:1:certificate-authority/abc",
					"CertificateAuthorityConfiguration": map[string]any{
						"Subject": map[string]any{"CommonName": "payments-issuing-ca"},
					},
					"NotAfter": notAfter.Unix(), "Status": "ACTIVE", "Type": "SUBORDINATE",
				},
				map[string]any{
					"Arn":      "arn:aws:acm-pca:eu-west-1:1:certificate-authority/old",
					"NotAfter": notAfter.Unix(), "Status": "DISABLED", "Type": "ROOT",
				},
			},
		})
	}))
	defer srv.Close()

	items, err := (&AWSSource{}).privateCAs(context.Background(), awsConfigFor(srv.URL), "acct")
	if err != nil {
		t.Fatalf("acm-pca: %v", err)
	}
	active, ok := itemNamed(items, "private-ca/payments-issuing-ca")
	if !ok {
		t.Fatalf("want the named CA, got %+v", items)
	}
	if active.Kind != KindTrustAnchor {
		t.Errorf("kind = %q, want %q", active.Kind, KindTrustAnchor)
	}
	disabled, _ := itemNamed(items, "private-ca/arn:aws:acm-pca:eu-west-1:1:certificate-authority/old")
	if disabled.Labels[LabelInUse] != "false" {
		t.Error("a disabled CA is not signing anything")
	}
}

func TestRDSReportsBothTheBundleAndWhatEachInstanceIsPinnedTo(t *testing.T) {
	validTill := time.Now().Add(200 * 24 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "text/xml")
		switch r.FormValue("Action") {
		case "DescribeCertificates":
			_, _ = w.Write([]byte(`<DescribeCertificatesResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
<DescribeCertificatesResult><Certificates>
<Certificate><CertificateIdentifier>rds-ca-rsa2048-g1</CertificateIdentifier>
<CertificateType>CA</CertificateType><CustomerOverride>false</CustomerOverride>
<ValidTill>` + validTill.UTC().Format(time.RFC3339) + `</ValidTill></Certificate>
</Certificates></DescribeCertificatesResult></DescribeCertificatesResponse>`))
		case "DescribeDBInstances":
			_, _ = w.Write([]byte(`<DescribeDBInstancesResponse xmlns="http://rds.amazonaws.com/doc/2014-10-31/">
<DescribeDBInstancesResult><DBInstances>
<DBInstance><DBInstanceIdentifier>payments</DBInstanceIdentifier><Engine>postgres</Engine>
<PubliclyAccessible>true</PubliclyAccessible>
<CertificateDetails><CAIdentifier>rds-ca-rsa2048-g1</CAIdentifier>
<ValidTill>` + validTill.UTC().Format(time.RFC3339) + `</ValidTill></CertificateDetails>
</DBInstance></DBInstances></DescribeDBInstancesResult></DescribeDBInstancesResponse>`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	items, err := (&AWSSource{}).rdsCertificates(context.Background(), awsConfigFor(srv.URL), "acct")
	if err != nil {
		t.Fatalf("rds: %v", err)
	}
	bundle, ok := itemNamed(items, "rds-ca/rds-ca-rsa2048-g1")
	if !ok {
		t.Fatalf("want the regional bundle, got %+v", items)
	}
	if bundle.Labels[LabelInUse] != "false" {
		t.Error("a CA that is not the account default has nothing pinned to it")
	}
	inst, ok := itemNamed(items, "rds/payments")
	if !ok {
		t.Fatalf("want the instance's own certificate, got %+v", items)
	}
	if inst.Labels[LabelPublic] != "true" {
		t.Error("RDS states publicly-accessible outright; it should not be inferred")
	}
	if inst.Labels["ca"] != "rds-ca-rsa2048-g1" {
		t.Errorf("the instance should record which CA it presents, got %v", inst.Labels)
	}
}
