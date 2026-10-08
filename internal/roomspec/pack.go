package roomspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// RecordFile is the pack's record inside the runner's folder, in the shape provision-room.ps1 wrote it, so a room provisioned
// by either is read by both.
const RecordFile = "atrium-agent-pack.json"

// PackSource is a pack as fetched: the commit, and each file as a path relative to the pack's folder ("agents/x.md",
// "skills/n/SKILL.md") with its bytes. Pin repo and commit, never vendor.
type PackSource struct {
	Repo, Branch, Commit string
	Files                map[string][]byte
	// Skipped names what a pack is not given: a symlink in the repo (a skill that is a link into another repository).
	Skipped []string
}

// PackRecord is the record of what was installed and from where.
type PackRecord struct {
	Repo        string            `json:"repo"`
	Branch      string            `json:"branch"`
	Commit      string            `json:"commit"`
	Agents      []string          `json:"agents"`
	Skills      []string          `json:"skills"`
	Files       map[string]string `json:"files"`
	InstalledAt string            `json:"installed_at,omitempty"`
}

// PackResult is what an install did, or would do.
type PackResult struct {
	Files, Changed int
	// Refused are files not written because a folder on their way is a link: the write would leave the runner's folder.
	Refused []string
	// Edited are files that were there, differ from the pack, and are not what the last pack wrote: someone edited them. They
	// are replaced, and said.
	Edited        []string
	RecordWritten bool
	Was           string // the commit the record named before
}

// Fetcher gets a pack. Fetch writes only to its own temp folder, and is for apply. Latest asks the hub's mirror for a branch's
// commit and changes nothing, so a plan may.
type Fetcher interface {
	Fetch(repo, branch, from string) (*PackSource, error)
	Latest(repo, branch string) (string, error)
}

// Sha is a file's SHA-256 in hex, the form the record uses.
func Sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// NewRecord is the record for a pack.
func NewRecord(src *PackSource) PackRecord {
	rec := PackRecord{Repo: src.Repo, Branch: src.Branch, Commit: src.Commit, Files: map[string]string{}, Agents: []string{}, Skills: []string{}}
	skills := map[string]bool{}
	for rel, b := range src.Files {
		rec.Files[rel] = Sha(b)
		parts := strings.Split(rel, "/")
		switch {
		case len(parts) == 2 && parts[0] == "agents" && strings.HasSuffix(parts[1], ".md"):
			rec.Agents = append(rec.Agents, strings.TrimSuffix(parts[1], ".md"))
		case len(parts) >= 3 && parts[0] == "skills":
			skills[parts[1]] = true
		}
	}
	for s := range skills {
		rec.Skills = append(rec.Skills, s)
	}
	sort.Strings(rec.Agents)
	sort.Strings(rec.Skills)
	return rec
}

// ReadRecord reads the installed record, or nil when there is none or it cannot be read.
func ReadRecord(r ReadFS, dir string) *PackRecord {
	b, err := r.ReadFile(dir + "/" + RecordFile)
	if err != nil {
		return nil
	}
	var rec PackRecord
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), bom)), &rec) != nil || rec.Commit == "" {
		return nil
	}
	return &rec
}

// base is what every adapter does the same way. The OS adapters embed it and add only what differs.
type base struct{}

// MakeDirs makes each folder that is not there. 0755: the account's own tree.
func (base) MakeDirs(f FS, dirs []string) (int, error) {
	made := 0
	for _, d := range dirs {
		if _, err := f.Lstat(d); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return made, fmt.Errorf("%s: %w", d, err)
		}
		if err := f.MkdirAll(d, 0o755); err != nil {
			return made, fmt.Errorf("make %s: %w", d, err)
		}
		made++
	}
	return made, nil
}

