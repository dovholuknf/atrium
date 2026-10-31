//go:build ignore

// check-imports pins the import graph of every package but the root to the layer table below. It fails when a package
// imports one its row does not list, and when a package has no row, so the table stays complete.
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
	{0, "docs/runtime/contract", nil},
	{0, "internal/base/address", nil},
	{0, "internal/base/cardcolors", nil},
	{0, "internal/base/cardroot", nil},
	{0, "internal/base/cardurl", nil},
	{0, "internal/base/codeid", nil},
	{0, "internal/base/dblock", nil},
	{0, "internal/deployready", nil},
	{0, "internal/base/docshare", nil},
	{0, "internal/base/edge", nil},
	{0, "internal/base/forge", nil},
	{0, "internal/base/inputlag", nil},
	{0, "internal/base/itemgate", nil},
	{0, "internal/base/land", nil},
	{0, "internal/base/linkfetch", nil},
	{0, "internal/base/mcprule", nil},
	{0, "internal/base/nowindow", nil},
	{0, "internal/base/prreview/render", nil},
	{0, "internal/base/remoteaccess", nil},
	{0, "internal/base/requirements", nil},
	{0, "internal/base/rules", nil},
	{0, "internal/base/resources", nil},
	{0, "internal/base/rolebin", nil},
	{0, "internal/base/safepath", nil},
	{0, "internal/base/scratch", nil},
	{0, "internal/base/shellpick", nil},
	{0, "internal/base/sshcfg", nil},
	{0, "internal/base/sshkey", nil},
	{0, "internal/base/sshtarget", nil},
	{0, "internal/base/testdiag", nil},
	{0, "internal/base/testguard", nil},
	{0, "internal/base/testwait", nil},
	{0, "internal/base/tokenprice", nil},
	{0, "internal/base/userenv", nil},
	{0, "internal/base/webasset", nil},
	{0, "internal/daemon/launch", nil},
	{0, "internal/daemon/promptline", nil},
	{0, "internal/link/wire", nil},
	{0, "internal/zitictx", nil},
	{0, "tools/devloop", nil},
	{1, "internal/base/cardproc", []string{"internal/base/nowindow"}},
	{1, "internal/base/claudeconf", []string{"internal/base/mcprule", "internal/base/rolebin"}},
	{1, "internal/base/detach", []string{"internal/base/nowindow"}},
	{1, "internal/link/ctl", []string{"internal/link/wire"}},
	{1, "internal/link/transport", []string{"internal/base/testwait", "internal/link/wire", "internal/zitictx"}},
	{1, "internal/guard", []string{"internal/base/nowindow"}},
	{1, "internal/ptyhost", []string{"internal/base/detach", "internal/base/nowindow", "internal/base/rolebin"}},
	{1, "internal/sshwizard", []string{
		"internal/base/nowindow", "internal/base/remoteaccess", "internal/base/sshcfg", "internal/base/sshkey",
	}},
	{1, "internal/store", []string{"internal/base/dblock", "internal/base/mcprule", "internal/base/rules"}},
	{1, "internal/wtfree", []string{"internal/base/nowindow"}},
	{1, "tools/codeid", []string{"internal/base/codeid"}},
	{2, "internal/cardreview", []string{"internal/base/nowindow", "internal/base/safepath", "internal/store"}},
	{2, "internal/gitsync", []string{
		"internal/base/claudeconf", "internal/base/nowindow", "internal/base/safepath", "internal/base/testguard",
		"internal/base/testwait",
	}},
	{2, "internal/daemon/screen", []string{"internal/store"}},
	{2, "internal/roomstats", []string{"internal/store", "internal/zitictx"}},
	{2, "internal/runnerprofile", []string{"internal/store"}},
	{2, "internal/small", []string{"internal/store"}},
	{2, "scripts/recognisers", []string{"internal/base/rules", "internal/store"}},
	{3, "internal/hubstore", []string{
		"internal/base/cardcolors", "internal/base/dblock", "internal/base/docshare", "internal/gitsync", "internal/base/itemgate",
		"internal/base/rules", "internal/base/sshkey", "internal/base/tokenprice",
	}},
	{3, "internal/link/conn", []string{
		"internal/base/forge", "internal/base/remoteaccess", "internal/base/testwait", "internal/gitsync", "internal/link/transport",
		"internal/link/wire",
	}},
	{3, "internal/runnersetup", []string{"internal/base/claudeconf", "internal/runnerprofile", "internal/store"}},
	{3, "internal/shepherd", []string{"internal/gitsync", "internal/base/nowindow"}},
	{3, "internal/worktrees", []string{"internal/gitsync"}},
	{4, "internal/api", []string{
		"internal/base/cardcolors", "internal/base/cardproc", "internal/cardreview", "internal/base/cardroot", "internal/base/cardurl",
		"internal/base/claudeconf", "internal/base/edge", "internal/base/forge", "internal/gitsync", "internal/base/inputlag",
		"internal/base/mcprule", "internal/base/nowindow", "internal/base/userenv", "internal/base/prreview/render", "internal/runnersetup",
		"internal/base/rules", "internal/base/safepath", "internal/base/scratch", "internal/base/shellpick", "internal/store",
		"internal/base/testguard", "internal/base/webasset", "internal/wtfree",
	}},
	{4, "internal/link/deploy", []string{
		"internal/base/address", "internal/base/edge", "internal/base/nowindow", "internal/deployready", "internal/gitsync", "internal/link/ctl",
		"internal/link/wire",
	}},
	{4, "internal/link/docs", []string{
		"internal/base/docshare", "internal/base/edge", "internal/hubstore", "internal/link/ctl", "internal/link/wire",
	}},
	{4, "internal/link", []string{
		"internal/base/address", "internal/base/cardcolors", "internal/base/cardurl", "internal/deployready", "internal/base/docshare",
		"internal/base/edge", "internal/base/forge", "internal/gitsync", "internal/hubstore", "internal/base/inputlag", "internal/base/itemgate", "internal/base/land",
		"internal/base/linkfetch", "internal/link/conn", "internal/link/ctl", "internal/link/deploy", "internal/link/docs", "internal/link/transport", "internal/link/wire", "internal/base/nowindow", "internal/base/remoteaccess", "internal/base/resources", "internal/base/rules",
		"internal/base/testguard", "internal/base/tokenprice", "internal/base/webasset", "internal/zitictx", "scripts/recognisers",
	}},
	{3, "internal/daemon/shared", []string{"internal/base/nowindow", "internal/daemon/screen", "internal/store"}},
	{4, "internal/roomspec", []string{"internal/runnersetup"}},
	{5, "internal/daemon/pulls", []string{
		"internal/api", "internal/base/forge", "internal/base/nowindow", "internal/base/prreview/render", "internal/base/safepath",
		"internal/daemon/promptline", "internal/gitsync", "internal/small", "internal/store",
	}},
	{5, "internal/daemon/publish", []string{
		"internal/api", "internal/base/docshare", "internal/base/edge", "internal/base/nowindow", "internal/base/safepath",
		"internal/store", "internal/zitictx",
	}},
	{5, "internal/daemon/roomconf", []string{
		"internal/api", "internal/base/rules", "internal/daemon/publish", "internal/small", "internal/store",
	}},
	{5, "internal/daemon/hublink", []string{
		"internal/base/address", "internal/daemon/publish", "internal/daemon/shared", "internal/roomstats", "internal/small", "internal/store",
	}},
	{5, "internal/daemon/home", []string{
		"internal/api", "internal/base/cardroot", "internal/base/forge", "internal/base/safepath", "internal/daemon/shared", "internal/gitsync", "internal/roomstats", "internal/small", "internal/store", "internal/wtfree",
	}},
	{5, "internal/daemon/cycle", []string{
		"internal/api", "internal/base/cardroot", "internal/base/nowindow", "internal/daemon/home", "internal/daemon/shared", "internal/small", "internal/store",
	}},
	{5, "internal/daemon", []string{
		"internal/base/address", "internal/api", "internal/base/cardproc", "internal/base/cardroot", "internal/base/claudeconf",
		"internal/base/docshare",
		"internal/daemon/cycle", "internal/daemon/home", "internal/daemon/hublink", "internal/daemon/launch", "internal/daemon/promptline", "internal/daemon/publish", "internal/daemon/pulls",
		"internal/daemon/roomconf", "internal/daemon/screen", "internal/daemon/shared", "internal/small",
		"internal/base/edge", "internal/base/forge", "internal/gitsync", "internal/base/inputlag", "internal/base/linkfetch", "internal/base/mcprule",
		"internal/base/nowindow", "internal/base/userenv", "internal/base/prreview/render", "internal/ptyhost", "internal/roomstats",
		"internal/base/rules", "internal/runnerprofile", "internal/runnersetup", "internal/base/safepath",
		"internal/base/scratch", "internal/base/shellpick", "internal/store", "internal/base/testguard", "internal/worktrees", "internal/wtfree",
		"internal/zitictx",
	}},
	{6, "internal/cli", []string{
		"docs/runtime/contract", "internal/base/dblock", "internal/base/testwait", "internal/worktrees",
		"internal/base/address", "internal/api", "internal/base/cardcolors", "internal/base/cardroot", "internal/base/claudeconf",
		"internal/daemon", "internal/daemon/screen", "internal/deployready", "internal/base/detach", "internal/base/docshare", "internal/base/edge", "internal/base/forge", "internal/gitsync",
		"internal/guard", "internal/hubstore", "internal/link", "internal/link/conn", "internal/link/transport", "internal/link/wire",
		"internal/base/nowindow", "internal/ptyhost", "internal/base/remoteaccess", "internal/base/requirements", "internal/base/resources", "internal/roomspec",
		"internal/roomstats", "internal/shepherd", "internal/base/sshcfg", "internal/base/sshtarget", "internal/sshwizard",
		"internal/store", "internal/base/testguard", "internal/zitictx",
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

	mods, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}/...").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go list -m:", err)
		os.Exit(2)
	}
	var out []byte
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd := exec.Command("go", "list", "-f",
			`{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}`)
		cmd.Args = append(cmd.Args, strings.Fields(string(mods))...)
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		o, err := cmd.Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "go list:", err)
			os.Exit(2)
		}
		out = append(out, o...)
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
	for i, b := range bad {
		if i == 0 || b != bad[i-1] {
			fmt.Println(b)
		}
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
