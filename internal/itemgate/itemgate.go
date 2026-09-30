// Package itemgate is what a work item waiting on other work is, shared by the hub's store
// and the hub's link side so neither has to import the other. It holds the shapes and the
// pure rules: what an item id is, what a target is, what a landed changelog path is, and
// whether a new edge closes a loop. Nothing here touches a database or runs git.
//
// See docs/runtime/item-dependencies-design.md.
package itemgate

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// The two kinds of target a gate can wait on.
const (
	KindItem = "item"
	KindCond = "cond"
)

// Who met a gate. A gate is met by a check the hub ran for itself, or by a person on the
// board. There is no third value: no agent verb marks a gate met.
const (
	MetByAtrium = "atrium"
	MetByHuman  = "human"
)

// AddedByHuman is `added_by` for a gate added from the board, which tells nobody when it
// clears because the board already shows it.
const AddedByHuman = "human"

// Bounds on what a caller may write.
const (
	MaxWhy     = 400
	MaxTarget  = 200
	MaxTargets = 20
)

// Gate is one row: item Item waits on Target.
type Gate struct {
	ID      string     `json:"id"`
	Repo    string     `json:"repo"`
	Item    string     `json:"item"`
	Kind    string     `json:"kind"`
	Target  string     `json:"target"`
	Why     string     `json:"why,omitempty"`
	AddedBy string     `json:"added_by"`
	AddedAt time.Time  `json:"added_at"`
	MetAt   *time.Time `json:"met_at,omitempty"`
	MetBy   string     `json:"met_by,omitempty"`
	MetWhy  string     `json:"met_why,omitempty"`
	ToldAt  *time.Time `json:"told_at,omitempty"`
}

// Open is whether the gate still holds its item back.
func (g Gate) Open() bool { return g.MetAt == nil }

// Target is one thing an item waits on.
type Target struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

// itemID is a backlog item id: `r-037`, `u-033`, `91`, `r-new-item-dependencies`. No space,
// no colon and no slash, which is what tells an item from a condition.
var itemID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ValidItem is whether s can name an item.
func ValidItem(s string) bool { return itemID.MatchString(s) && !strings.Contains(s, "..") }

// ParseTarget reads one `waits_on` entry. Something shaped like an item id is an item, and
// anything else is a condition: `live:u-033`, `room:sg3`, `sha:4815d47`, or free text.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{}, errors.New("an empty entry waits on nothing")
	}
	if ValidItem(s) {
		return Target{Kind: KindItem, Target: s}, nil
	}
	if len(s) > MaxTarget {
		return Target{}, fmt.Errorf("a condition is at most %d bytes", MaxTarget)
	}
	if kind, arg := Cond(s); kind != "" && arg == "" {
		return Target{}, fmt.Errorf("%q names no %s", s, kind)
	}
	return Target{Kind: KindCond, Target: s}, nil
}

// Cond splits a condition the hub can check into its kind and argument. A condition with
// no check (free text) answers an empty kind, and a person clears it.
func Cond(s string) (kind, arg string) {
	for _, k := range []string{"live", "room", "sha"} {
		if rest, ok := strings.CutPrefix(s, k+":"); ok {
			return k, strings.TrimSpace(rest)
		}
	}
	return "", ""
}

// shaArg is what a `sha:` condition may name: a hex commit, abbreviated or whole.
var shaArg = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

// ValidSHA is whether a `sha:` argument is a commit id, so nothing else reaches git.
func ValidSHA(s string) bool { return shaArg.MatchString(s) }

// ItemOfTitle is the item a launch is for: a worker's title starts `<id>:`. A title with no
// colon counts only when the whole of it is an id. Empty when neither.
func ItemOfTitle(title string) string {
	title = strings.TrimSpace(title)
	if i := strings.Index(title, ":"); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	if ValidItem(title) {
		return title
	}
	return ""
}

// landedName is the per-item changelog file's name: `YYYY-MM-DD-<id>.md`.
var landedName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-(.+)\.md$`)

// LandedID reads a path under changelog/ as the item it records landing. Only
// `changelog/<dept>/<date>-<id>.md` counts, and the id is the whole rest of the name, so
// `r-007-3` never satisfies `r-007`.
func LandedID(p string) (string, bool) {
	parts := strings.Split(p, "/")
	if len(parts) != 3 || parts[0] != "changelog" || parts[1] == "" {
		return "", false
	}
	m := landedName.FindStringSubmatch(path.Base(p))
	if m == nil || !ValidItem(m[1]) {
		return "", false
	}
	return m[1], true
}

// FindPath is the chain of item edges from `from` that reaches `to`, both ends included,
// or nil. `edges` maps an item to the items it waits on.
func FindPath(edges map[string][]string, from, to string) []string {
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if at == to {
			var out []string
			for n := at; n != ""; n = prev[n] {
				out = append([]string{n}, out...)
			}
			return out
		}
		for _, next := range edges[at] {
			if _, seen := prev[next]; !seen {
				prev[next] = at
				queue = append(queue, next)
			}
		}
	}
	return nil
}

// LoopError names a loop the way a person reads it: `r-040 waits on r-041, which waits on
// r-040`. `loop` starts and ends with the same item.
func LoopError(loop []string) error {
	var b strings.Builder
	for i, n := range loop {
		switch i {
		case 0:
			b.WriteString(n)
		case 1:
			b.WriteString(" waits on " + n)
		default:
			b.WriteString(", which waits on " + n)
		}
	}
	return errors.New(b.String() + ". a gate that closes a loop can never clear, so it is refused")
}

// Bound cuts s to at most n bytes on a rune boundary.
func Bound(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
