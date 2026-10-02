package gitsync

import (
	"strings"
)

// maxRefLen bounds a ref name. A path component of a loose ref is a file name on a disk that may be NTFS.
const maxRefLen = 200

// Refs a push may name.
const (
	headsPrefix = "refs/heads/"
	tagsPrefix  = "refs/tags/"
	// ClaudeMainRef is the second branch only the operator moves.
	ClaudeMainRef = "refs/heads/claude/main"
)

// checkRefName says why a ref name is not one the hub takes, or "". It is a strict ASCII allowlist on top
// of git's rules, because a branch name lands as a file name on the room's disk, and sg4's is NTFS, and
// because the name goes into a log, a sentence and a board.
//
// What git's `check-ref-format` also refuses is checked by the caller, after this. This comes first, so a
// name with a control character never reaches a process.
func checkRefName(ref string) string {
	rest, ok := strings.CutPrefix(ref, headsPrefix)
	if !ok {
		rest, ok = strings.CutPrefix(ref, tagsPrefix)
	}
	if !ok {
		return "only branches (refs/heads) and tags (refs/tags) can be pushed"
	}
	if rest == "" || len(ref) > maxRefLen {
		return "that ref name is empty or too long"
	}
	if strings.Contains(rest, "..") || strings.Contains(rest, "//") || strings.Contains(rest, "@{") {
		return "that ref name has .. or // or @{ in it"
	}
	if strings.HasSuffix(rest, "/") || strings.HasSuffix(rest, ".") {
		return "that ref name ends in a slash or a dot"
	}
	for _, part := range strings.Split(rest, "/") {
		switch {
		case part == "":
			return "that ref name has an empty part"
		case strings.HasPrefix(part, "."):
			return "a part of that ref name starts with a dot"
		case strings.HasSuffix(part, ".lock"):
			return "a part of that ref name ends in .lock"
		case part == "@":
			return "a part of that ref name is a bare @"
		case reserved.MatchString(part):
			return "a part of that ref name is a name Windows will not make a file called"
		}
	}
	for _, c := range rest {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("._-/+=,@", c):
		default:
			return "a ref name is letters, digits and . _ - / + = , @ only"
		}
	}
	return ""
}

// isMainRef is a ref only the operator moves.
func isMainRef(ref string) bool { return ref == MainRef || ref == ClaudeMainRef }

// foldedRef is the key two refs share when a disk that ignores case would make them one file.
func foldedRef(ref string) string { return strings.ToLower(ref) }

// dirFileConflict says whether one of two (already folded) ref names is a directory the other is a file
// in: `fix` and `fix/x`.
func dirFileConflict(a, b string) bool {
	return strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
