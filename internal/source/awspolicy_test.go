package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/iam-readonly-policy.json is what users attach, so it must grant exactly
// the AWS calls the source makes: a missing action silently loses a service's
// findings, an extra one breaks the least-privilege promise.
func TestIAMPolicyDocMatchesTheCallsMade(t *testing.T) {
	raw, err := os.ReadFile("../../docs/iam-readonly-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Statement []struct {
			Effect string
			Action []string
		}
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("docs/iam-readonly-policy.json: %v", err)
	}
	granted := map[string]bool{}
	for _, s := range policy.Statement {
		if s.Effect != "Allow" {
			t.Errorf("unexpected effect %q", s.Effect)
		}
		for _, a := range s.Action {
			granted[a] = true
		}
	}

	// Every SDK call takes a &pkg.OpInput{}, so the inputs name the actions.
	prefix := map[string]string{"acmpca": "acm-pca"}
	input := regexp.MustCompile(`&(\w+)\.(\w+)Input\{`)
	files, _ := filepath.Glob("aws*.go")
	called := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range input.FindAllStringSubmatch(string(src), -1) {
			svc := m[1]
			if p, ok := prefix[svc]; ok {
				svc = p
			}
			called[svc+":"+m[2]] = true
		}
	}
	if len(called) == 0 {
		t.Fatal("found no AWS calls; the pattern no longer matches the source")
	}

	var missing, extra []string
	for a := range called {
		if !granted[a] {
			missing = append(missing, a)
		}
	}
	for a := range granted {
		if !called[a] {
			extra = append(extra, a)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("called but not granted in the policy: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("granted in the policy but never called: %v", extra)
	}
}
