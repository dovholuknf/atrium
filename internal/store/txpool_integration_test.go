//go:build integration

package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNothingHoldingTheConnectionReachesThePool reads this package's source and
// fails when code that holds the one connection can reach `s.db`.
//
// The pool is `SetMaxOpenConns(1)`. A `*Tx` or a `querier` IS that connection,
// so anything holding one that asks the pool for another waits on itself
// forever, and every handler behind the store freezes with it. That took the
// live room down on 2026-09-29: `appendEventOn(tx)` -> `promptOwes` ->
// `s.Qualify` -> `s.Tenant` -> `s.Setting` -> `s.db`. The runtime test for
// that one path is `TestPromptInsideATransactionDoesNotDeadlock`. This one
// covers every path, including the ones nobody has written yet.
//
// It is syntactic and goes by name: a function counts as holding the
// connection when it takes a `*Tx` or a `querier`, and so does an inline
// `func(tx *Tx) error` passed to `inTx`. Calls through an interface or a
// function value are not followed.
//
// ATRIUM_TXPOOL_DIR points it at another copy of this package, which is how
// it was checked against the tree that deadlocked.
func TestNothingHoldingTheConnectionReachesThePool(t *testing.T) {
	dir := "."
	if d := os.Getenv("ATRIUM_TXPOOL_DIR"); d != "" {
		dir = d
	}
	paths := txpoolAudit(t, dir)
	for _, p := range paths {
		t.Errorf("holds the connection and reaches the pool, which deadlocks under SetMaxOpenConns(1): %s", p)
	}
	if len(paths) > 0 {
		t.Log("read through the *Tx or querier already in hand instead. see promptOwes and qualifyAs for the pattern")
	}
}

type txpoolFn struct {
	name  string
	at    token.Position
	db    []token.Position
	calls map[string]token.Position
	holds bool
}

func txpoolAudit(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no source in %s: %v", dir, err)
	}
	holds := func(ft *ast.FuncType) bool {
		for _, f := range ft.Params.List {
			switch x := f.Type.(type) {
			case *ast.StarExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "Tx" {
					return true
				}
			case *ast.Ident:
				if x.Name == "querier" {
					return true
				}
			}
		}
		return false
	}
	// Every name a *Store is reachable by inside one function: its receiver
	// and any *Store parameter, since promptOwes took `s *Store` as a plain one.
	stores := func(ft *ast.FuncType, recv string) map[string]bool {
		m := map[string]bool{}
		if recv != "" {
			m[recv] = true
		}
		for _, f := range ft.Params.List {
			if st, ok := f.Type.(*ast.StarExpr); ok {
				if id, ok := st.X.(*ast.Ident); ok && id.Name == "Store" {
					for _, n := range f.Names {
						m[n.Name] = true
					}
				}
			}
		}
		return m
	}
	scan := func(f *txpoolFn, body ast.Node, s map[string]bool) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && s[id.Name] && x.Sel.Name == "db" {
					f.db = append(f.db, fset.Position(x.Pos()))
				}
			case *ast.CallExpr:
				switch c := x.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := c.X.(*ast.Ident); ok && s[id.Name] {
						f.calls["S."+c.Sel.Name] = fset.Position(x.Pos())
					}
				case *ast.Ident:
					f.calls[c.Name] = fset.Position(x.Pos())
				}
			}
			return true
		})
	}

	funcs := map[string]*txpoolFn{}
	var holders []*txpoolFn
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name, recv := fd.Name.Name, ""
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				if st, ok := fd.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := st.X.(*ast.Ident); ok && id.Name == "Store" {
						name = "S." + name
						if len(fd.Recv.List[0].Names) > 0 {
							recv = fd.Recv.List[0].Names[0].Name
						}
					}
				}
			}
			s := stores(fd.Type, recv)
			f := &txpoolFn{name: name, at: fset.Position(fd.Pos()), calls: map[string]token.Position{}, holds: holds(fd.Type)}
			scan(f, fd.Body, s)
			funcs[name] = f
			if f.holds {
				holders = append(holders, f)
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if fl, ok := n.(*ast.FuncLit); ok && holds(fl.Type) {
					l := &txpoolFn{name: name + ".func(tx)", at: fset.Position(fl.Pos()), calls: map[string]token.Position{}, holds: true}
					scan(l, fl.Body, s)
					holders = append(holders, l)
				}
				return true
			})
		}
	}

	memo := map[string]string{}
	var reach func(name string, seen map[string]bool) string
	reach = func(name string, seen map[string]bool) string {
		if v, ok := memo[name]; ok {
			return v
		}
		f := funcs[name]
		if f == nil || seen[name] {
			return ""
		}
		seen[name] = true
		if len(f.db) > 0 {
			memo[name] = name + " (s.db at " + filepath.Base(f.db[0].Filename) + ":" + strconv.Itoa(f.db[0].Line) + ")"
			return memo[name]
		}
		for _, c := range sortedKeys(f.calls) {
			if p := reach(c, seen); p != "" {
				memo[name] = name + " -> " + p
				return memo[name]
			}
		}
		memo[name] = ""
		return ""
	}

	var out []string
	for _, h := range holders {
		where := filepath.Base(h.at.Filename) + ":" + strconv.Itoa(h.at.Line) + " " + h.name
		if len(h.db) > 0 {
			out = append(out, where+" uses s.db at line "+strconv.Itoa(h.db[0].Line))
			continue
		}
		for _, c := range sortedKeys(h.calls) {
			if p := reach(c, map[string]bool{}); p != "" {
				out = append(out, where+" calls at line "+strconv.Itoa(h.calls[c].Line)+": "+p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]token.Position) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
