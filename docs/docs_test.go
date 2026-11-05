// Package docs holds no code; this test keeps the shipped least-privilege IAM
// policy honest against the AWS calls the source actually makes. A call the
// policy misses is a service that silently skips in the field.
package docs

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// iamPrefix maps an aws-sdk-go-v2 service package to its IAM action prefix.
var iamPrefix = map[string]string{
	"acm":            "acm",
	"acmpca":         "acm-pca",
	"iam":            "iam",
	"rds":            "rds",
	"route53domains": "route53domains",
	"secretsmanager": "secretsmanager",
	"sts":            "sts",
}

type policy struct {
	Version   string
	Statement []struct {
		Effect   string
		Action   []string
		Resource string
	}
}

func loadPolicy(t *testing.T) (policy, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile("iam-readonly-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var p policy
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("iam-readonly-policy.json: %v", err)
	}
	allowed := map[string]bool{}
	for _, s := range p.Statement {
		for _, a := range s.Action {
			allowed[a] = true
		}
	}
	return p, allowed
}

func TestIAMPolicyIsReadOnly(t *testing.T) {
	p, allowed := loadPolicy(t)
	if p.Version != "2012-10-17" {
		t.Errorf("Version = %q, want 2012-10-17", p.Version)
	}
	if len(p.Statement) == 0 {
		t.Fatal("policy has no statements")
	}
	for _, s := range p.Statement {
		if s.Effect != "Allow" {
			t.Errorf("Effect = %q, want Allow", s.Effect)
		}
	}
	for a := range allowed {
		_, verb, ok := strings.Cut(a, ":")
		if !ok || strings.Contains(verb, "*") ||
			!(strings.HasPrefix(verb, "List") || strings.HasPrefix(verb, "Describe") || strings.HasPrefix(verb, "Get")) {
			t.Errorf("action %q is not a single List/Describe/Get call", a)
		}
	}
}

func TestIAMPolicyCoversEveryAWSCall(t *testing.T) {
	_, allowed := loadPolicy(t)
	files, err := filepath.Glob("../internal/source/*.go")
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]string{} // action -> file that calls it
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			pkg, ok := strings.CutPrefix(path, "github.com/aws/aws-sdk-go-v2/service/")
			if !ok || strings.Contains(pkg, "/") {
				continue
			}
			prefix, ok := iamPrefix[pkg]
			if !ok {
				t.Errorf("%s imports %s: add its IAM prefix to iamPrefix", f, path)
				continue
			}
			// Every SDK call takes a <Operation>Input, paginated or not.
			re := regexp.MustCompile(`\b` + pkg + `\.(\w+)Input\b`)
			for _, m := range re.FindAllSubmatch(src, -1) {
				used[prefix+":"+string(m[1])] = f
			}
		}
	}
	if len(used) == 0 {
		t.Fatal("found no AWS SDK calls; the scan is broken")
	}
	for action, f := range used {
		if !allowed[action] {
			t.Errorf("%s calls %s, which iam-readonly-policy.json does not grant", f, action)
		}
	}
	for action := range allowed {
		if _, ok := used[action]; !ok {
			t.Errorf("iam-readonly-policy.json grants %s, which no source calls", action)
		}
	}
}
