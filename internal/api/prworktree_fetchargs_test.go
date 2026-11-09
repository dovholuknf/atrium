package api

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

func TestFetchArgsNeverCollectGarbage(t *testing.T) {
	t.Parallel()
	forgeSpec := forge.FetchSpec{Remote: "https://github.com/o/r.git", Refspec: "pull/7/head"}
	hubSpec := forge.FetchSpec{Remote: "http://127.0.0.1:9/tok/git/hub/github/o/r.git", Refspec: "refs/atrium/pr/7",
		Hub: "github/o/r"}
	for name, args := range map[string]string{
		"forge": fetchLine(forgeSpec, "refs/atrium/pr/7", "!gh auth git-credential"),
		"hub":   fetchLine(hubSpec, "refs/atrium/pr/7", ""),
	} {
		for _, want := range []string{"-c gc.auto=0", "-c maintenance.auto=false"} {
			if !strings.Contains(args, want) || strings.Index(args, want) > strings.Index(args, " fetch ") {
				t.Errorf("%s: %q missing before fetch in %s", name, want, args)
			}
		}
	}
}

func fetchLine(spec forge.FetchSpec, dst, helper string) string {
	config, op := fetchArgs(spec, dst, helper)
	args, _ := gitexec.Argv(gitexec.Opts{Config: config}, op)
	return strings.Join(args, " ")
}
