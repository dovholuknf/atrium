package shared

import (
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// MaxPeerMessage bounds one peer message.
const MaxPeerMessage = 8000

// IsClaude reports whether a runner row runs Claude Code, which is the one
// runner `--settings` means anything to.
func IsClaude(h *store.Harness) bool {
	return RunsAs(h, "claude")
}

func RunsAs(h *store.Harness, name string) bool {
	if h == nil {
		return false
	}
	if strings.EqualFold(h.ID, name) {
		return true
	}
	leaf := strings.ToLower(filepath.Base(filepath.FromSlash(strings.TrimSpace(h.Cmd))))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		leaf = strings.TrimSuffix(leaf, ext)
	}
	return leaf == name
}

// TruncatePeer bounds a notice to what a peer message may carry.
func TruncatePeer(text string) string {
	if len(text) > MaxPeerMessage {
		return text[:MaxPeerMessage]
	}
	return text
}
