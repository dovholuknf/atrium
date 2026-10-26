package land

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Commit is one commit as `git rev-list --header` prints it.
type Commit struct {
	SHA       string
	Tree      string
	Parents   []string
	Author    string // "Name <email> <unix> <tz>"
	Committer string
	Signed    bool
	Msg       string // the message, without rev-list's four-space indent
}

// ParseRevList reads the output of `git rev-list --header`, whose records end in NUL.
func ParseRevList(out []byte) ([]Commit, error) {
	var cs []Commit
	for _, rec := range strings.Split(string(out), "\x00") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		sha, rest, _ := strings.Cut(rec, "\n")
		hdr, msg, _ := strings.Cut(rest, "\n\n")
		c := Commit{SHA: strings.TrimSpace(sha)}
		for _, l := range strings.Split(hdr, "\n") {
			if strings.HasPrefix(l, " ") {
				continue // a continuation of a gpgsig or mergetag
			}
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "tree":
				c.Tree = v
			case "parent":
				c.Parents = append(c.Parents, v)
			case "author":
				c.Author = v
			case "committer":
				c.Committer = v
			case "gpgsig", "gpgsig-sha256":
				c.Signed = true
			}
		}
		if c.Tree == "" || c.Author == "" || c.Committer == "" {
			return nil, fmt.Errorf("rev-list record for %s has no tree, author or committer", c.SHA)
		}
		var lines []string
		for _, l := range strings.Split(msg, "\n") {
			lines = append(lines, strings.TrimPrefix(l, "    "))
		}
		c.Msg = strings.Join(lines, "\n")
		cs = append(cs, c)
	}
	return cs, nil
}

// Message is what filter-branch's msg filter made of a message: blank and whitespace-only lines dropped, the first line kept, trailing
// whitespace trimmed, then the scrub rules.
func Message(raw string) string {
	for _, l := range strings.Split(raw, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		return Scrub(strings.TrimRight(l, " \t\r"))
	}
	return Scrub("")
}

// Identity is who every landed commit is by.
type Identity struct{ Name, Email string }

// stamp is the "<unix> <tz>" at the end of an author or committer line.
func stamp(line string) string {
	f := strings.Fields(line)
	if len(f) < 2 {
		return line
	}
	return f[len(f)-2] + " " + f[len(f)-1]
}

// Signer returns an armored detached signature of a commit body. Nil means do not sign.
type Signer func(body []byte) (string, error)

// Body is the commit object's content, as `git commit-tree` writes it. sig is the armored signature, or "".
func Body(tree string, parents []string, id Identity, author, committer, msg, sig string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "tree %s\n", tree)
	for _, p := range parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	fmt.Fprintf(&b, "author %s <%s> %s\n", id.Name, id.Email, stamp(author))
	fmt.Fprintf(&b, "committer %s <%s> %s\n", id.Name, id.Email, stamp(committer))
	if sig != "" {
		b.WriteString("gpgsig " + strings.ReplaceAll(strings.TrimRight(sig, "\n"), "\n", "\n ") + "\n")
	}
	b.WriteString("\n" + msg)
	return []byte(b.String())
}

// Rewrite writes a new commit for each of cs, which must list parents before children, and returns old sha to new.
// A parent outside cs keeps its sha. A parent that two old parents collapse to is listed once, as commit-tree does.
// put stores an object and returns nothing: its sha is computed here.
func Rewrite(cs []Commit, id Identity, sign Signer, put func(sha string, body []byte) error) (map[string]string, error) {
	newSHA := make(map[string]string, len(cs))
	for _, c := range cs {
		var parents []string
		seen := map[string]bool{}
		for _, p := range c.Parents {
			if np, ok := newSHA[p]; ok {
				p = np
			}
			if !seen[p] {
				seen[p] = true
				parents = append(parents, p)
			}
		}
		msg := Message(c.Msg)
		body := Body(c.Tree, parents, id, c.Author, c.Committer, msg, "")
		if sign != nil {
			sig, err := sign(body)
			if err != nil {
				return nil, fmt.Errorf("signing %s: %w", c.SHA[:10], err)
			}
			body = Body(c.Tree, parents, id, c.Author, c.Committer, msg, sig)
		}
		sha := Hash(body)
		if err := put(sha, body); err != nil {
			return nil, err
		}
		newSHA[c.SHA] = sha
	}
	return newSHA, nil
}

// Hash is the sha1 git gives a commit object with this body.
func Hash(body []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "commit %d\x00", len(body))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// LooseWriter stores commit objects as loose files under objectsDir.
func LooseWriter(objectsDir string) func(sha string, body []byte) error {
	return func(sha string, body []byte) error {
		dir := filepath.Join(objectsDir, sha[:2])
		path := filepath.Join(dir, sha[2:])
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		fmt.Fprintf(w, "commit %d\x00", len(body))
		w.Write(body)
		if err := w.Close(); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, "tmp_obj_")
		if err != nil {
			return err
		}
		if _, err := tmp.Write(z.Bytes()); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmp.Name())
			return err
		}
		os.Chmod(tmp.Name(), 0o444)
		if err := os.Rename(tmp.Name(), path); err != nil {
			os.Remove(tmp.Name())
			if _, serr := os.Stat(path); serr == nil {
				return nil // another writer made the same object
			}
			return err
		}
		return nil
	}
}

// Verify checks landed commits and returns one line per problem: a message that is not one line, names the agent or
// a co-author, an author or committer who is not id, or no signature when wantSigned.
func Verify(cs []Commit, id Identity, wantSigned bool) []string {
	var bad []string
	who := id.Name + " <" + id.Email + ">"
	for _, c := range cs {
		short := c.SHA
		if len(short) > 10 {
			short = short[:10]
		}
		msg := strings.TrimRight(c.Msg, "\n \t")
		switch {
		case msg == "":
			bad = append(bad, short+": empty message")
		case strings.Contains(msg, "\n"):
			bad = append(bad, short+": message is more than one line")
		}
		if m := Banned.FindString(c.Msg); m != "" {
			bad = append(bad, fmt.Sprintf("%s: message contains %q", short, m))
		}
		if !strings.HasPrefix(c.Author, who+" ") {
			bad = append(bad, short+": author is "+c.Author)
		}
		if !strings.HasPrefix(c.Committer, who+" ") {
			bad = append(bad, short+": committer is "+c.Committer)
		}
		if wantSigned && !c.Signed {
			bad = append(bad, short+": not signed")
		}
	}
	return bad
}
