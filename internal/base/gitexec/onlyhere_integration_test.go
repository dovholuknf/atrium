//go:build integration

package gitexec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOnlyGitexecStartsGit(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no module root at %s: %v", root, err)
	}
	own, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "build.claude":
				return filepath.SkipDir
			}
			if p == own {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		found = append(found, gitExecs(t, root, p)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("git started outside internal/base/gitexec, use gitexec.Run or gitexec.Command:\n  %s",
			strings.Join(found, "\n  "))
	}
}

func TestGitExecsFindsADirectGit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.go")
	src := "package x\nimport \"os/exec\"\nfunc a() { exec.Command(\"git\", \"status\"); exec.CommandContext(nil, \"git.exe\") }\n" +
		"func b() { exec.Command(\"go\", \"git\") }\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitExecs(t, filepath.Dir(p), p); len(got) != 2 {
		t.Fatalf("want the two git calls, got %q", got)
	}
}

// gitExecs is each exec.Command or exec.CommandContext in the file p whose program is the literal git.
func gitExecs(t *testing.T, root, p string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "exec" {
			return true
		}
		prog := 0
		switch sel.Sel.Name {
		case "Command":
		case "CommandContext":
			prog = 1
		default:
			return true
		}
		if len(call.Args) <= prog {
			return true
		}
		lit, ok := call.Args[prog].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil && (v == "git" || v == "git.exe") {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel)+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
		}
		return true
	})
	return out
}
