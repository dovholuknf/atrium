// Package sshkey is the one rule for a public key that crosses between rooms. The hub applies it before it stores a
// key, and a room applies it again before it writes one into authorized_keys, because a room does not trust what the
// hub relayed any more than the hub trusts what a room sent.
package sshkey

import (
	"encoding/base64"
	"errors"
	"strings"
)

// Clean reads a public key line and returns it as `ssh-ed25519 <base64>`, the comment dropped. Only ssh-ed25519, on
// one line, with no options in front and a body that decodes as base64.
func Clean(line string) (string, error) {
	line = strings.TrimSpace(line)
	if strings.ContainsAny(line, "\r\n") {
		return "", errors.New("an ssh key is one line")
	}
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return "", errors.New("an ssh key here is `ssh-ed25519 <base64>`")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) == 0 || len(blob) > 256 {
		return "", errors.New("that ssh key's body is not base64")
	}
	return f[0] + " " + f[1], nil
}
