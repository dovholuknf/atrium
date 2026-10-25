package land

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSigner signs the way `git commit-tree -S` does, from the repository's own config: gpg.format (openpgp or ssh),
// gpg.program or gpg.ssh.program, and user.signingkey. It starts one signing process per commit and never reads a key.
func GitSigner(cfg func(key string) string) (Signer, error) {
	key := cfg("user.signingkey")
	switch format := cfg("gpg.format"); format {
	case "", "openpgp":
		prog := cfg("gpg.openpgp.program")
		if prog == "" {
			prog = cfg("gpg.program")
		}
		if prog == "" {
			prog = "gpg"
		}
		args := []string{"--status-fd=2", "-bsa"}
		if key != "" {
			args = []string{"--status-fd=2", "-bsau", key}
		}
		return func(body []byte) (string, error) {
			cmd := exec.Command(prog, args...)
			cmd.Stdin = bytes.NewReader(body)
			var out, errb bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errb
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("%s: %v: %s", prog, err, strings.TrimSpace(errb.String()))
			}
			if !strings.Contains(errb.String(), "[GNUPG:] SIG_CREATED ") {
				return "", fmt.Errorf("%s made no signature: %s", prog, strings.TrimSpace(errb.String()))
			}
			return out.String(), nil
		}, nil
	case "ssh":
		prog := cfg("gpg.ssh.program")
		if prog == "" {
			prog = "ssh-keygen"
		}
		if key == "" {
			return nil, fmt.Errorf("gpg.format is ssh and user.signingkey is not set")
		}
		// Like git, sign a file: ssh-keygen writes <file>.sig beside it.
		return func(body []byte) (string, error) {
			dir, err := os.MkdirTemp("", "land-sign-")
			if err != nil {
				return "", err
			}
			defer os.RemoveAll(dir)
			file := filepath.Join(dir, "body")
			if err := os.WriteFile(file, body, 0o600); err != nil {
				return "", err
			}
			cmd := exec.Command(prog, "-Y", "sign", "-n", "git", "-f", key, file)
			var errb bytes.Buffer
			cmd.Stderr = &errb
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("%s: %v: %s", prog, err, strings.TrimSpace(errb.String()))
			}
			sig, err := os.ReadFile(file + ".sig")
			return string(sig), err
		}, nil
	default:
		return nil, fmt.Errorf("gpg.format %q is not supported", format)
	}
}
