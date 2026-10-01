package hubstore

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

// The secret checks a hub document passes before it is stored.
//
// ── a speed bump, not a guarantee ───────────────────────
//
// These catch the shapes that are unambiguous and nothing else. They cannot see
// a secret in a screenshot, in a password written as a sentence, or split over
// two lines, and the publish tool says so. What they are for is the accident: a
// model that reads `.env` to be helpful and pastes it into a report.
//
// ── two checks, and the second runs on every way in ─────
//
// BY NAME, on the RESOLVED target of a path (SecretFileName). Checking the name
// that was asked for is not enough: a link `notes.md -> .env` inside the card
// passes containment and passes a name check on `notes.md`. The caller resolves
// first and asks about the result.
//
// BY CONTENT (SecretContent), on `path`, on `content` AND on a board upload. An
// agent can read `.env` itself and hand it over as `content`, so a check on
// `path` alone would be a door left open beside a locked one. DocAdd runs it, so
// there is no way into the store that skips it.

// Rule names, as the API answers them. Part of the contract with the board.
const (
	RuleFileName = "secret-file-name"
	RulePEM      = "pem-private-key"
	RuleGitHub   = "github-token"
	RuleAWS      = "aws-access-key"
	RuleSlack    = "slack-token"
	RuleJWT      = "jwt"
	RuleZrok     = "zrok-token"
)

type contentRule struct {
	name string
	// hint is a literal every match contains. Checked first, because a regular
	// expression over twenty megabytes of image is the slow way to say no.
	hints [][]byte
	re    *regexp.Regexp
}

var contentRules = []contentRule{
	{RulePEM, [][]byte{[]byte("PRIVATE KEY")},
		regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY( BLOCK)?-----`)},
	{RuleGitHub, [][]byte{[]byte("gh"), []byte("github_pat_")},
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{22,}`)},
	{RuleAWS, [][]byte{[]byte("AKIA")}, regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{RuleSlack, [][]byte{[]byte("xox")}, regexp.MustCompile(`xox[abeoprs]-[A-Za-z0-9-]{10,}`)},
	{RuleJWT, [][]byte{[]byte("eyJ")},
		regexp.MustCompile(`eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}`)},
	// A zrok token has no shape of its own: an account token and a share token are
	// both twelve letters and digits, which is every English word's cousin. What is
	// recognisable is where one is put, so this names the places. That is why the
	// rule is weaker than the others, and said so in the notes.
	{RuleZrok, caseVariants("zrok"),
		regexp.MustCompile(`(?i)zrok\s+(?:enable|access\s+private|reserve\s+private)\s+(?:--\S+\s+)*[A-Za-z0-9]{8,}` +
			`|(?i:zrok[_-]?(?:account[_-]?|admin[_-]?|api[_-]?|share[_-]?)?token)["']?\s*[=:]\s*["']?[A-Za-z0-9]{8,}` +
			`|--zrok-private[ =]+[A-Za-z0-9]{8,}`)},
}

// caseVariants is every upper and lower case spelling of a literal, so a hint can be matched
// with bytes.Contains and still agree with a (?i) pattern. A hint that is case-sensitive while its
// pattern is not would let `ZROK_TOKEN=...` skip the check.
func caseVariants(lit string) [][]byte {
	out := [][]byte{nil}
	for _, r := range lit {
		lo, up := strings.ToLower(string(r)), strings.ToUpper(string(r))
		var next [][]byte
		for _, p := range out {
			next = append(next, append(append([]byte{}, p...), lo...))
			if up != lo {
				next = append(next, append(append([]byte{}, p...), up...))
			}
		}
		out = next
	}
	return out
}

// SecretContent names the first rule the bytes break, or "".
func SecretContent(b []byte) string {
	for _, r := range contentRules {
		hit := false
		for _, h := range r.hints {
			if bytes.Contains(b, h) {
				hit = true
				break
			}
		}
		if hit && r.re.Match(b) {
			return r.name
		}
	}
	return ""
}

// secretDirs are directories whose every file is refused, at any depth, lower case. Each holds
// credentials by design: git's, ssh's, zrok's environment, the cloud CLIs', gpg's and docker's.
var secretDirs = map[string]bool{
	".git": true, ".ssh": true, ".zrok": true, ".zrok2": true,
	".aws": true, ".kube": true, ".gnupg": true, ".docker": true,
}

// secretNames are exact names, lower case.
var secretNames = map[string]bool{
	".npmrc": true, ".netrc": true, ".pgpass": true, ".git-credentials": true, ".htpasswd": true,
	// Atrium's own, from the hub's key directory.
	"zrok-share": true, "room.json": true, "hub.db": true, "ca.key": true, "ca.crt": true,
}

// SecretFileName reports whether a path, which the caller has ALREADY RESOLVED
// through symlinks and made relative to the card, is a file that is refused by
// name. Case-insensitive, because `.ENV` is the same file on two of the three
// systems atrium runs on. The match is on every segment for the directories
// (`.git/`, `.ssh/`) and on the last for the file.
func SecretFileName(rel string) bool {
	rel = strings.ReplaceAll(rel, `\`, "/")
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		seg = strings.ToLower(strings.TrimSpace(seg))
		if seg == "" {
			continue
		}
		if i < len(segs)-1 {
			if secretDirs[seg] {
				return true
			}
			continue
		}
		if secretDirs[seg] || secretNames[seg] {
			return true
		}
		switch {
		case strings.HasPrefix(seg, ".env"),
			strings.HasPrefix(seg, "id_rsa"),
			strings.HasPrefix(seg, "id_ed25519"),
			strings.HasPrefix(seg, "id_ecdsa"),
			strings.HasPrefix(seg, "id_dsa"),
			strings.HasPrefix(seg, "credentials"),
			strings.HasSuffix(seg, ".pem"),
			strings.HasSuffix(seg, ".key"),
			strings.HasSuffix(seg, ".p12"),
			strings.HasSuffix(seg, ".pfx"),
			strings.HasSuffix(seg, ".kdbx"),
			strings.HasSuffix(seg, ".token"):
			return true
		}
		// `.env.local` and `prod.env` are both the shape. A trailing `.env` is
		// the second.
		if path.Ext(seg) == ".env" {
			return true
		}
	}
	return false
}
