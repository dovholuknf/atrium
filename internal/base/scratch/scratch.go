package scratch

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// WHERE A CARD PUTS THROWAWAY FILES: one folder per card under the room's scratch root. The room decides the root, so
// nothing here knows about drives or operating systems.

// Env is the variable a runner reads its scratch folder from.
const Env = "ATRIUM_SCRATCH"

var slugRun = regexp.MustCompile(`[^a-z0-9]+`)

// Check refuses a root that is not one absolute path on one line, or that is a drive root.
func Check(root string) (string, error) {
	root = strings.TrimSpace(root)
	if strings.ContainsAny(root, "\x00\n\r") {
		return "", errors.New("scratch_root is one path on one line")
	}
	if !filepath.IsAbs(filepath.FromSlash(root)) {
		return "", fmt.Errorf("scratch_root is an absolute path on this machine, not %q", root)
	}
	abs, err := filepath.Abs(filepath.FromSlash(root))
	if err != nil {
		return "", err
	}
	if filepath.Dir(abs) == abs {
		return "", errors.New("scratch_root is a drive root. name a folder of its own for scratch files")
	}
	return abs, nil
}

// Short is a card's short name: its title, else its wire name, as a lowercase slug cut at 40, then the first eight
// characters of its id, which tells two cards with one title apart.
func Short(title, wire, id string) string {
	slug := ""
	for _, s := range []string{title, wire} {
		g := strings.Trim(slugRun.ReplaceAllString(strings.ToLower(s), "-"), "-")
		if len(g) > 40 {
			g = strings.Trim(g[:40], "-")
		}
		if g != "" {
			slug = g
			break
		}
	}
	if i := strings.LastIndex(id, "~"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 8 {
		id = id[:8]
	}
	switch {
	case slug == "" && id == "":
		return "card"
	case slug == "":
		return id
	case id == "":
		return slug
	}
	return slug + "-" + id
}

// Dir is the folder for a card under a root, in forward slashes. Empty for an unset root.
func Dir(root, title, wire, id string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(root), Short(title, wire, id)))
}

// Line is the sentence the session-start hook carries.
func Line(dir string) string {
	return "Throwaway files (patches, logs, downloads, notes) go in " + dir + ". Never C:\\ or the system temp directory."
}
