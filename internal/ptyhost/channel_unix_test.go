//go:build !windows

package ptyhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A state dir whose socket path would pass sun_path gets a short per-user address that binds, and a path that
// cannot be shortened names the limit.
func TestAddressFallsBackToAShortDirWhenTheStateDirIsTooLong(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	addr := Address(long)
	if len(addr) > maxSocketPath {
		t.Fatalf("address is %d bytes, over %d: %s", len(addr), maxSocketPath, addr)
	}
	if filepath.Dir(addr) != shortSocketDir() {
		t.Fatalf("address %s is not under %s", addr, shortSocketDir())
	}
	short := t.TempDir()
	if len(filepath.Join(short, "ptyhost-0123456789abcdef.sock")) <= maxSocketPath {
		if got := Address(short); filepath.Dir(got) != short {
			t.Fatalf("a short state dir moved its socket: %s", got)
		}
	}
	ln, err := listenChannel(addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	_ = os.Remove(addr)
}

func TestListenChannelNamesTheSocketPathLimit(t *testing.T) {
	_, err := listenChannel("/" + strings.Repeat("a", 150) + ".sock")
	if err == nil || !strings.Contains(err.Error(), "104") {
		t.Fatalf("err = %v, want one naming the limit", err)
	}
}
