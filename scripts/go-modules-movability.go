//go:build ignore

// go-modules-movability measures, for every package under internal/daemon and internal/link, what stops it from
// leaving the root module. It prints the markdown tables that docs/rnd/go-modules-movability.md quotes.
//
// Usage: go run scripts/go-modules-movability.go
//
// It runs go list for GOOS windows and linux with tags integration,git and takes the union, then parses the files
// of each package to name the symbols behind every edge into the root module.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const mod = "github.com/dovholuknf/atrium"

type listed struct {
	ImportPath   string
	Name         string
	Dir          string
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Module       *struct{ Path string }
}

type pkg struct {
	path, name, dir, module string
	prod, test              map[string]bool
	prodFiles, testFiles    map[string]bool
	external                map[string]bool
}

var pkgs = map[string]*pkg{}

func short(p string) string { return strings.TrimPrefix(p, mod+"/") }

func list(goos string) {
	cmd := exec.Command("go", "list", "-e", "-json", "-tags", "integration,git", mod+"/...")
	cmd.Env = append(os.Environ(), "GOOS="+goos)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go list:", err)
		os.Exit(1)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var l listed
		if err := dec.Decode(&l); err != nil {
			fmt.Fprintln(os.Stderr, "decode:", err)
			os.Exit(1)
		}
		p := pkgs[l.ImportPath]
		if p == nil {
			p = &pkg{path: l.ImportPath, name: l.Name, dir: l.Dir, prod: map[string]bool{}, test: map[string]bool{},
				prodFiles: map[string]bool{}, testFiles: map[string]bool{}, external: map[string]bool{}}
			if l.Module != nil {
				p.module = l.Module.Path
			}
			pkgs[l.ImportPath] = p
		}
		for _, f := range append(append([]string{}, l.GoFiles...), l.CgoFiles...) {
			p.prodFiles[f] = true
		}
		for _, f := range append(append([]string{}, l.TestGoFiles...), l.XTestGoFiles...) {
			p.testFiles[f] = true
		}
		add := func(into map[string]bool, imps []string) {
			for _, i := range imps {
				if i == l.ImportPath {
					continue
				}
				if strings.HasPrefix(i, mod+"/") || i == mod {
					into[i] = true
				} else if first := strings.SplitN(i, "/", 2)[0]; strings.Contains(first, ".") {
					p.external[i] = true
				}
			}
		}
		add(p.prod, l.Imports)
		add(p.test, l.TestImports)
		add(p.test, l.XTestImports)
	}
}

func inRoot(p string) bool {
	q := pkgs[p]
	return q != nil && q.module == mod
}

func inBase(p string) bool {
	q := pkgs[p]
	return q != nil && q.module == mod+"/internal/base"
}

func target(p string) bool {
	s := short(p)
	return s == "internal/daemon" || s == "internal/link" ||
		strings.HasPrefix(s, "internal/daemon/") || strings.HasPrefix(s, "internal/link/")
}

func sorted(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var layers = map[string]int{}

// layer is one above the highest layer of a prod import, the rule scripts/check-imports.go uses.
func layer(p string, seen map[string]bool) int {
	if l, ok := layers[p]; ok {
		return l
	}
	if seen[p] {
		return -1
	}
	seen[p] = true
	l := 0
	if q := pkgs[p]; q != nil {
		for i := range q.prod {
			if il := layer(i, seen); il+1 > l {
				l = il + 1
			}
		}
	}
	layers[p] = l
	return l
}

// uses names the selectors each file of p makes on the package imported as imp, split prod and test.
func uses(p *pkg, imp string) (prodSyms, testSyms map[string]int) {
	prodSyms, testSyms = map[string]int{}, map[string]int{}
	impName := ""
	if q := pkgs[imp]; q != nil {
		impName = q.name
	}
	fset := token.NewFileSet()
	files := map[string]bool{}
	for f := range p.prodFiles {
		files[f] = false
	}
	for f := range p.testFiles {
		files[f] = true
	}
	for f, isTest := range files {
		af, err := parser.ParseFile(fset, filepath.Join(p.dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		local := ""
		for _, is := range af.Imports {
			ip, _ := strconv.Unquote(is.Path.Value)
			if ip != imp {
				continue
			}
			local = impName
			if is.Name != nil {
				local = is.Name.Name
			}
		}
		if local == "" || local == "_" {
			continue
		}
		into := prodSyms
		if isTest {
			into = testSyms
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if se, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := se.X.(*ast.Ident); ok && id.Name == local {
					into[se.Sel.Name]++
				}
			}
			return true
		})
	}
	return
}

func countTests(p *pkg) int {
	n := 0
	for f := range p.testFiles {
		fh, err := os.Open(filepath.Join(p.dir, f))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "func Test") {
				n++
			}
		}
		fh.Close()
	}
	return n
}

