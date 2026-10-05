package store

import (
	"regexp"
)

// The forge CLIs atrium knows. A room keeps no forge configuration: a room with a hub never runs one, and a room with
// no hub runs the CLI by its own name against its default host. Atrium never holds a credential.
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

// ValidForgeHost says whether h is a host name fit to be an argument of a status command.
func ValidForgeHost(h string) bool { return forgeHostRE.MatchString(h) }
