package source

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// These test the VERDICT, not AWS. The cases that matter are exactly the ones
// no account produces on demand: a resource with no expiry, one service denied
// while the others work, an empty account. Getting any of them wrong turns the
// live run into a report that reads like a pass and proves nothing.

func item(kind Kind, src string, in time.Duration, now time.Time) Item {
	return Item{Kind: kind, Name: "x", Source: src, Expires: now.Add(in)}
}

func svcOK(name string, n int) serviceResult { return serviceResult{Name: name, Items: n} }
func svcSkipped(name string) serviceResult   { return serviceResult{Name: name, Skipped: true} }
func svcErr(name string, e error) serviceResult {
	return serviceResult{Name: name, Err: e}
}

var errAccessDenied = errors.New("operation error ACM: ListCertificates, AccessDeniedException: " +
	"User is not authorized to perform: acm:ListCertificates")

// ---- degradation: the criterion that was untestable before ------------------

func TestOneDeniedServiceKeepsTheOthersFindings(t *testing.T) {
	// The rule the issue names, and the reason collectServices exists as its
	// own function: with the AWS calls inline there was no way to reach it
	// without an account.
	got, per, err := collectServices([]awsService{
		{"acm", false, func() ([]Item, error) { return nil, errAccessDenied }},
		{"iam", false, func() ([]Item, error) { return []Item{{Name: "k"}}, nil }},
		{"secretsmanager", false, func() ([]Item, error) { return []Item{{Name: "s"}}, nil }},
	})
	if err == nil {
		t.Fatal("the denial was not reported at all")
	}
	if len(got) != 2 {
		t.Fatalf("a denied ACM lost %d of the other services' items", 2-len(got))
	}
	if per[0].Err == nil || per[1].Err != nil {
		t.Fatalf("the failure was attributed to the wrong service: %+v", per)
	}
}

func TestPartialItemsFromAFailingServiceAreStillKept(t *testing.T) {
	// A paginator that dies on page three has already returned two pages. They
	// are real findings and throwing them away would make a partial outage look
	// like an empty account.
	got, _, err := collectServices([]awsService{
		{"acm", false, func() ([]Item, error) {
			return []Item{{Name: "page-1"}}, errors.New("throttled on page 2")
		}},
	})
	if err == nil {
		t.Fatal("the error was swallowed")
	}
	if len(got) != 1 {
		t.Fatalf("the page that did arrive was discarded")
	}
}

func TestASkippedServiceIsNotAFailure(t *testing.T) {
	_, per, err := collectServices([]awsService{
		{"acm", true, func() ([]Item, error) { t.Fatal("a skipped service was called"); return nil, nil }},
	})
	if err != nil {
		t.Fatalf("skipping produced an error: %v", err)
	}
	if !per[0].Skipped {
		t.Fatal("the skip was not recorded")
	}
}

// ---- the verdict ------------------------------------------------------------

func TestAnEmptyAccountIsInconclusiveRatherThanAPass(t *testing.T) {
	// The failure mode this exists to prevent: an account with nothing in it
	// passes every check written as "nothing was wrong", and reporting that as
	// evidence the adapters work would be a lie.
	now := time.Now()
	v := buildAWSVerdict("eu-west-1", nil, []serviceResult{
		svcOK("acm", 0), svcOK("iam", 0), svcOK("secretsmanager", 0),
	}, nil, now)

	var conclusive int
	for _, c := range v.Checks {
		if c.Passed {
			conclusive++
		}
	}
	if conclusive != 0 {
		t.Fatalf("%d check(s) passed on an empty account:\n%s", conclusive, v.Text())
	}
	if !strings.Contains(v.Text(), "no items") {
		t.Error("the report does not say the adapters returned nothing")
	}
}

func TestAResourceWithNoExpiryIsAFailureNotANote(t *testing.T) {
	// A zero time reads as 1 January year 1, which ranks as the most urgent
	// thing in the account — so a missing field does not merely lose an item,
	// it puts a phantom at the top of the list.
	now := time.Now()
	items := []Item{
		item(KindTLSCert, "aws:acm", 30*24*time.Hour, now),
		{Kind: KindSecret, Name: "no-rotation", Source: "aws:secretsmanager"}, // zero Expires
	}
	v := buildAWSVerdict("eu-west-1", items, []serviceResult{svcOK("acm", 2)}, nil, now)
	if v.OK() {
		t.Fatalf("an undated item passed verification:\n%s", v.Text())
	}
	if !strings.Contains(v.Text(), "zero or implausible date") {
		t.Errorf("the report does not name the problem:\n%s", v.Text())
	}
}

func TestADenialIsReportedAsDegradedRatherThanBroken(t *testing.T) {
	now := time.Now()
	items := []Item{item(KindTLSCert, "aws:acm", 10*24*time.Hour, now),
		item(KindSecret, "aws:secretsmanager", 40*24*time.Hour, now)}
	v := buildAWSVerdict("eu-west-1", items, []serviceResult{
		svcErr("iam", errAccessDenied), svcOK("acm", 1), svcOK("secretsmanager", 1),
	}, errAccessDenied, now)

	if !v.Services[0].Denied {
		t.Fatal("an AccessDeniedException was not recognised as a denial")
	}
	if !strings.Contains(v.Text(), "DENIED (degraded, not fatal)") {
		t.Errorf("the report reads like a hard failure:\n%s", v.Text())
	}
	// And the degradation criterion is now decidable, and decided.
	for _, c := range v.Checks {
		if strings.HasPrefix(c.Name, "a denied service degrades") {
			if !c.Passed {
				t.Errorf("two services worked through a denial and it did not count: %s", c.Detail)
			}
			return
		}
	}
	t.Fatal("the degradation check is missing from the report")
}

