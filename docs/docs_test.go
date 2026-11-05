// Package docs holds no code; this test keeps the shipped least-privilege IAM
// policy honest against the AWS calls the source actually makes. A call the
// policy misses is a service that silently skips in the field.
package docs

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
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

// TestVaultPolicyCoversEveryVaultCall checks vault-readonly-policy.hcl against
// every s.do call in vault.go: each request must be granted, each grant must be
// used, and nothing may write.
func TestVaultPolicyCoversEveryVaultCall(t *testing.T) {
	raw, err := os.ReadFile("vault-readonly-policy.hcl")
	if err != nil {
		t.Fatal(err)
	}
	granted := map[string]bool{} // "capability path"
	block := regexp.MustCompile(`path "([^"]+)" \{\s*capabilities = \[([^\]]*)\]`)
	for _, m := range block.FindAllStringSubmatch(string(raw), -1) {
		for _, c := range strings.Split(m[2], ",") {
			c, _ = strconv.Unquote(strings.TrimSpace(c))
			if c != "read" && c != "list" {
				t.Errorf("path %q grants %q, want read or list only", m[1], c)
			}
			granted[c+" "+m[1]] = true
		}
	}
	if len(granted) == 0 {
		t.Fatal("found no path blocks; the parse is broken")
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../internal/source/vault.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	capability := map[string]string{"http.MethodGet": "read", `"LIST"`: "list"}
	// The policy is written for the default `pki` mount; any other variable in
	// a path, such as a serial, is what the policy's `*` stands for.
	var flatten func(ast.Expr) string
	flatten = func(e ast.Expr) string {
		switch e := e.(type) {
		case *ast.BasicLit:
			s, _ := strconv.Unquote(e.Value)
			return s
		case *ast.BinaryExpr:
			return flatten(e.X) + flatten(e.Y)
		case *ast.CallExpr:
			if len(e.Args) == 1 {
				if id, ok := e.Args[0].(*ast.Ident); ok && id.Name == "mount" {
					return "pki"
				}
			}
		}
		return "*"
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 5 {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "do" {
			return true
		}
		var method strings.Builder
		if err := printer.Fprint(&method, fset, call.Args[2]); err != nil {
			t.Fatal(err)
		}
		c, ok := capability[method.String()]
		if !ok {
			t.Errorf("vault.go sends %s, which is not a read-only method", method.String())
			return true
		}
		req := c + " " + strings.TrimPrefix(flatten(call.Args[3]), "/v1/")
		used[req] = true
		if !granted[req] {
			t.Errorf("vault.go needs %q, which vault-readonly-policy.hcl does not grant", req)
		}
		return true
	})
	if len(used) == 0 {
		t.Fatal("found no Vault calls; the scan is broken")
	}
	for g := range granted {
		if !used[g] {
			t.Errorf("vault-readonly-policy.hcl grants %q, which no source calls", g)
		}
	}
}
