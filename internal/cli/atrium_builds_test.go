package cli

import (
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
		// The one binary's names, which a builds directory holds from now on.
		{"atrium_linux_amd64", "linux", "amd64", true},
		{"atrium_windows_amd64.exe", "windows", "amd64", true},
		{"atrium_linux", "", "", false},
		{"atrium3_linux_amd64", "", "", false},
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
