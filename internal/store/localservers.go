package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SettingLocalServers is the room setting `local_servers`: a JSON list of local servers the room starts and keeps
// up, such as a model server a local runner talks to, so nothing needs a startup task of its own.
const SettingLocalServers = "local_servers"

// LocalServer is one server the room keeps up. Health is a URL that answers while it runs.
type LocalServer struct {
	Name   string   `json:"name"`
	Cmd    string   `json:"cmd"`
	Args   []string `json:"args"`
	Health string   `json:"health"`
}

// localServerName is a name that is also its log file's name.
var localServerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ParseLocalServers reads the setting's value. Empty is none.
func ParseLocalServers(raw string) ([]LocalServer, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []LocalServer
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("local_servers is not a JSON list of {name, cmd, args, health}: %w", err)
	}
	seen := map[string]bool{}
	for _, s := range out {
		switch {
		case !localServerName.MatchString(s.Name):
			return nil, fmt.Errorf("local server name %q is not letters, digits, dot, dash or underscore", s.Name)
		case seen[s.Name]:
			return nil, fmt.Errorf("local server %q is named twice", s.Name)
		case strings.TrimSpace(s.Cmd) == "":
			return nil, fmt.Errorf("local server %q has no cmd", s.Name)
		case !strings.HasPrefix(s.Health, "http://") && !strings.HasPrefix(s.Health, "https://"):
			return nil, fmt.Errorf("local server %q needs a health URL, http:// or https://", s.Name)
		}
		seen[s.Name] = true
	}
	return out, nil
}

// LocalServers is the room's list. An unreadable or malformed value is none, and the caller logs why.
func (s *Store) LocalServers() ([]LocalServer, error) {
	v, err := s.Setting(SettingLocalServers)
	if err != nil {
		return nil, err
	}
	return ParseLocalServers(v)
}
