package nowindow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var visibleOnPurpose = map[string]string{
	"internal/api/fileopen.go:openFile":              "an editor the operator asked for",
	"internal/api/termopen.go:openTerminal":          "a terminal window the operator asked for",
	"internal/daemon/homedisk.go:OpenHome":           "the file manager, opened for the operator",
	"internal/daemon/launch.go:launchCore":           "window launch mode: a terminal the operator asked for",
	"internal/cli/control.go:spawnDetached":          "starts the daemon itself, DETACHED_PROCESS on purpose (cli.detach)",
	"internal/base/detach/detach_windows.go:Start":   "flags are deliberate: the pty host and the room must outlive their job",
	"internal/cli/new.go:repoOfCheckout":             "operator CLI, runs in the operator's own console",
	"internal/cli/roomsetup.go:Query":                "operator CLI, runs in the operator's own console",
	"internal/cli/roomsetup.go:gitCmd":               "operator CLI, runs in the operator's own console",
	"internal/base/land/duplicates.go:Duplicates":    "operator script, runs in the operator's own console",
	"internal/base/land/identity.go:CheckGitEmail":   "operator CLI, runs in the operator's own console",
	"internal/base/land/sign.go:GitSigner":           "operator CLI, runs in the operator's own console",
	"internal/link/deployready.go:spawnDeployScript": "link.detach gives it a windowless console of its own",
	"internal/base/forge/exec.go:Exec":               "the caller passes prepare, and link passes hideWindow",
}

var otherOS = []string{"_other.go", "_darwin.go", "_linux.go", "_unix.go"}

func isExecStart(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

func isHide(call *ast.CallExpr) bool {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name == "hideWindow" || f.Name == "prepareTree"
	case *ast.SelectorExpr:
		return f.Sel.Name == "Hide"
	}
	return false
}

func TestEveryExecStartIsHiddenOrListed(t *testing.T) {
	root := filepath.Join("..", "..", "..", "internal")
	var bad []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("%s: %v", path, perr)
			return nil
		}
		for _, suf := range otherOS {
			if strings.HasSuffix(path, suf) {
				return nil
			}
		}
		rel := "internal/" + filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			var starts []token.Pos
			hidden := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if isExecStart(c) {
						starts = append(starts, c.Pos())
					}
					if isHide(c) {
						hidden = true
					}
				}
				return true
			})
			if len(starts) == 0 || hidden {
				continue
			}
			if _, ok := visibleOnPurpose[rel+":"+fd.Name.Name]; ok {
				continue
			}
			for _, p := range starts {
				bad = append(bad, rel+":"+fd.Name.Name+" "+fset.Position(p).String())
			}
		}
		return nil
	})
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("exec start not hidden and not in visibleOnPurpose: %s", b)
	}
}
