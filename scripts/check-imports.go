//go:build ignore

// check-imports pins the import graph of every package but the root to the layer table below, which is the graph in
// docs/design/go-modules.md section 1. It fails when a package imports one its row does not list, and when a
// package has no row, so the table stays complete.
//
// Usage: go run scripts/check-imports.go
// Exit 1 when any is found.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const modPath = "github.com/dovholuknf/atrium"

type row struct {
	layer int
	pkg   string
	// allowed is every atrium package pkg imports today, tests included. Adding to it widens the graph.
	allowed []string
}

// Layer is one above the highest layer a package imports, so a new edge to the same layer or above is a cycle risk.
var table = []row{
	{0, "cmd/ptyhost-spike", nil},
	{0, "internal/cardcolors", nil},
	{0, "internal/cardproc", nil},
	{0, "internal/cardroot", nil},
	{0, "internal/cardurl", nil},
	{0, "internal/codeid", nil},
	{0, "internal/dblock", nil},
	{0, "internal/deployready", nil},
	{0, "internal/detach", nil},
	{0, "internal/edge", nil},
	{0, "internal/forge", nil},
	{0, "internal/inputlag", nil},
	{0, "internal/itemgate", nil},
	{0, "internal/land", nil},
	{0, "internal/linkfetch", nil},
	{0, "internal/mcprule", nil},
	{0, "internal/nowindow", nil},
	{0, "internal/prreview/render", nil},
	{0, "internal/remoteaccess", nil},
	{0, "internal/requirements", nil},
	{0, "internal/rules", nil},
	{0, "internal/resources", nil},
	{0, "internal/rolebin", nil},
	{0, "internal/safepath", nil},
	{0, "internal/scratch", nil},
	{0, "internal/shellpick", nil},
	{0, "internal/sshcfg", nil},
	{0, "internal/sshkey", nil},
	{0, "internal/sshtarget", nil},
	{0, "internal/testdiag", nil},
	{0, "internal/testguard", nil},
	{0, "internal/testwait", nil},
	{0, "internal/tokenprice", nil},
	{0, "internal/webasset", nil},
	{0, "internal/zitictx", nil},
	{0, "tools/devloop", nil},
	{1, "internal/claudeconf", []string{"internal/mcprule", "internal/rolebin"}},
	{1, "internal/guard", []string{"internal/nowindow"}},
	{1, "internal/ptyhost", []string{"internal/detach", "internal/rolebin"}},
	{1, "internal/sshwizard", []string{
		"internal/nowindow", "internal/remoteaccess", "internal/sshcfg", "internal/sshkey",
	}},
	{1, "internal/store", []string{"internal/dblock", "internal/mcprule", "internal/rules"}},
	{1, "internal/wtfree", []string{"internal/nowindow"}},
	{1, "tools/codeid", []string{"internal/codeid"}},
	{2, "internal/cardreview", []string{"internal/nowindow", "internal/safepath", "internal/store"}},
	{2, "internal/gitsync", []string{
		"internal/claudeconf", "internal/nowindow", "internal/safepath", "internal/testguard",
	}},
	{2, "internal/roomstats", []string{"internal/store", "internal/zitictx"}},
	{2, "internal/runnerprofile", []string{"internal/store"}},
	{2, "scripts/recognisers", []string{"internal/rules", "internal/store"}},
	{3, "internal/hubstore", []string{
		"internal/cardcolors", "internal/dblock", "internal/gitsync", "internal/itemgate", "internal/rules",
		"internal/sshkey", "internal/tokenprice",
	}},
	{3, "internal/runnersetup", []string{"internal/claudeconf", "internal/runnerprofile", "internal/store"}},
	{3, "internal/shepherd", []string{"internal/gitsync", "internal/nowindow"}},
	{3, "internal/worktrees", []string{"internal/gitsync"}},
	{4, "internal/api", []string{
		"internal/cardcolors", "internal/cardproc", "internal/cardreview", "internal/cardroot", "internal/cardurl",
		"internal/claudeconf", "internal/edge", "internal/forge", "internal/gitsync", "internal/inputlag",
		"internal/mcprule", "internal/nowindow", "internal/prreview/render", "internal/runnersetup",
		"internal/rules", "internal/safepath", "internal/scratch", "internal/shellpick", "internal/store",
		"internal/testguard", "internal/webasset", "internal/wtfree",
	}},
	{4, "internal/link", []string{
		"internal/cardcolors", "internal/cardurl", "internal/deployready", "internal/edge", "internal/forge",
		"internal/gitsync", "internal/hubstore", "internal/inputlag", "internal/itemgate", "internal/land",
		"internal/linkfetch", "internal/nowindow", "internal/remoteaccess", "internal/resources", "internal/rules",
		"internal/testguard", "internal/tokenprice", "internal/webasset", "internal/zitictx", "scripts/recognisers",
	}},
	{4, "internal/roomspec", []string{"internal/runnersetup"}},
	{5, "internal/daemon", []string{
		"internal/api", "internal/cardroot", "internal/claudeconf", "internal/edge", "internal/forge",
		"internal/gitsync", "internal/hubstore", "internal/inputlag", "internal/linkfetch", "internal/mcprule",
		"internal/nowindow", "internal/prreview/render", "internal/ptyhost", "internal/roomstats",
		"internal/rules", "internal/runnerprofile", "internal/runnersetup", "internal/safepath",
		"internal/scratch", "internal/shellpick", "internal/store", "internal/testguard", "internal/worktrees", "internal/wtfree",
		"internal/zitictx",
	}},
	{6, "internal/cli", []string{
		"internal/api", "internal/cardcolors", "internal/cardroot", "internal/claudeconf", "internal/daemon",
		"internal/deployready", "internal/detach", "internal/edge", "internal/forge", "internal/gitsync",
		"internal/guard", "internal/hubstore", "internal/link", "internal/nowindow", "internal/ptyhost",
		"internal/remoteaccess", "internal/requirements", "internal/resources", "internal/roomspec",
		"internal/roomstats", "internal/shepherd", "internal/sshcfg", "internal/sshtarget", "internal/sshwizard",
		"internal/store", "internal/testguard", "internal/zitictx",
	}},
	{7, "cmd/atrium", []string{"internal/cli"}},
}

