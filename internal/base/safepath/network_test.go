package safepath

import (
	"errors"
	"runtime"
	"testing"
)

func TestNetworkSpotsSharesAndDevicePaths(t *testing.T) {
	for _, p := range []string{`\\host\share\x`, `//host/share`, `\\?\UNC\host\share`} {
		if !Network(p) {
			t.Errorf("Network(%q) = false", p)
		}
	}
	for _, p := range []string{`/a/b`, `a/b`, ``} {
		if Network(p) {
			t.Errorf("Network(%q) = true", p)
		}
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{`\??\UNC\host\share`, `\\.\pipe\x`} {
			if !Network(p) {
				t.Errorf("Network(%q) = false", p)
			}
		}
		if Network(`C:\work`) {
			t.Error(`Network(C:\work) = true`)
		}
	}
}

func TestContainedNeverResolvesAShareOutsideTheRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC and device paths are Windows only")
	}
	probed := ""
	eval := func(p string) (string, error) {
		if Network(p) {
			probed = p
		}
		return p, nil
	}
	for _, p := range []string{`\\evil\share\x`, `\??\UNC\evil\share\x`, `\\?\UNC\evil\share\x`} {
		if _, err := containedWith(eval, `C:\work`, p); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: err = %v, want ErrOutside", p, err)
		}
	}
	if probed != "" {
		t.Fatalf("resolved %q before refusing it", probed)
	}
}
