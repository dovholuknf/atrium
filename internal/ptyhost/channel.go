package ptyhost

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// Address is where the host for a state dir listens, and where a daemon dials it. A pure function of the state
// dir's full path and the user, so two atriums for two state dirs or two users on one machine never meet, and the
// name reveals no path (design G6).
//
// Windows: a named pipe, `\\.\pipe\atrium-ptyhost-<hash>`. The pipe namespace is flat and global, which is why the
// name has to be a hash rather than something read out of the state dir.
//
// Elsewhere: a unix socket `ptyhost-<hash>.sock` inside the state dir, at mode 0600. Mind sun_path, about 104
// bytes on macOS and 108 on Linux: a state dir path near that limit will not bind.
func Address(stateDir string) string {
	h := channelHash(stateDir)
	return channelAddress(stateDir, h)
}

func channelHash(stateDir string) string {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		abs = stateDir
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs + "\x00" + userIdentity()))
	return hex.EncodeToString(sum[:8])
}