// forbidden names the edges the module plan rules out for good, so a violation says why and not only that.
var forbidden = []struct{ from, to, why string }{
	{"internal/link", "internal/daemon", "the hub holds no work"},
	{"internal/link", "internal/api", "the hub holds no work"},
	{"internal/daemon", "internal/link", "the room never reaches into the hub"},
	{"internal/hubstore", "internal/store", "the hub's database and the room's stay apart"},
	{"internal/hubstore", "internal/api", "the hub's database and the room's stay apart"},
	{"internal/store", "internal/hubstore", "the hub's database and the room's stay apart"},
	{"internal/ptyhost", "internal/daemon", "the pty host is a leaf the daemon calls"},
	{"internal/cli", "cmd/atrium", "cli is the top"},
}

func main() {
	byPkg := map[string]row{}
	for _, r := range table {
		byPkg[r.pkg] = r
	}
	layerOf := func(p string) int {
		if r, ok := byPkg[p]; ok {
			return r.layer
		}
		return -1
	}

	out, err := exec.Command("go", "list", "-f",
		`{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}`, "./...").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go list:", err)
		os.Exit(2)
	}

	var bad []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), "|")
		pkg := rel(parts[0])
		if pkg == "." {
			continue
		}
		seen[pkg] = true
		r, ok := byPkg[pkg]
		if !ok {
			bad = append(bad, pkg+" is not in the table in scripts/check-imports.go: add it with its layer and allowed imports")
			continue
		}
		allowed := map[string]bool{}
		for _, a := range r.allowed {
			allowed[a] = true
		}
		imported := map[string]bool{}
		for _, f := range strings.Fields(strings.Join(parts[1:], " ")) {
			if strings.HasPrefix(f, modPath+"/") {
				imported[rel(f)] = true
			}
		}
		for imp := range imported {
			if imp == pkg || allowed[imp] {
				continue
			}
			bad = append(bad, fmt.Sprintf("%s -> %s (not allowed: %s)", pkg, imp, reason(r, imp, layerOf(imp))))
		}
	}
	for _, r := range table {
		if !seen[r.pkg] {
			bad = append(bad, r.pkg+" is in the table in scripts/check-imports.go but no longer exists: remove its row")
		}
	}

	sort.Strings(bad)
	for _, b := range bad {
		fmt.Println(b)
	}
	if len(bad) > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: %d packages match the layer table\n", len(seen))
}

func reason(r row, imp string, impLayer int) string {
	for _, f := range forbidden {
		if f.from == r.pkg && f.to == imp {
			return f.why
		}
	}
	switch {
	case impLayer < 0:
		return imp + " has no row in the table, add it"
	case impLayer >= r.layer:
		return fmt.Sprintf("layer %d may not import layer %d", r.layer, impLayer)
	}
	return fmt.Sprintf("layer %d does not list it, widen the table only with a plan step", r.layer)
}

func rel(p string) string {
	if p == modPath {
		return "."
	}
	return strings.TrimPrefix(p, modPath+"/")
}
