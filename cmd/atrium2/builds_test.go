package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A NAME THAT DOES NOT PARSE IS SKIPPED, NOT GUESSED AT.
//
// The platform is read from the filename and nothing checks it afterwards: the
// room trusts the hub about what a binary is for. So a name this cannot read
// must produce no offer at all. Guessing would mean a room swapping in a
// binary for another operating system and not starting again.
func TestOnlyWellNamedBuildsAreOffered(t *testing.T) {
	for _, c := range []struct {
		name       string
		goos, arch string
		ok         bool
	}{
		{"atrium2_linux_amd64", "linux", "amd64", true},
		{"atrium2_windows_amd64.exe", "windows", "amd64", true},
		{"atrium2_darwin_arm64", "darwin", "arm64", true},
		// Everything a release directory actually contains besides builds.
		{"atrium2", "", "", false},
		{"atrium2_linux", "", "", false},
		{"atrium2_linux_amd64_v2", "", "", false},
		{"atrium.exe", "", "", false},
		{"checksums.txt", "", "", false},
		{"_linux_amd64", "", "", false},
		{"atrium2__amd64", "", "", false},
		{"atrium2_linux_", "", "", false},
	} {
		goos, arch, ok := platformOf(c.name)
		if ok != c.ok || goos != c.goos || arch != c.arch {
			t.Errorf("%s read as %q/%q ok=%v, expected %q/%q ok=%v",
				c.name, goos, arch, ok, c.goos, c.arch, c.ok)
		}
	}
}

// A hub with no directory still offers its own binary, so the common case --
// one machine, or a fleet that matches it -- needs no configuration.
func TestAHubAlwaysOffersItsOwnBuild(t *testing.T) {
	got := hubBuilds("", "v9")
	if len(got) != 1 {
		t.Fatalf("offered %d builds", len(got))
	}
	if got[0].OS != runtime.GOOS || got[0].Arch != runtime.GOARCH {
		t.Errorf("its own build was described as %s/%s", got[0].OS, got[0].Arch)
	}
	if got[0].SHA256 == "" || got[0].Size <= 0 {
		t.Error("its own build was offered without a hash or a size")
	}
}

// A RELEASE SOMEBODY PUT THERE BEATS WHATEVER IS RUNNING. A hub pointed at a
// directory of builds is being told what to hand out, and its own binary is
// the fallback for platforms that directory did not cover.
func TestADirectoryWinsForItsOwnPlatform(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "atrium2_"+runtime.GOOS+"_"+runtime.GOARCH+exeSuffix())
	if err := os.WriteFile(mine, []byte("not really a binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "atrium2_plan9_mips")
	if err := os.WriteFile(other, []byte("nor is this"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := hubBuilds(dir, "v9")
	if len(got) != 2 {
		t.Fatalf("offered %d builds, expected the two in the directory", len(got))
	}
	for _, b := range got {
		if b.OS == runtime.GOOS && b.Arch == runtime.GOARCH && b.Path != mine {
			t.Errorf("this platform was answered with %s rather than the one in the directory", b.Path)
		}
	}
}

// A directory that does not cover this platform still leaves rooms like this
// hub with something to take.
func TestItsOwnBuildFillsTheGap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "atrium2_plan9_mips"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := hubBuilds(dir, "v9")
	if len(got) != 2 {
		t.Fatalf("offered %d builds", len(got))
	}
	var mine bool
	for _, b := range got {
		if b.OS == runtime.GOOS && b.Arch == runtime.GOARCH {
			mine = true
		}
	}
	if !mine {
		t.Error("a hub offered nothing to a room like itself")
	}
}