// InstallPack puts each file of the pack in dir as a real file. Rules, each one a review finding:
//   - a folder BELOW dir that is a link or a junction would send the write to wherever it points, possibly outside dir: the
//     file is refused and named. dir itself may be a link (a dotfiles layout): it is the account's.
//   - a file that is a link, or a folder where a file belongs, is replaced by the real file.
//   - a file that is there, differs from the pack, and is not what the previous record says the last pack wrote has been edited
//     here: it is replaced, and named in Edited.
//   - nothing else in dir is touched, and the record is written only when files changed or the commit differs, and never when a
//     file was refused (so a rerun tries again and the lock does not claim the pack is current).
//
// With apply false nothing is written and the result says what would be.
func (base) InstallPack(f FS, dir string, src *PackSource, prev *PackRecord, apply bool) (PackResult, error) {
	var res PackResult
	if prev != nil {
		res.Was = prev.Commit
	}
	rels := make([]string, 0, len(src.Files))
	for rel := range src.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		res.Files++
		if unsafeRel(rel) {
			res.Refused = append(res.Refused, fmt.Sprintf("%s (a path that is not inside the pack)", rel))
			continue
		}
		if lp := linkedParent(f, dir, rel); lp != "" {
			res.Refused = append(res.Refused, fmt.Sprintf("%s (%s is a link)", rel, lp))
			continue
		}
		to := dir + "/" + rel
		want := Sha(src.Files[rel])
		have := ""
		fi, err := f.Lstat(to)
		if err == nil && !IsLink(fi) && !fi.IsDir() {
			if b, rerr := f.ReadFile(to); rerr == nil {
				have = Sha(b)
			}
		}
		if have == want {
			continue
		}
		if have != "" {
			var was string
			if prev != nil {
				was = prev.Files[rel]
			}
			if have != was {
				res.Edited = append(res.Edited, rel)
			}
		}
		res.Changed++
		if !apply {
			continue
		}
		if err == nil && (IsLink(fi) || fi.IsDir()) {
			if rerr := f.RemoveAll(to); rerr != nil {
				return res, fmt.Errorf("replace %s: %w", to, rerr)
			}
		}
		if merr := f.MkdirAll(path.Dir(to), 0o755); merr != nil {
			return res, fmt.Errorf("make %s: %w", path.Dir(to), merr)
		}
		mode := fs.FileMode(0o644)
		if strings.HasPrefix(string(src.Files[rel]), "#!") {
			mode = 0o755
		}
		if werr := f.WriteFile(to, src.Files[rel], mode); werr != nil {
			return res, fmt.Errorf("write %s: %w", to, werr)
		}
	}
	if apply && len(res.Refused) == 0 && (res.Changed > 0 || prev == nil || prev.Commit != src.Commit) {
		rec := NewRecord(src)
		rec.InstalledAt = time.Now().UTC().Format(time.RFC3339)
		b, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return res, err
		}
		if err := f.MkdirAll(dir, 0o755); err != nil {
			return res, err
		}
		if err := f.WriteFile(dir+"/"+RecordFile, append(b, '\n'), 0o644); err != nil {
			return res, fmt.Errorf("write the pack record: %w", err)
		}
		res.RecordWritten = true
	}
	return res, nil
}

// unsafeRel is a pack path that is absolute, has a drive or a backslash, or climbs: a fetcher is not trusted to have cleaned it.
func unsafeRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\\:\x00") {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return true
		}
	}
	return false
}

// linkedParent is the first folder between dir (exclusive) and rel's file that is a link, or "".
func linkedParent(f ReadFS, dir, rel string) string {
	d := dir
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if seg == "." || seg == "" {
			continue
		}
		d += "/" + seg
		if fi, err := f.Lstat(d); err == nil && IsLink(fi) {
			return d
		}
	}
	return ""
}

// PackVerdict says whether an installed pack is current, from its record (nil when there is none), the commit the hub's mirror
// has now ("" when it could not be asked), and the agents something relies on. Status is ok or warn, never fail: a room
// without the pack works, it only cannot run the operator's panels and skills.
func PackVerdict(rec *PackRecord, have []string, latest string, need []string) (status, detail string) {
	var missing []string
	for _, n := range need {
		if !contains(have, n) {
			missing = append(missing, n)
		}
	}
	fix := "run `atrium room setup --apply` to install it"
	if rec == nil {
		m := ""
		if len(missing) > 0 {
			m = " the agents " + strings.Join(missing, ", ") + " that the review panel names are missing."
		}
		return StatusTodo, "the agent pack is not installed." + m + " " + fix
	}
	short := rec.Commit
	if len(short) > 9 {
		short = short[:9]
	}
	if len(missing) > 0 {
		return StatusWarn, fmt.Sprintf("the agents %s that the review panel names are missing, though the pack at %s is recorded. %s", strings.Join(missing, ", "), short, fix)
	}
	if latest != "" && latest != rec.Commit {
		l := latest
		if len(l) > 9 {
			l = l[:9]
		}
		return StatusWarn, fmt.Sprintf("the agent pack is stale: %s is installed and the hub's mirror has %s. %s", short, l, fix)
	}
	d := fmt.Sprintf("the agent pack at %s, %d agents and %d skills", short, len(rec.Agents), len(rec.Skills))
	if latest == "" {
		d += " (the hub mirror could not be asked, so whether it is current is not known)"
	}
	return StatusOK, d
}
