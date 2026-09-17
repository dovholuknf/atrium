package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// The binaries a hub can hand out, one per platform.
//
// ── the hole this closes ─────────────────────────────────
//
// A hub can only offer binaries it has, and the one it is certain to have is
// the one it is running. That is enough for a fleet of machines like itself
// and nothing to anybody else: a Windows hub with a Linux room has nothing
// that room could execute, so the feature would do nothing for exactly the
// people who have several kinds of machine. Rooms are on OTHER machines. That
// is what makes them rooms.
//
// So a hub may be pointed at a directory of builds and will offer each room
// the one for what it actually runs on.
//
// ── the naming ───────────────────────────────────────────
//
// `atrium2_<goos>_<arch>`, with `.exe` on Windows, which is what `go build`
// produces when a release script loops over platforms and what the artifacts
// of a cross-compiling CI job are already called. Anything that does not parse
// as that is skipped and said so, rather than guessed at: a binary offered to
// the wrong platform is a room that will not start after the swap.
//
// The hub's own binary is added last and only for platforms the directory did
// not already cover, so pointing a hub at a directory of releases means those
// releases win over whatever happens to be running.

// buildsIn reads a directory of per-platform binaries.
func buildsIn(dir, version string) ([]link.Build, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []link.Build
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		goos, arch, ok := platformOf(e.Name())
		if !ok {
			log.Printf("[hub] skipping %s: a build is named atrium2_<os>_<arch>", e.Name())
			continue
		}
		b, err := link.Describe(filepath.Join(dir, e.Name()), version, goos, arch)
		if err != nil {
			log.Printf("[hub] skipping %s: %v", e.Name(), err)
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

// platformOf reads `atrium2_linux_amd64` or `atrium2_windows_amd64.exe`.
func platformOf(name string) (goos, arch string, ok bool) {
	name = strings.TrimSuffix(name, ".exe")
	parts := strings.Split(name, "_")
	if len(parts) != 3 || parts[0] != "atrium2" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// hubBuilds is everything this hub can offer: the directory it was pointed at,
// plus its own binary for any platform that directory did not cover.
func hubBuilds(dir, version string) []link.Build {
	var out []link.Build
	if strings.TrimSpace(dir) != "" {
		got, err := buildsIn(dir, version)
		if err != nil {
			log.Printf("[hub] cannot read %s, so only this machine's build is offered: %v", dir, err)
		}
		out = got
	}
	for _, b := range out {
		if b.OS == runtime.GOOS && b.Arch == runtime.GOARCH {
			// The directory has one for this platform already, and a release
			// somebody put there beats whatever happens to be running.
			return out
		}
	}
	self, err := link.Offered(version)
	if err != nil {
		log.Printf("[hub] cannot describe this binary, so rooms like this one are offered nothing: %v", err)
		return out
	}
	return append(out, self)
}

// saysWhatItHas is the startup line, because "upgrades are configured" is a
// thing to be able to see rather than infer from a room's behaviour later.
func saysWhatItHas(builds []link.Build) string {
	if len(builds) == 0 {
		return ""
	}
	names := make([]string, 0, len(builds))
	for _, b := range builds {
		names = append(names, fmt.Sprintf("%s/%s", b.OS, b.Arch))
	}
	return strings.Join(names, ", ")
}
