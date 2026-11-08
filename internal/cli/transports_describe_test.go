package cli

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestDescribePathMasksTheZrokShare(t *testing.T) {
	got := describePath(wire.Path{Transport: "zrok", Share: "abcd1234secret"})
	if !strings.Contains(got, "abcd...") || strings.Contains(got, "1234") {
		t.Fatalf("describePath = %q", got)
	}
}
