package land

import (
	"fmt"
	"os/exec"
	"strings"
)

// AuthorEmail is the only address a landed commit may carry. GitHub refuses a push that exposes a private one (GH007).
const AuthorEmail = "46322585+dovholuknf@users.noreply.github.com"

// CheckEmail refuses any address but AuthorEmail, naming where git read it from so the stray config can be found.
// origin is the output of `git config --show-origin user.email`, or empty when it is not known.
func CheckEmail(email, origin string) error { return CheckEmailIs(AuthorEmail, email, origin) }

// CheckEmailIs is CheckEmail for an operator whose address is want.
func CheckEmailIs(want, email, origin string) error {
	if email == want {
		return nil
	}
	from := strings.TrimSpace(origin)
	if from == "" {
		from = "unknown config"
	}
	return fmt.Errorf("user.email is %q (from %s), not %s: fix that config before rewriting anything", email, from, want)
}

// CheckGitEmail checks user.email as git resolves it in the current directory.
func CheckGitEmail(email string) error {
	out, _ := exec.Command("git", "config", "--show-origin", "user.email").Output()
	return CheckEmail(email, strings.TrimSpace(string(out)))
}
