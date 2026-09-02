package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// staticCreds is enough to make the SDK sign a request; the fake endpoint below
// never checks the signature. Written out rather than pulled from the
// credentials module so the test needs no direct dependency the tool does not
// already have.
type staticCreds struct{}

func (staticCreds) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "AKIATEST", SecretAccessKey: "secret", Source: "test"}, nil
}

// awsConfigFor points a real SDK client at a local fake, so the IAM code under
// test is the code that runs against AWS — not a re-implementation of it.
func awsConfigFor(url string) aws.Config {
	return aws.Config{
		Region:       "eu-west-1",
		Credentials:  staticCreds{},
		BaseEndpoint: aws.String(url),
	}
}

const iamXMLNS = "https://iam.amazonaws.com/doc/2010-05-08/"

// fakeIAM answers the two calls AWSSource.iam makes, over two pages each, so
// both paginator loops are exercised: a single-page fake would leave the
// "there is more" branch untested, which is exactly where a paging bug lives.
func fakeIAM(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing the IAM request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		switch action, marker := r.Form.Get("Action"), r.Form.Get("Marker"); {
		case action == "ListUsers" && marker == "":
			writeXML(w, `<ListUsersResponse xmlns="`+iamXMLNS+`"><ListUsersResult>
				<IsTruncated>true</IsTruncated><Marker>page2</Marker>
				<Users><member><Path>/</Path><UserName>alice</UserName><UserId>AIDA1</UserId>
				<Arn>arn:aws:iam::123456789012:user/alice</Arn>
				<CreateDate>2020-01-01T00:00:00Z</CreateDate></member></Users>
				</ListUsersResult></ListUsersResponse>`)
		case action == "ListUsers":
			writeXML(w, `<ListUsersResponse xmlns="`+iamXMLNS+`"><ListUsersResult>
				<IsTruncated>false</IsTruncated>
				<Users><member><Path>/</Path><UserName>bob</UserName><UserId>AIDA2</UserId>
				<Arn>arn:aws:iam::123456789012:user/bob</Arn>
				<CreateDate>2020-01-01T00:00:00Z</CreateDate></member></Users>
				</ListUsersResult></ListUsersResponse>`)
		case action == "ListAccessKeys" && r.Form.Get("UserName") == "alice" && marker == "":
			// One usable key, one inactive key that must not be reported, and
			// another page to walk.
			writeXML(w, `<ListAccessKeysResponse xmlns="`+iamXMLNS+`"><ListAccessKeysResult>
				<UserName>alice</UserName><IsTruncated>true</IsTruncated><Marker>keys2</Marker>
				<AccessKeyMetadata>
				<member><UserName>alice</UserName><AccessKeyId>AKIAALICE1</AccessKeyId>
				<Status>Active</Status><CreateDate>2026-01-01T00:00:00Z</CreateDate></member>
				<member><UserName>alice</UserName><AccessKeyId>AKIAALICE2</AccessKeyId>
				<Status>Inactive</Status><CreateDate>2026-01-01T00:00:00Z</CreateDate></member>
				</AccessKeyMetadata></ListAccessKeysResult></ListAccessKeysResponse>`)
		case action == "ListAccessKeys" && r.Form.Get("UserName") == "alice":
			writeXML(w, `<ListAccessKeysResponse xmlns="`+iamXMLNS+`"><ListAccessKeysResult>
				<UserName>alice</UserName><IsTruncated>false</IsTruncated>
				<AccessKeyMetadata>
				<member><UserName>alice</UserName><AccessKeyId>AKIAALICE3</AccessKeyId>
				<Status>Active</Status><CreateDate>2026-02-01T00:00:00Z</CreateDate></member>
				</AccessKeyMetadata></ListAccessKeysResult></ListAccessKeysResponse>`)
		case action == "ListAccessKeys":
			writeXML(w, `<ListAccessKeysResponse xmlns="`+iamXMLNS+`"><ListAccessKeysResult>
				<UserName>bob</UserName><IsTruncated>false</IsTruncated>
				<AccessKeyMetadata>
				<member><UserName>bob</UserName><AccessKeyId>AKIABOB1</AccessKeyId>
				<Status>Active</Status><CreateDate>2026-03-01T00:00:00Z</CreateDate></member>
				</AccessKeyMetadata></ListAccessKeysResult></ListAccessKeysResponse>`)
		default:
			t.Errorf("unexpected IAM action %q", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func writeXML(w http.ResponseWriter, body string) {
	_, _ = w.Write([]byte(`<?xml version="1.0"?>` + body))
}

// An IAM access key has no expiry date, so the rotation policy is the deadline.
// This asserts the whole translation: every page of every user, active keys
// only, and CreateDate + MaxKeyAge as the date the rest of the tool ranks.
func TestAWSSourceTurnsAccessKeyAgeIntoARotationDeadline(t *testing.T) {
	srv := fakeIAM(t)
	defer srv.Close()

	s := &AWSSource{MaxKeyAge: 30 * 24 * time.Hour}
	items, err := s.iam(context.Background(), awsConfigFor(srv.URL), "123456789012")
	if err != nil {
		t.Fatalf("iam: %v", err)
	}

	want := map[string]time.Time{
		"alice/AKIAALICE1": time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		"alice/AKIAALICE3": time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
		"bob/AKIABOB1":     time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d (inactive keys must be skipped, both pages walked): %+v", len(items), len(want), items)
	}
	for _, it := range items {
		expires, ok := want[it.Name]
		if !ok {
			t.Errorf("unexpected key %q", it.Name)
			continue
		}
		if !it.Expires.Equal(expires) {
			t.Errorf("%s expires = %v, want CreateDate+MaxKeyAge = %v", it.Name, it.Expires.UTC(), expires)
		}
		if it.Kind != KindIAMKey || it.Source != "aws:iam" || it.Namespace != "123456789012" {
			t.Errorf("%s = kind %s / source %s / namespace %s", it.Name, it.Kind, it.Source, it.Namespace)
		}
		if it.Labels["policy.days"] != "30" {
			t.Errorf("%s policy.days = %q, want the rotation window in days", it.Name, it.Labels["policy.days"])
		}
	}
}

// Zero MaxKeyAge is the common case — nothing sets it unless the config does —
// so the 90-day default is part of the contract, not an implementation detail.
func TestAWSSourceDefaultsToANinetyDayRotationWindow(t *testing.T) {
	srv := fakeIAM(t)
	defer srv.Close()

	items, err := (&AWSSource{}).iam(context.Background(), awsConfigFor(srv.URL), "")
	if err != nil {
		t.Fatalf("iam: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("want the active keys back")
	}
	for _, it := range items {
		if it.Labels["policy.days"] != "90" {
			t.Fatalf("%s policy.days = %q, want 90", it.Name, it.Labels["policy.days"])
		}
	}
}

// A denied ListAccessKeys must surface as an error rather than a short report:
// a silently truncated inventory reads exactly like a clean estate.
func TestAWSSourceReportsAnIAMFailureRatherThanASilentlyShortList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("Action") == "ListUsers" {
			w.Header().Set("Content-Type", "text/xml")
			writeXML(w, `<ListUsersResponse xmlns="`+iamXMLNS+`"><ListUsersResult>
				<IsTruncated>false</IsTruncated>
				<Users><member><Path>/</Path><UserName>alice</UserName><UserId>AIDA1</UserId>
				<Arn>arn:aws:iam::123456789012:user/alice</Arn>
				<CreateDate>2020-01-01T00:00:00Z</CreateDate></member></Users>
				</ListUsersResult></ListUsersResponse>`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := awsConfigFor(srv.URL)
	cfg.RetryMaxAttempts = 1
	if _, err := (&AWSSource{}).iam(context.Background(), cfg, ""); err == nil {
		t.Fatal("a denied ListAccessKeys must be reported, not swallowed")
	}
}
