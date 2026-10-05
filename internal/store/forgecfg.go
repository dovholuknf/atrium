package store

import (
	"fmt"
	"regexp"
	"strings"
)

// What a room is told about the forge CLIs it needs: a host and a command NAME per CLI, and nothing else. Atrium never
// holds a credential. The CLI keeps its own login, and atrium only asks it for its status, on ask.
// See docs/rnd/scm-forge-design.md, the answers section.

// ForgeTool is one forge CLI atrium knows how to ask for its login status.
type ForgeTool struct {
	Key         string
	DefaultHost string
}

// ForgeTools is the fixed list. The key is also the default command name.
var ForgeTools = []ForgeTool{
	{"gh", "github.com"},
	{"bb", "bitbucket.org"},
	{"glab", "gitlab.com"},
}

var (
	forgeHostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)
	// A bare command name. A path would let a setting point at any binary on the machine.
	forgeCmdRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// KnownForge says whether key names a forge CLI.
func KnownForge(key string) bool {
	for _, t := range ForgeTools {
		if t.Key == key {
			return true
		}
	}
	return false
}

// ForgeDefaultHost is the host a tool means when none is named.
func ForgeDefaultHost(key string) string {
	for _, t := range ForgeTools {
		if t.Key == key {
			return t.DefaultHost
		}
	}
	return ""
}

func forgeKey(tool, field string) string { return "forge." + tool + "." + field }

// ForgeConfig is the host a room needs the tool logged in to and the command name it runs. Host is empty when the
// room does not need the tool. Cmd is never empty.
func (s *Store) ForgeConfig(tool string) (host, cmd string) {
	host, _ = s.Setting(forgeKey(tool, "host"))
	cmd, _ = s.Setting(forgeKey(tool, "cmd"))
	host, cmd = strings.TrimSpace(host), strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = tool
	}
	return host, cmd
}

// SetForgeConfig stores both, refusing a tool atrium does not know, a host that is not a host name, and a command
// that is not a bare name. Empty clears.
func (s *Store) SetForgeConfig(tool, host, cmd string) error {
	if !KnownForge(tool) {
		return fmt.Errorf("no forge CLI called %q. the ones there are: gh, bb, glab", tool)
	}
	host, cmd = strings.TrimSpace(host), strings.TrimSpace(cmd)
	if host != "" && !forgeHostRE.MatchString(host) {
		return fmt.Errorf("%q is not a host name. a name like github.com, with no scheme and no path", host)
	}
	if cmd != "" && !forgeCmdRE.MatchString(cmd) {
		return fmt.Errorf("%q is not a command name. atrium stores a name found on PATH, never a path or arguments", cmd)
	}
	if err := s.SetSetting(forgeKey(tool, "host"), host); err != nil {
		return err
	}
	return s.SetSetting(forgeKey(tool, "cmd"), cmd)
}

// ValidForgeHost says whether h is a host name fit to be an argument of a status command.
func ValidForgeHost(h string) bool { return forgeHostRE.MatchString(h) }
