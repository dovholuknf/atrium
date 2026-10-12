//go:build ignore

// check-test-globals lists every assignment in a _test.go file to an
// identifier declared with var at package level in the same package. A test
// that does this changes state every other test in the package shares, so a
// timer or a goroutine left by one test can fire into the next. Hooks, delays,
// limits and clocks belong on the struct that uses them.
//
// Usage: go run scripts/check-test-globals.go [-summary] [root]
// Exit 1 when any is found.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	summary := flag.Bool("summary", false, "print a count per package only")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	dirs := map[string][]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" || n == "testdata" || strings.HasPrefix(n, "build")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			dirs[filepath.Dir(p)] = append(dirs[filepath.Dir(p)], p)
		}
		return nil
	})
	var dirNames []string
	for d := range dirs {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)
	total := 0
	for _, dir := range dirNames {
		fset := token.NewFileSet()
		files := map[string]*ast.File{}
		vars := map[string]bool{}
		for _, p := range dirs[dir] {
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				continue
			}
			files[p] = f
			if strings.HasSuffix(p, "_test.go") && strings.HasSuffix(f.Name.Name, "_test") {
				continue // external test package cannot see unexported vars
			}
			for _, decl := range f.Decls {
				if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.VAR {
					for _, s := range g.Specs {
						for _, n := range s.(*ast.ValueSpec).Names {
							vars[n.Name] = true
						}
					}
				}
			}
		}
		count := 0
		var lines []string
		for p, f := range files {
			if !strings.HasSuffix(p, "_test.go") {
				continue
			}
			// package-level in this file: Obj nil (declared elsewhere) or in file scope
			isGlobal := func(id *ast.Ident) bool {
				if id.Name == "_" || !vars[id.Name] {
					return false
				}
				if id.Obj == nil {
					return true
				}
				return f.Scope != nil && f.Scope.Objects[id.Name] == id.Obj
			}
			report := func(e ast.Expr, pos token.Pos) {
				for {
					switch x := e.(type) {
					case *ast.ParenExpr:
						e = x.X
						continue
					case *ast.IndexExpr:
						e = x.X
						continue
					case *ast.SelectorExpr:
						e = x.X
						continue
					}
					break
				}
				if id, ok := e.(*ast.Ident); ok && isGlobal(id) {
					count++
					lines = append(lines, fmt.Sprintf("%s: assigns package var %s", fset.Position(pos), id.Name))
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch s := n.(type) {
				case *ast.AssignStmt:
					if s.Tok != token.DEFINE {
						for _, l := range s.Lhs {
							report(l, s.Pos())
						}
					}
				case *ast.IncDecStmt:
					report(s.X, s.Pos())
				}
				return true
			})
		}
		if count == 0 {
			continue
		}
		total += count
		sort.Strings(lines)
		if *summary {
			fmt.Printf("%4d %s\n", count, dir)
		} else {
			fmt.Println(strings.Join(lines, "\n"))
		}
	}
	if total > 0 {
		fmt.Fprintf(os.Stderr, "check-test-globals: %d assignment(s) to package variables in tests\n", total)
		os.Exit(1)
	}
}
