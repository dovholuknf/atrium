//go:build integration

package cli

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// A zrok join string comes from the share the running hub wrote down, and with
// no hub running there is none to mint from.
func TestRoomsTokenOverZrokReadsTheRunningHubsShare(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	store, err := hubstore.Open(filepath.Join(keys.Dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := store.Add("vm1", hubstore.TransportZrok)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := joinStringFor(keys, store, r, "", "", "zrok", ""); err == nil ||
		!strings.Contains(err.Error(), "--transport zrok") {
		t.Fatalf("with no hub running, got %v, want a refusal naming --transport zrok", err)
	}

	if err := writeZrokShare(keys, "sharetoken123"); err != nil {
		t.Fatal(err)
	}
	if err := keys.EnsureCA(nil); err != nil {
		t.Fatal(err)
	}
	line, err := joinStringFor(keys, store, r, "", "", "zrok", "")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	if j.Transport != "zrok" || j.ShareToken != "sharetoken123" || j.Name != "vm1" {
		t.Fatalf("minted %+v, want zrok, the written share, and vm1", j)
	}

	clearZrokShare(keys)
	if _, err := os.Stat(zrokShareFile(keys)); !os.IsNotExist(err) {
		t.Fatalf("the share file outlived the hub: %v", err)
	}
}

// `room join --openziti <file.jwt>` reads the token from the file rather than
// treating the path as the token. The enrolment itself needs a controller, so
// what is checked is that the error is about the token's contents.
func TestRoomJoinReadsAJwtFromAFile(t *testing.T) {
	dir := t.TempDir()
	tok, err := link.MintOverlayToken("ziti", "vm1", "atrium-hub", "")
	if err != nil {
		t.Fatal(err)
	}
	jwtFile := filepath.Join(dir, "vm1.jwt")
	if err := os.WriteFile(jwtFile, []byte("not-a-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := joinCmd()
	c.SetArgs([]string{tok, "--dir", dir, "--openziti", jwtFile, "--no-run"})
	err = c.Execute()
	if err == nil {
		t.Fatal("a file holding garbage enrolled")
	}
	if strings.Contains(err.Error(), jwtFile) {
		t.Fatalf("the path was used as the token: %v", err)
	}
}

// `room --detach` refuses a machine that never joined, and leaves a room that
// already answers alone rather than starting a second.
func TestRoomDetachRefusesUnjoinedAndLeavesARunningRoom(t *testing.T) {
	dir := t.TempDir()
	if err := detachRoom(roomLaunch{dir: dir, human: "127.0.0.1:1"}); err == nil ||
		!strings.Contains(err.Error(), "has not joined") {
		t.Fatalf("unjoined, got %v", err)
	}

	tok, err := link.MintOverlayToken("ziti", "vm1", "atrium-hub", "")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := (link.Keys{Dir: dir}).SaveOverlayRoom(j, "vm1", "x.json"); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}
	go srv.Serve(ln)
	defer srv.Close()

	if err := detachRoom(roomLaunch{dir: dir, human: ln.Addr().String()}); err != nil {
		t.Fatalf("a room already answering: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, roomLogName)); !os.IsNotExist(err) {
		t.Fatal("a second room was started beside the one answering")
	}
}
