package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SettingCardColors holds the card colours as one JSON value: a default theme name and a map of
// `provider/org/repo` to theme name. A row that does not exist has never been set and is seeded on
// first read. A row holding `{"default":"","repos":{}}` was set, to nothing, and stays that way.
const SettingCardColors = "card_colors"

const (
	maxCardColorRepos = 500
	maxCardColorKey   = 200
	maxCardColorName  = 100
)

// CardColors is the stored shape, and what the board reads and writes.
type CardColors struct {
	Default string            `json:"default"`
	Repos   map[string]string `json:"repos"`
}

// seededCardColors is the operator's own table, as full keys. Written once, the first time the
// setting is read and has never been set, so a user who deletes an entry does not get it back.
var seededCardColors = map[string]string{
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

var cardColorKey = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+$`)

// validCardColors refuses a key that is not lowercase `provider/org/repo` and sizes past the caps.
// A theme name is not checked against a list: brought themes are loaded at runtime.
func validCardColors(c CardColors) error {
	if len(c.Default) > maxCardColorName {
		return fmt.Errorf("the default colour name is longer than %d", maxCardColorName)
	}
	if len(c.Repos) > maxCardColorRepos {
		return fmt.Errorf("at most %d repos may have a colour", maxCardColorRepos)
	}
	for k, v := range c.Repos {
		if len(k) > maxCardColorKey || !cardColorKey.MatchString(k) {
			return fmt.Errorf("%q is not a lowercase provider/org/repo", k)
		}
		if len(v) == 0 || len(v) > maxCardColorName {
			return fmt.Errorf("the colour for %s must be a name of 1 to %d characters", k, maxCardColorName)
		}
	}
	return nil
}

// CardColors reads the setting, seeding it when the row does not exist.
func (s *Server) CardColors() CardColors {
	raw, _ := s.st.Setting(SettingCardColors)
	if strings.TrimSpace(raw) == "" {
		seed := CardColors{Repos: map[string]string{}}
		for k, v := range seededCardColors {
			seed.Repos[k] = v
		}
		if b, err := json.Marshal(seed); err == nil {
			_ = s.st.SetSetting(SettingCardColors, string(b))
		}
		return seed
	}
	var c CardColors
	_ = json.Unmarshal([]byte(raw), &c)
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	return c
}

func (s *Server) setCardColors(c CardColors) error {
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	c.Default = strings.TrimSpace(c.Default)
	if err := validCardColors(c); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.st.SetSetting(SettingCardColors, string(b))
}
