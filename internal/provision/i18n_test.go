package provision

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every preflight problem, and every sentence a step says, has words in all
// five languages.
//
// A step's name has been translated since the panel shipped, from
// servers.steps.*. The line underneath it — "Connected to 203.0.113.10",
// "Ubuntu 24.04, 4 cores, 8192 MB memory, 40 GB free" — was the English the Go
// code wrote, so somebody adding a server in Indonesian read a translated
// heading over an English sentence. The preflight problems were the same.
//
// The codes are read out of the source rather than listed here, so a new
// problem or a new step message cannot be added without a translation.
func TestEveryPreflightProblemAndStepMessageHasItsWords(t *testing.T) {
	preflight := codesIn(t, "preflight.go", "Code", "code")
	steps := stepMessageKeys(t)

	if len(preflight) < 15 {
		t.Fatalf("only found %d preflight codes, so this test has stopped reading the source", len(preflight))
	}
	if len(steps) < 10 {
		t.Fatalf("only found %d step messages, so this test has stopped reading the source", len(steps))
	}

	locales, err := filepath.Glob(filepath.Join("..", "..", "web", "src", "locales", "*.json"))
	if err != nil || len(locales) < 5 {
		t.Fatalf("find the locales: %v", err)
	}

	for _, path := range locales {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var locale struct {
			Errors struct {
				Catalogue map[string]map[string]string `json:"catalogue"`
			} `json:"errors"`
			Servers struct {
				StepMessage map[string]string `json:"stepMessage"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(body, &locale); err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		language := strings.TrimSuffix(filepath.Base(path), ".json")

		// A fatal preflight problem becomes an errdoc Problem whose code is
		// "preflight." plus this one, so it is looked up in the same catalogue
		// as every other error.
		for _, code := range preflight {
			key := "preflight_" + code
			entry, ok := locale.Errors.Catalogue[key]
			if !ok {
				t.Errorf("%s: the preflight problem %q has no words: add errors.catalogue.%s",
					language, code, key)
				continue
			}
			for _, field := range []string{"title", "cause", "impact", "fix"} {
				if strings.TrimSpace(entry[field]) == "" {
					t.Errorf("%s: errors.catalogue.%s.%s is missing", language, key, field)
				}
			}
		}

		for _, key := range steps {
			if strings.TrimSpace(locale.Servers.StepMessage[key]) == "" {
				t.Errorf("%s: the step message %q has no words: add servers.stepMessage.%s",
					language, key, key)
			}
		}
		for key := range locale.Servers.StepMessage {
			if !slices.Contains(steps, key) {
				t.Errorf("%s: servers.stepMessage.%s is not a sentence any step says", language, key)
			}
		}
	}
}

// codesIn reads every string literal assigned to the named struct field or to
// the named variable, in one file of this package.
func codesIn(t *testing.T, file, field, variable string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	seen := map[string]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && key.Name == field {
				if value, ok := literal(n.Value); ok {
					seen[value] = true
				}
			}
		case *ast.AssignStmt:
			for i, target := range n.Lhs {
				name, ok := target.(*ast.Ident)
				if !ok || name.Name != variable || i >= len(n.Rhs) {
					continue
				}
				if value, ok := literal(n.Rhs[i]); ok {
					seen[value] = true
				}
			}
		}
		return true
	})
	return sorted(seen)
}

// stepMessageKeys reads every store.StepNote's Key in this package.
func stepMessageKeys(t *testing.T) []string {
	t.Helper()
	// Two packages write step notes: adding and removing a server, and
	// restoring a backup. Both fill the same list on the same screen.
	var files []string
	for _, dir := range []string{".", filepath.Join("..", "backup")} {
		found, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("list %s: %v", dir, err)
		}
		files = append(files, found...)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			composite, ok := node.(*ast.CompositeLit)
			if !ok || !isStepNote(composite.Type) {
				return true
			}
			for _, element := range composite.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.Ident)
				if !ok || key.Name != "Key" {
					continue
				}
				value, ok := literal(pair.Value)
				// A failed step borrows the error catalogue's own entry, which
				// TestEveryErrorHasItsWordsInTheInterface already covers.
				if ok && value != "" && !strings.HasPrefix(value, "problem:") {
					seen[value] = true
				}
			}
			return true
		})
	}
	return sorted(seen)
}

func isStepNote(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "store" && sel.Sel.Name == "StepNote"
}

func literal(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, ok1 := literal(v.X)
		right, ok2 := literal(v.Y)
		return left + right, ok1 && ok2
	}
	return "", false
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
