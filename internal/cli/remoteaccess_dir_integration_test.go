//go:build integration

package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
	"github.com/dovholuknf/atrium/internal/daemon"
)

// writeRoomCert puts a loadable room.crt and room.key in dir, which is all that makes it a room's directory here.
func writeRoomCert(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "r"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	must := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("room.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	must("room.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// recordRoom writes the address file a running room leaves, and points this process at it.
func recordRoom(t *testing.T, roomDirName string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(daemon.Location{Agent: "http://localhost:7777", Room: "sg3", RoomDir: roomDirName, PID: 1})
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", p)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
}

func runRA(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := roomRemoteAccessCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestRemoteAccessRefusesADirWithNoRoomCert(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	live := t.TempDir()
	writeRoomCert(t, live)
	recordRoom(t, live)
	bad := t.TempDir()

	for _, args := range [][]string{{"on"}, {"off"}, {}, {"log"}} {
		out, err := runRA(t, append(args, "--dir", bad)...)
		if err == nil {
			t.Fatalf("%v: accepted a dir with no room certificate: %q", args, out)
		}
		if !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), live) {
			t.Fatalf("%v: the refusal names neither dir: %v", args, err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(bad, "*")); len(left) != 0 {
		t.Fatalf("a refused dir was written to: %v", left)
	}
}

func TestRemoteAccessAValidDirWorksAndNamesTheRunningRoom(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	t.Setenv(remoteaccess.EnvForce, "")
	live, other := t.TempDir(), t.TempDir()
	writeRoomCert(t, live)
	writeRoomCert(t, other)
	recordRoom(t, live)

	out, err := runRA(t, "off", "--dir", live)
	if err != nil || !strings.Contains(out, live) || !strings.Contains(out, "running room's") {
		t.Fatalf("off in the running room's dir: %q %v", out, err)
	}
	// A real room that is not the running one is allowed, and the answer says the running room is elsewhere.
	out, err = runRA(t, "off", "--dir", other)
	if err != nil || !strings.Contains(out, "the running room on this machine uses "+live) {
		t.Fatalf("off in another room's dir: %q %v", out, err)
	}
	if _, err := remoteaccess.Load(other); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteAccessWithNoDirSaysWhereItLooked(t *testing.T) {
	t.Setenv("WORKTREE_ROOT", t.TempDir())
	t.Setenv("ATRIUM_LOCATION", filepath.Join(t.TempDir(), "none.json"))
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	_, err := runRA(t)
	if err == nil || !strings.Contains(err.Error(), roomDir()) || !strings.Contains(err.Error(), "looked in") {
		t.Fatalf("no room found: %v", err)
	}
}

func TestRemoteAccessWithNoDirPrefersTheRunningRoomOverAStaleDefault(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	t.Setenv(remoteaccess.EnvForce, "")
	t.Setenv("WORKTREE_ROOT", t.TempDir())
	if err := os.MkdirAll(roomDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRoomCert(t, roomDir())
	live := t.TempDir()
	writeRoomCert(t, live)
	recordRoom(t, live)

	out, err := runRA(t, "off")
	if err != nil || !strings.Contains(out, "room directory: "+live) {
		t.Fatalf("off with no dir: %q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(live, "remote_access.json")); err != nil {
		t.Fatalf("nothing written in the running room's dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(roomDir(), "remote_access.json")); err == nil {
		t.Fatal("wrote into the stale default")
	}
}
