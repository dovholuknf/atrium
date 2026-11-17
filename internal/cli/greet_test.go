package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The board address the hub prints is 127.0.0.1, since localhost costs an IPv6 fallback on Windows.
func TestGreetPrintsTheBoardAsLoopbackIP(t *testing.T) {
	store, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	greet(store, &hubSide{says: "x"}, ":7778", "id", false)
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "http://127.0.0.1:7778") || strings.Contains(string(out), "localhost") {
		t.Fatalf("out = %q", out)
	}
}
