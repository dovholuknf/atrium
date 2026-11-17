// Package capability is a room's inventory of what it can do: a declared list of named claims, a probe for each, and
// the newest result of every probe. It imports nothing from the daemon, the hub or the store.
//
// A probe is an argv list and never a shell string. The only commands that run are the ones in the declared list, and
// an asker names ids from it. Output is cut and scrubbed before it is kept, and the raw output never leaves this
// package.
package capability

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

//go:embed default.yaml
var defaultYAML []byte

const (
	Pass    = "pass"
	Fail    = "fail"
	Unknown = "unknown"

	DefaultTimeout = 10 * time.Second
	MaxTimeout     = 120 * time.Second
	ReasonMax      = 200
)

// Cap is one declared capability.
type Cap struct {
	ID    string   `yaml:"id" json:"id"`
	Kind  string   `yaml:"kind" json:"kind"`
	Probe []string `yaml:"probe" json:"-"`
	// Dir is where the probe runs. ${NAME} is read from the environment, and ${SCRATCH} is a directory the room made.
	Dir     string   `yaml:"dir,omitempty" json:"-"`
	Timeout string   `yaml:"timeout,omitempty" json:"-"`
	Pass    string   `yaml:"pass,omitempty" json:"-"`
	Match   string   `yaml:"match,omitempty" json:"-"`
	Version string   `yaml:"version,omitempty" json:"-"`
	Needs   []string `yaml:"needs,omitempty" json:"needs,omitempty"`
	// Optional shows a failure neutral everywhere. NotExpected does the same on the rooms it names.
	Optional    bool     `yaml:"optional,omitempty" json:"optional,omitempty"`
	NotExpected []string `yaml:"not_expected,omitempty" json:"not_expected,omitempty"`
	Every       string   `yaml:"every,omitempty" json:"every,omitempty"`
}

// List is the declared list.
type List struct {
	Version int   `yaml:"version" json:"version"`
	Caps    []Cap `yaml:"caps" json:"caps"`
}

var kinds = map[string]bool{"reach": true, "toolchain": true, "build": true, "access": true}

// Parse reads a list and checks it: known kinds and pass rules, no duplicate id, every need resolves.
func Parse(b []byte) (List, error) {
	l, err := parse(b)
	if err != nil {
		return l, err
	}
	return l, resolves(l)
}

func parse(b []byte) (List, error) {
	var l List
	if err := yaml.Unmarshal(b, &l); err != nil {
		return l, err
	}
	seen := map[string]bool{}
	for _, c := range l.Caps {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return l, fmt.Errorf("a capability has no id")
		case seen[c.ID]:
			return l, fmt.Errorf("%s is declared twice", c.ID)
		case !kinds[c.Kind]:
			return l, fmt.Errorf("%s has kind %q, want reach, toolchain, build or access", c.ID, c.Kind)
		case len(c.Probe) == 0:
			return l, fmt.Errorf("%s has no probe", c.ID)
		}
		switch c.Pass {
		case "", "exit0", "http2xx":
		default:
			return l, fmt.Errorf("%s has pass rule %q, want exit0 or http2xx", c.ID, c.Pass)
		}
		for _, r := range []string{c.Match, c.Version} {
			if _, err := regexp.Compile(r); err != nil {
				return l, fmt.Errorf("%s: %w", c.ID, err)
			}
		}
		if _, err := c.timeout(); err != nil {
			return l, fmt.Errorf("%s: %w", c.ID, err)
		}
		seen[c.ID] = true
	}
	return l, nil
}

func resolves(l List) error {
	seen := map[string]bool{}
	for _, c := range l.Caps {
		seen[c.ID] = true
	}
	for _, c := range l.Caps {
		for _, n := range c.Needs {
			if !seen[n] {
				return fmt.Errorf("%s needs %s, which is not declared", c.ID, n)
			}
		}
	}
	return nil
}

func (c Cap) timeout() (time.Duration, error) {
	if c.Timeout == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil || d <= 0 || d > MaxTimeout {
		return 0, fmt.Errorf("timeout %q is not a duration up to %s", c.Timeout, MaxTimeout)
	}
	return d, nil
}

// Default is the list atrium ships.
func Default() List {
	l, err := Parse(defaultYAML)
	if err != nil {
		panic("capability: the shipped list is invalid: " + err.Error())
	}
	return l
}

// Load is the shipped list with the room's own additions from path laid over it. A local entry adds or replaces by id.
// A missing file is no additions, and a file that does not parse is an error so the room says so instead of dropping it.
func Load(path string) (List, error) {
	l := Default()
	if path == "" {
		return l, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	local, err := Parse(b)
	if err != nil {
		return l, fmt.Errorf("%s: %w", path, err)
	}
	at := map[string]int{}
	for i, c := range l.Caps {
		at[c.ID] = i
	}
	for _, c := range local.Caps {
		if i, ok := at[c.ID]; ok {
			l.Caps[i] = c
		} else {
			l.Caps = append(l.Caps, c)
		}
	}
	if local.Version > l.Version {
		l.Version = local.Version
	}
	if _, err := Parse(mustMarshal(l)); err != nil {
		return Default(), err
	}
	return l, nil
}

// Lint checks a hub list laid over the shipped one and answers the list as written.
func Lint(b []byte) (List, error) {
	h, err := parse(b)
	if err != nil {
		return h, err
	}
	_, err = WithHub(Default(), b)
	return h, err
}

// WithHub lays the hub's list over l by id, so a room's own addition cannot loosen a hub entry.
func WithHub(l List, hub []byte) (List, error) {
	h, err := parse(hub)
	if err != nil {
		return l, fmt.Errorf("the hub's list: %w", err)
	}
	out := List{Version: l.Version, Caps: append([]Cap(nil), l.Caps...)}
	at := map[string]int{}
	for i, c := range out.Caps {
		at[c.ID] = i
	}
	for _, c := range h.Caps {
		if i, ok := at[c.ID]; ok {
			out.Caps[i] = c
		} else {
			at[c.ID] = len(out.Caps)
			out.Caps = append(out.Caps, c)
		}
	}
	if h.Version > out.Version {
		out.Version = h.Version
	}
	if err := resolves(out); err != nil {
		return l, fmt.Errorf("the hub's list: %w", err)
	}
	return out, nil
}

func mustMarshal(l List) []byte {
	b, _ := yaml.Marshal(l)
	return b
}

// Result is the outcome of one probe.
type Result struct {
	Cap     string `json:"cap"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
	At      string `json:"at"`
	MS      int64  `json:"ms,omitempty"`
}

var secretShape = regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}|-----BEGIN[^\n]*|bearer\s+\S+|[A-Za-z0-9+/=_-]{40,})`)

// Scrub cuts text to ReasonMax characters on one line, with anything shaped like a credential taken out first.
func Scrub(s string) string {
	s = secretShape.ReplaceAllString(s, "<cut>")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > ReasonMax {
		s = string(r[:ReasonMax])
	}
	return s
}
