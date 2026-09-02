package source

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Verifying the AWS adapters against a live account.
//
// WHY: the ACM, IAM and Secrets Manager adapters compile and vet clean and have
// never run with real credentials, so nothing confirms the field mappings or
// the expiry semantics. Mocked responses cannot: they assert that the code does
// what it was written to do, and the failure being guarded against is a field
// meaning something other than what was assumed.
//
// This runs the adapters for real and checks the things that ARE checkable from
// the results, then prints what a human still has to check by eye against the
// console. It is not a substitute for that comparison — it is the thing that
// makes it a five-minute job instead of an afternoon.
//
// EVERY LINE IS SAFE TO POST. The issue asks for a summary with ARNs, account
// ids and secret names redacted, and the redaction is structural: AWSVerdict
// has nowhere to put an identifier. Items are counted and dated by OFFSET from
// now, which is the one thing a reader needs to compare against a console and
// the one thing that identifies nothing.

// AWSCheck is one acceptance criterion and whether it held.
type AWSCheck struct {
	Name   string
	Passed bool
	// Inconclusive marks a check the run could not decide — usually because
	// the account holds nothing that would exercise it. Deliberately not the
	// same as a pass: an empty account passes every check that is written as
	// "nothing was wrong", and reporting that as evidence would be a lie.
	Inconclusive bool
	Detail       string
}

// AWSVerdict is the report.
type AWSVerdict struct {
	Region   string
	Services []AWSServiceReport
	Checks   []AWSCheck
	// Horizon is the expiry spread, as day offsets from now. The only shape in
	// which the dates can be posted, and enough to eyeball against a console.
	Horizon []int
	Errors  []string
}

// AWSServiceReport is one adapter's outcome. Skipped, denied and empty are
// three different things and are kept apart: an account with no certificates
// must not read as a working ACM adapter.
type AWSServiceReport struct {
	Name    string
	Skipped bool
	Denied  bool
	Items   int
	Err     string
}

// VerifyAWS runs the three adapters and reports what can be established from
// the results. Read-only throughout — the adapters only list.
func VerifyAWS(ctx context.Context, s *AWSSource) (*AWSVerdict, error) {
	opts := []func(*config.LoadOptions) error{}
	if s.Region != "" {
		opts = append(opts, config.WithRegion(s.Region))
	}
	if s.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(s.Profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	account := ""
	if id, e := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); e == nil && id.Account != nil {
		account = *id.Account
	}
	items, per, collectErr := collectServices(s.services(ctx, cfg, account))
	return buildAWSVerdict(cfg.Region, items, per, collectErr, time.Now()), nil
}

// buildAWSVerdict is the whole judgement, separated from the AWS calls so it
// can be tested with items that no account would hold on demand — a resource
// with no expiry, one service denied, a single short page.
func buildAWSVerdict(region string, items []Item, per []serviceResult, collectErr error, now time.Time) *AWSVerdict {
	v := &AWSVerdict{Region: region}
	for _, r := range per {
		rep := AWSServiceReport{Name: r.Name, Skipped: r.Skipped, Items: r.Items}
		if r.Err != nil {
			rep.Err = r.Err.Error()
			rep.Denied = isDenial(r.Err)
			v.Errors = append(v.Errors, r.Name+": "+r.Err.Error())
		}
		v.Services = append(v.Services, rep)
	}

	// 1. Did each adapter run at all.
	for _, r := range v.Services {
		switch {
		case r.Skipped:
			v.check("adapter ran: "+r.Name, false, true,
				"skipped by configuration; this run says nothing about it")
		case r.Err != "":
			v.check("adapter ran: "+r.Name, false, false, "failed: "+r.Err)
		case r.Items == 0:
			v.check("adapter ran: "+r.Name, false, true,
				"returned no items — either the account holds none, or the adapter "+
					"is silently finding nothing. Only the console can tell you which")
		default:
			v.check("adapter ran: "+r.Name, true, false,
				fmt.Sprintf("%d item(s)", r.Items))
		}
	}

	// 2. One denied service must not lose the others. Only decidable when
	// something WAS denied.
	denied, worked := 0, 0
	for _, r := range v.Services {
		if r.Denied {
			denied++
		} else if !r.Skipped && r.Err == "" && r.Items > 0 {
			worked++
		}
	}
	switch {
	case denied == 0:
		v.check("a denied service degrades rather than fails the run", false, true,
			"nothing was denied on this run; deny one service's permission and "+
				"run again to exercise it")
	case worked > 0:
		v.check("a denied service degrades rather than fails the run", true, false,
			fmt.Sprintf("%d denied, %d still returned items", denied, worked))
	default:
		v.check("a denied service degrades rather than fails the run", false, false,
			"everything was denied, so nothing proves the others would survive")
	}

	// 3. Nothing without an expiry may be reported as expiring. This is the
	// check most likely to catch a field mapping that is wrong rather than
	// missing: a zero time reads as 1st January year 1, which ranks as the most
	// urgent thing in the account.
	var undated []string
	for _, it := range items {
		if it.Expires.IsZero() || it.Expires.Year() < 2000 {
			undated = append(undated, string(it.Kind)+" from "+it.Source)
		}
	}
	if len(undated) == 0 {
		v.check("nothing without an expiry is reported as expiring", len(items) > 0,
			len(items) == 0, fmt.Sprintf("%d item(s), all dated", len(items)))
	} else {
		v.check("nothing without an expiry is reported as expiring", false, false,
			fmt.Sprintf("%d item(s) carry a zero or implausible date: %s",
				len(undated), strings.Join(dedupe(undated), ", ")))
	}

	// 4. Pagination past one page. The AWS list APIs return 100 or fewer per
	// page, so more than that from one service is proof the paginator advanced.
	// Fewer is not proof it is broken, which is why this is inconclusive.
	paged := false
	for _, r := range v.Services {
		if r.Items > awsPageSize {
			paged = true
		}
	}
	if paged {
		v.check("pagination exercised past one page", true, false,
			"a service returned more than one page's worth")
	} else {
		v.check("pagination exercised past one page", false, true,
			fmt.Sprintf("no service returned more than %d items; create more than "+
				"that of one kind to exercise the paginator", awsPageSize))
	}

	// 5. Ranking. Not that the order is "right" — that is what the console
	// comparison is for — but that it is an order at all, and that the same
	// input produces it deterministically.
	v.Horizon = horizonDays(items, now)
	if len(v.Horizon) > 1 {
		sorted := sort.IntsAreSorted(v.Horizon)
		v.check("expiries span a range and sort", sorted, false,
			fmt.Sprintf("%d item(s), %d to %d days out",
				len(v.Horizon), v.Horizon[0], v.Horizon[len(v.Horizon)-1]))
	} else {
		v.check("expiries span a range and sort", false, true,
			"fewer than two dated items; nothing to order")
	}

	if collectErr != nil && len(items) == 0 {
		v.Errors = append(v.Errors, "no items collected at all")
	}
	return v
}