func join(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	var out []string
	for _, s := range ss {
		out = append(out, "`"+short(s)+"`")
	}
	return strings.Join(out, ", ")
}

func symList(m map[string]int, max int) string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) > max {
		return strings.Join(names[:max], " ") + fmt.Sprintf(" and %d more", len(names)-max)
	}
	return strings.Join(names, " ")
}

func main() {
	list("windows")
	list("linux")

	importers := map[string]map[string]bool{}
	testImporters := map[string]map[string]bool{}
	for _, p := range pkgs {
		for i := range p.prod {
			if importers[i] == nil {
				importers[i] = map[string]bool{}
			}
			importers[i][p.path] = true
		}
		for i := range p.test {
			if p.prod[i] {
				continue
			}
			if testImporters[i] == nil {
				testImporters[i] = map[string]bool{}
			}
			testImporters[i][p.path] = true
		}
	}

	var targets []string
	for p := range pkgs {
		if target(p) {
			targets = append(targets, p)
		}
	}
	sort.Strings(targets)

	fmt.Println("## Summary")
	fmt.Println()
	fmt.Println("| Package | Layer | Files | Tests | Root edges (prod) | Root edges (test only) | Into base |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- |")
	for _, t := range targets {
		p := pkgs[t]
		var rp, rt []string
		for _, i := range sorted(p.prod) {
			if inRoot(i) {
				rp = append(rp, i)
			}
		}
		for _, i := range sorted(p.test) {
			if inRoot(i) && !p.prod[i] {
				rt = append(rt, i)
			}
		}
		base := "yes"
		for i := range p.prod {
			if !inBase(i) {
				base = "no"
			}
		}
		for i := range p.test {
			if !inBase(i) {
				base = "no"
			}
		}
		fmt.Printf("| `%s` | %d | %d | %d | %s | %s | %s |\n", short(t), layer(t, map[string]bool{}), len(p.prodFiles),
			countTests(p), join(rp), join(rt), base)
	}

	needed := map[string]bool{}
	for _, t := range targets {
		for i := range pkgs[t].prod {
			if inRoot(i) && !target(i) {
				needed[i] = true
			}
		}
		for i := range pkgs[t].test {
			if inRoot(i) && !target(i) {
				needed[i] = true
			}
		}
	}
	fmt.Println()
	fmt.Println("## Root packages outside daemon and link that they need")
	fmt.Println()
	fmt.Println("| Package | Layer | Files | Tests | Root edges (prod) | Root edges (test only) | Third party | Imported by (prod) |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, n := range sorted(needed) {
		p := pkgs[n]
		var rp, rt []string
		for _, i := range sorted(p.prod) {
			if inRoot(i) {
				rp = append(rp, i)
			}
		}
		for _, i := range sorted(p.test) {
			if inRoot(i) && !p.prod[i] {
				rt = append(rt, i)
			}
		}
		fmt.Printf("| `%s` | %d | %d | %d | %s | %s | %d | %s |\n", short(n), layer(n, map[string]bool{}), len(p.prodFiles),
			countTests(p), join(rp), join(rt), len(p.external), join(sorted(importers[n])))
	}

	for _, t := range targets {
		p := pkgs[t]
		fmt.Println()
		fmt.Printf("### `%s`\n\n", short(t))
		fmt.Println("| Edge | Module | Side | Symbols used (prod, then test) |")
		fmt.Println("| --- | --- | --- | --- |")
		all := map[string]bool{}
		for i := range p.prod {
			all[i] = true
		}
		for i := range p.test {
			all[i] = true
		}
		for _, i := range sorted(all) {
			side := "prod"
			if !p.prod[i] {
				side = "test"
			} else if p.test[i] {
				side = "prod+test"
			}
			m := "?"
			if q := pkgs[i]; q != nil {
				m = short(q.module)
				if q.module == mod {
					m = "root"
				}
			}
			syms := ""
			if m == "root" {
				ps, ts := uses(p, i)
				syms = symList(ps, 8)
				if len(ts) > 0 {
					syms += " / test: " + symList(ts, 8)
				}
			}
			fmt.Printf("| `%s` | %s | %s | %s |\n", short(i), m, side, syms)
		}
		fmt.Println()
		fmt.Printf("Imported by (prod): %s\n\n", join(sorted(importers[t])))
		fmt.Printf("Imported by (test only): %s\n\n", join(sorted(testImporters[t])))
		var ext []string
		for e := range p.external {
			ext = append(ext, "`"+e+"`")
		}
		sort.Strings(ext)
		if len(ext) > 0 {
			fmt.Printf("Third party: %s\n", strings.Join(ext, ", "))
		}
	}
}
