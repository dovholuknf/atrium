// Package cardcolors is the card colour table both the room's settings and the hub's own use:
// the stored shape, the seed, and the validation. The board's colours belong to the board, so the
// hub keeps them; a room keeps its own for a room-scoped view.
package cardcolors

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Setting names the key that holds the card colours as one JSON value: a default theme name and a map of
// `provider/org/repo` to theme name. A row that does not exist has never been set and is seeded on
// first read. A row holding `{"default":"","repos":{}}` was set, to nothing, and stays that way.
const Setting = "card_colors"

const (
	maxCardColorRepos = 500
	maxCardColorKey   = 200
	maxCardColorName  = 100
)

// Colors is the stored shape, and what the board reads and writes.
type Colors struct {
	Default string            `json:"default"`
	Repos   map[string]string `json:"repos"`
}

// seeded is the operator's own table, as full keys. Written once, the first time the
// setting is read and has never been set, so a user who deletes an entry does not get it back.
var seeded = map[string]string{
	"github/openziti-test-kitchen/appetizer": "active-work",
	"github/dovholuknf/atrium":               "active-light",
	"github/openziti/desktop-edge-win":       "dracula",
	"github/netfoundry/docusaurus-shared":    "matrix",
	"github/dovholuknf/dotfiles":             "tangent",
	"github/openziti/sdk-golang":             "terracotta",
	"github/netfoundry/sterling":             "orange-coral",
	"github/openziti/ziti":                   "teal-dusk",
	"github/openziti/ziti-console":           "deep-amethyst",
	"github/openziti/ziti-doc":               "imperial-purple",
	"github/openziti/ziti-openwrt":           "ocean-deep",
	"github/openziti/ziti-sdk-c":             "neon-grape",
	"github/openziti/ziti-sdk-csharp":        "nord",
	"github/openziti/ziti-sdk-py":            "deep-ocean",
	"github/openziti/ziti-tunnel-sdk-c":      "gruvbox-dark",
	"github/openziti-test-kitchen/ziti-tv":   "mauve-purple",
	"github/openziti/zrok":                   "electric-purple",
}

var keyRe = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`)

// Valid refuses a key that is not lowercase `provider/org/repo` and sizes past the caps.
// A theme name is not checked against a list: brought themes are loaded at runtime.
func Valid(c Colors) error {
	if len(c.Default) > maxCardColorName {
		return fmt.Errorf("the default colour name is longer than %d", maxCardColorName)
	}
	if len(c.Repos) > maxCardColorRepos {
		return fmt.Errorf("at most %d repos may have a colour", maxCardColorRepos)
	}
	for k, v := range c.Repos {
		if len(k) > maxCardColorKey || !keyRe.MatchString(k) {
			return fmt.Errorf("%q is not a lowercase provider/org/repo", k)
		}
		if len(v) == 0 || len(v) > maxCardColorName {
			return fmt.Errorf("the colour for %s must be a name of 1 to %d characters", k, maxCardColorName)
		}
	}
	return nil
}

// Seed is a fresh copy of the seed table.
func Seed() Colors {
	c := Colors{Repos: map[string]string{}}
	for k, v := range seeded {
		c.Repos[k] = v
	}
	return c
}

// Load decodes a stored value. A value that was never set (blank) answers the seed and true, so the
// caller writes it back once. A stored empty table stays empty.
func Load(raw string) (Colors, bool) {
	if strings.TrimSpace(raw) == "" {
		return Seed(), true
	}
	var c Colors
	_ = json.Unmarshal([]byte(raw), &c)
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	return c, false
}

// Encode trims, validates and encodes a table for storage.
func Encode(c Colors) (string, error) {
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	c.Default = strings.TrimSpace(c.Default)
	if err := Valid(c); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	return string(b), err
}
