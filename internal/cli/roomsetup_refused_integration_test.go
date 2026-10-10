//go:build integration

package cli

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// A spec the binary refuses exits 1 AND says why on stderr: the provision scripts print that reason, and a silent exit 1 would
// leave the operator with nothing to act on.
func TestRoomSetupSaysWhyASpecIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"a filesystem root", []string{"--spec", "-", "--plan"}, "version: 1\nname: r\nos: " + runtime.GOOS + "\naccount: a\nwork_root: /\n", "work_root"},
		{"not yaml of ours", []string{"--spec", "-", "--plan"}, "version: 1\nsurprise: yes\n", "room spec"},
		{"neither plan nor apply", []string{"--spec", "-"}, "", "exactly one of --plan or --apply"},
		{"no spec", []string{"--plan"}, "", "--spec"},
		{"validate a relative root", []string{"--validate", "--os", "linux", "--work-root", "rel/x"}, "", "not an absolute path"},
		{"validate another user's home", []string{"--validate", "--os", "darwin", "--work-root", "/Users/bob/x", "--account", "al"}, "", "bob's home"},
		{"validate with a spec", []string{"--validate", "--spec", "-"}, "", "no --spec"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := roomSetupCmd()
			var out, errb bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errb)
			cmd.SetIn(strings.NewReader(c.stdin))
			cmd.SetArgs(c.args)
			err := cmd.Execute()
			if err == nil || !errors.Is(err, errAlreadySaid) {
				t.Fatalf("want an already-said error, got %v", err)
			}
			if !strings.Contains(errb.String(), "atrium room setup: ") || !strings.Contains(errb.String(), c.want) {
				t.Fatalf("stderr should say why (%q), got %q", c.want, errb.String())
			}
			if out.Len() != 0 {
				t.Fatalf("a refusal prints no setup rows, got %q", out.String())
			}
		})
	}
}
