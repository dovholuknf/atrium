//go:build integration

package docshare

import "testing"

// A SECRET FILE NAME IS REFUSED, case-insensitively, on the path it was resolved to.
func TestSecretFileNames(t *testing.T) {
	for _, n := range []string{".env", ".ENV", ".env.local", "deploy/.Env.prod", "prod.env", "server.PEM", "a/b/tls.key",
		"id_rsa", "id_rsa.pub", "id_dsa", "ID_DSA.pub", ".htpasswd", ".HTPASSWD", "vault.kdbx", "Pass.KDBX",
		".zrok/environment.json", "home/.zrok2/x", ".aws/credentials", ".AWS/config", ".kube/config", "a/.gnupg/pubring.kbx",
		".docker/config.json", "x.p12", "x.pfx", ".npmrc", ".NETRC", "credentials", "Credentials.json",
		".git/config", "sub/.git/hooks/x", ".ssh/known_hosts", "a/.SSH/id", "zrok-share", "room.json", "ca.key"} {
		if !SecretFileName(n) {
			t.Errorf("%q was allowed", n)
		}
	}
	for _, n := range []string{"notes.md", "environment.md", "docs/key-ideas.md", "keyboard.txt", "awsome.md", "zrok-notes.md", "docker.md", "report.md", ".gitignore", "env.md", "git/readme.md"} {
		if SecretFileName(n) {
			t.Errorf("%q was refused", n)
		}
	}
}

// The Windows spellings of a secret name are refused, and the near misses are not.
func TestSecretFileNamesWindowsForms(t *testing.T) {
	for _, n := range []string{"key.pem.", "key.pem ", "key.pem...", "key.pem. . ", "a.pem::$DATA", "a.pem:alt",
		".env:alt", ".env.", "sub/id_rsa.", `sub\key.pem.`, ".git./config", ".git::$INDEX_ALLOCATION/config",
		".ssh /id", "x.key::$DATA", "ca.key. "} {
		if !SecretFileName(n) {
			t.Errorf("%q was allowed", n)
		}
	}
	for _, n := range []string{"notes.md.", "notes.md ", "notes.md::$DATA", "notes.md:alt", "a.b.", "...", ":", "a:.pem.md",
		"git./readme.md", "key.pem.md."} {
		if SecretFileName(n) {
			t.Errorf("%q was refused", n)
		}
	}
}