func TestEverythingDeniedProvesNothing(t *testing.T) {
	now := time.Now()
	v := buildAWSVerdict("eu-west-1", nil, []serviceResult{
		svcErr("acm", errAccessDenied), svcErr("iam", errAccessDenied),
		svcErr("secretsmanager", errAccessDenied),
	}, errAccessDenied, now)
	if v.OK() {
		t.Fatal("a run where every call was refused passed verification")
	}
}

func TestPaginationIsInconclusiveUntilSomethingActuallyPages(t *testing.T) {
	now := time.Now()
	small := buildAWSVerdict("eu-west-1", []Item{item(KindTLSCert, "aws:acm", time.Hour, now)},
		[]serviceResult{svcOK("acm", 3)}, nil, now)
	big := buildAWSVerdict("eu-west-1", []Item{item(KindTLSCert, "aws:acm", time.Hour, now)},
		[]serviceResult{svcOK("acm", awsPageSize+1)}, nil, now)

	if find(t, small, "pagination").Inconclusive != true {
		t.Error("three items were reported as proof the paginator advanced")
	}
	if !find(t, big, "pagination").Passed {
		t.Error("more than a full page did not count as pagination")
	}
}

func TestASkippedAdapterIsNeitherPassNorFail(t *testing.T) {
	now := time.Now()
	v := buildAWSVerdict("eu-west-1", nil, []serviceResult{svcSkipped("iam")}, nil, now)
	c := find(t, v, "adapter ran: iam")
	if c.Passed || !c.Inconclusive {
		t.Fatalf("a skipped adapter was scored: passed=%v inconclusive=%v", c.Passed, c.Inconclusive)
	}
}

// ---- the report is postable -------------------------------------------------

func TestNoIdentifierReachesTheReport(t *testing.T) {
	// The issue asks for ARNs, account ids and secret names redacted. The
	// guarantee is structural — AWSVerdict has nowhere to put one — and this
	// is what keeps it that way when somebody adds a field.
	now := time.Now()
	items := []Item{{
		Kind:      KindTLSCert,
		Name:      "payments.internal.example.com",
		Source:    "aws:acm",
		Namespace: "123456789012",
		Expires:   now.Add(9 * 24 * time.Hour),
		Labels: map[string]string{
			"arn": "arn:aws:acm:eu-west-1:123456789012:certificate/abc-123",
		},
	}}
	text := buildAWSVerdict("eu-west-1", items, []serviceResult{svcOK("acm", 1)}, nil, now).Text()

	for _, secret := range []string{
		"payments.internal.example.com", "123456789012", "arn:aws:", "abc-123",
	} {
		if strings.Contains(text, secret) {
			t.Errorf("%q reached the pasteable report:\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "9]") && !strings.Contains(text, "[9") {
		t.Errorf("the day offset is the useful half and did not survive:\n%s", text)
	}
}

func TestTheReportSaysWhatStillNeedsAHumansEyes(t *testing.T) {
	// Nothing here can confirm that a date MEANS what the adapter assumed, and
	// a report that did not say so would be read as a full verification.
	now := time.Now()
	text := buildAWSVerdict("eu-west-1", nil, nil, nil, now).Text()
	for _, phrase := range []string{"STILL TO CHECK BY EYE", "console", "max-key-age"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("the report omits %q", phrase)
		}
	}
}

// ---- helpers ----------------------------------------------------------------

func find(t *testing.T, v *AWSVerdict, prefix string) AWSCheck {
	t.Helper()
	for _, c := range v.Checks {
		if strings.HasPrefix(c.Name, prefix) {
			return c
		}
	}
	t.Fatalf("no check named %q in:\n%s", prefix, v.Text())
	return AWSCheck{}
}

func TestDenialDetectionCoversTheThreeServicesWordings(t *testing.T) {
	// Three services, three typed errors for the same thing. Matched by string
	// on purpose: a type switch across all of them would be more code than the
	// check it serves, and would silently stop matching when a fourth is added.
	for _, msg := range []string{
		"AccessDeniedException: not authorized to perform acm:ListCertificates",
		"AccessDenied: User: arn:...:user/x is not authorized",
		"api error UnauthorizedOperation",
		"with an explicit deny in a service control policy",
	} {
		if !isDenial(errors.New(msg)) {
			t.Errorf("not recognised as a denial: %s", msg)
		}
	}
	for _, msg := range []string{"connection refused", "context deadline exceeded", "throttled"} {
		if isDenial(errors.New(msg)) {
			t.Errorf("wrongly read as a denial: %s", msg)
		}
	}
	if isDenial(nil) {
		t.Error("nil is not a denial")
	}
}
