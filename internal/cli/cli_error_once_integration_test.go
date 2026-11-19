//go:build integration

package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestARunEErrorPrintsOneLine(t *testing.T) {
	root := newRoot()
	root.AddCommand(&cobra.Command{Use: "fails", RunE: func(*cobra.Command, []string) error { return errors.New("boom") }})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if code := runRoot(root, []string{"fails"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := errOut.String(); got != "atrium: boom\n" {
		t.Errorf("stderr = %q, want exactly one line %q", got, "atrium: boom\n")
	}
}
