//go:build ignore

// check-test-waits counts the flat waits in the tests: waitForTimeout(, setTimeout(r, and time.Sleep(. A wait is spun on
// a condition under one deadline instead, and one that has to stay carries a `// wait: <why>` comment, which is not
// counted. The per-file counts may only go down: they are compared with scripts/test-waits-baseline.json.
//
// Usage: go run scripts/check-test-waits.go [-write] [root]
// Exit 1 when a file's count rose. A count that fell is reported and does not fail. -write rewrites the baseline from
// the current counts.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var flat = regexp.MustCompile(`waitForTimeout\(|setTimeout\(r,|time\.Sleep\(`)

func counted(rel string) bool {
	switch {
	case rel == "scripts/test-board-headless.js":
		return true
	case strings.HasPrefix(rel, "scripts/board-tests/") && strings.HasSuffix(rel, ".js"):
		return !strings.Contains(rel[len("scripts/board-tests/"):], "/")
	case strings.HasPrefix(rel, "internal/") && strings.HasSuffix(rel, "_test.go"):
		return true
	}
	return false
}

func count(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "// wait:") {
			continue
		}
		n += len(flat.FindAllString(line, -1))
	}
	return n
}

func main() {
	write := flag.Bool("write", false, "rewrite the baseline from the current counts")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	now := map[string]int{}
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
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !counted(rel) {
			return nil
		}
		if n := count(p); n > 0 {
			now[rel] = n
		}
		return nil
	})
	basePath := filepath.Join(root, "scripts", "test-waits-baseline.json")
	if *write {
		b, _ := json.MarshalIndent(now, "", "  ")
		if err := os.WriteFile(basePath, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s: %d files\n", filepath.ToSlash(basePath), len(now))
		return
	}
	base := map[string]int{}
	if b, err := os.ReadFile(basePath); err == nil {
		if err := json.Unmarshal(b, &base); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", basePath, err)
			os.Exit(1)
		}
	}
	seen := map[string]bool{}
	var names []string
	for _, m := range []map[string]int{now, base} {
		for f := range m {
			if !seen[f] {
				seen[f] = true
				names = append(names, f)
			}
		}
	}
	sort.Strings(names)
	rose := 0
	for _, f := range names {
		switch {
		case now[f] > base[f]:
			rose++
			fmt.Printf("%s: %d flat waits, baseline %d. Wait on a condition under one deadline, or mark the line `// wait: <why>`.\n", f, now[f], base[f])
		case now[f] < base[f]:
			fmt.Printf("%s: %d flat waits, baseline %d. Lower the baseline: go run scripts/check-test-waits.go -write\n", f, now[f], base[f])
		}
	}
	if rose > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: %d files, no flat wait added\n", len(now))
}