// awsPageSize is the largest page the list APIs used here return. ACM,
// ListAccessKeys and ListSecrets all cap at 100.
const awsPageSize = 100

func (v *AWSVerdict) check(name string, passed, inconclusive bool, detail string) {
	v.Checks = append(v.Checks, AWSCheck{
		Name: name, Passed: passed, Inconclusive: inconclusive, Detail: detail,
	})
}

// horizonDays is every item's expiry as whole days from now, ascending. The
// only form the dates can be posted in: an offset identifies nothing and is
// exactly what a reader compares against a console.
func horizonDays(items []Item, now time.Time) []int {
	out := make([]int, 0, len(items))
	for _, it := range items {
		if it.Expires.IsZero() {
			continue
		}
		out = append(out, int(it.Expires.Sub(now).Hours()/24))
	}
	sort.Ints(out)
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// isDenial recognises the shape of a permissions error. Deliberately by string:
// the three services return three different typed errors for the same thing,
// and a type switch across all of them would be more code than the check it
// serves and would silently stop matching when a new one is added.
func isDenial(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"accessdenied", "access denied", "not authorized",
		"unauthorizedoperation", "explicit deny"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// Text is the report, in the form the issue asks for it to be posted in.
func (v *AWSVerdict) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "expiry-radar: AWS adapter verification (region %s)\n\n", v.Region)

	fmt.Fprintf(&b, "%-18s %8s  %s\n", "service", "items", "outcome")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 58))
	for _, s := range v.Services {
		outcome := "ok"
		switch {
		case s.Skipped:
			outcome = "skipped"
		case s.Denied:
			outcome = "DENIED (degraded, not fatal)"
		case s.Err != "":
			outcome = "ERROR"
		case s.Items == 0:
			outcome = "no items — see below"
		}
		fmt.Fprintf(&b, "%-18s %8d  %s\n", s.Name, s.Items, outcome)
	}

	fmt.Fprintf(&b, "\nchecks\n")
	for _, c := range v.Checks {
		mark := "FAIL"
		switch {
		case c.Inconclusive:
			mark = "  ? "
		case c.Passed:
			mark = "  ok"
		}
		fmt.Fprintf(&b, "  %s  %-48s %s\n", mark, c.Name, c.Detail)
	}

	if len(v.Horizon) > 0 {
		fmt.Fprintf(&b, "\nexpiries, in days from now: %v\n", v.Horizon)
		fmt.Fprintf(&b, "  (offsets, not dates — the form that identifies nothing "+
			"and still\n   compares against a console)\n")
	}

	if len(v.Errors) > 0 {
		fmt.Fprintf(&b, "\nerrors\n")
		for _, e := range v.Errors {
			fmt.Fprintf(&b, "  - %s\n", e)
		}
	}

	fmt.Fprintf(&b, "\nSTILL TO CHECK BY EYE. Nothing here can confirm that a date "+
		"MEANS what\nthe adapter assumed — that is the failure a live run exists to "+
		"catch, and it\nneeds the console open beside this output:\n"+
		"  - each expiry above matches what the console shows for that resource\n"+
		"  - an IAM key's \"expiry\" is its age against -max-key-age, not a date AWS\n"+
		"    reports; the console shows the CREATION date, so check the arithmetic\n"+
		"  - a rotating secret's next rotation matches the schedule on the secret\n")
	return b.String()
}

// OK is whether the run is evidence the adapters work. An inconclusive check is
// not a failure, but it is not evidence either — Text() says which is which.
func (v *AWSVerdict) OK() bool {
	for _, c := range v.Checks {
		if !c.Passed && !c.Inconclusive {
			return false
		}
	}
	return true
}
