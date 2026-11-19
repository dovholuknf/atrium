//go:build integration

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A room that was added and never connected is removed without --force.
func TestRoomThatNeverConnectedRemovesWithoutForce(t *testing.T) {
	db := filepath.Join(t.TempDir(), "hub.db")
	st, err := hubstore.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add("ghost", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	st.Close()
	out, err := runRooms(t, "rm", "ghost", "--atrium-db", db)
	if err != nil || !strings.Contains(out, "gone") {
		t.Fatalf("rm: %v %s", err, out)
	}
}
