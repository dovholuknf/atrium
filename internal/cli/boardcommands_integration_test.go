//go:build integration

package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var boardCommand = regexp.MustCompile(`(?:<code>|<pre>)(atrium2?(?: [^<\n]+)?)`)

var boardWord = regexp.MustCompile(`^[a-z][a-z-]*$`)

func TestEveryCommandTheBoardShowsExistsInTheCLI(t *testing.T) {
	web := filepath.Join("..", "api", "web")
	root := newRoot()
	seen := 0
	err := filepath.WalkDir(web, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(filepath.ToSlash(path), "/vendor/") {
			return err
		}
		if ext := filepath.Ext(path); ext != ".js" && ext != ".html" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range boardCommand.FindAllStringSubmatch(string(b), -1) {
			fields := strings.Fields(m[1])
			if fields[0] != "atrium" {
				t.Errorf("%s shows %q, and there is no such binary", path, m[1])
				continue
			}
			var words []string
			for _, f := range fields[1:] {
				if !boardWord.MatchString(f) {
					break
				}
				words = append(words, f)
			}
			if len(words) == 0 {
				continue
			}
			seen++
			cmd, rest, err := root.Find(words)
			if err != nil || cmd == root {
				t.Errorf("%s shows %q, and the CLI has no such command", path, m[1])
				continue
			}
			if len(rest) > 0 && cmd.HasSubCommands() {
				t.Errorf("%s shows %q, and %q has no subcommand %q", path, m[1], cmd.CommandPath(), rest[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("the board shows no atrium command, so this test reads nothing")
	}
}
