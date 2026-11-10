package daemon

import (
	"encoding/json"
	"slices"

	"github.com/dovholuknf/atrium/internal/small"
)

// ChatTag marks a card launched with `chat: true`: a superlean session with no tools of its own and only atrium_need
// from atrium-control, which grows by atrium_need as `atrium:tool:<name>` tags.
const ChatTag = "atrium:chat"

// chatGrantable are the built-ins atrium_need can add to a chat card, in the order the tool list holds them.
var chatGrantable = []string{"Read", "Edit", "Write", "Grep", "Glob", "Bash", "PowerShell", "WebFetch"}

// chatSystemPrompt is the whole of a chat card's appended prompt.
const chatSystemPrompt = `You are a thinking friend. You have no tools: no files, no shell, no web, and the only atrium tool you have is atrium_need. Talk the problem through. When you need a tool, say "I can get that tool if you want" and name it. On a yes, call atrium_need with needs ["tool:<Name>"], Name being one of Read, Edit, Write, Grep, Glob, Bash, PowerShell or WebFetch, and a one line why, then end your turn: atrium restarts you on this conversation with the tool and types continue.`

// isChatTool says whether name is a built-in atrium_need can add to a chat card.
func isChatTool(name string) bool { return small.HasTag(chatGrantable, name) }

// chatTools is the `--tools` list of a chat card: the granted built-ins and nothing else, so none at the start.
func chatTools(k leanKit) []string {
	out := []string{}
	for _, t := range chatGrantable {
		if small.HasTag(k.Tools, t) {
			out = append(out, t)
		}
	}
	return out
}

// chatServers is servers with a stdio `atrium control` given `--chat`, so it serves atrium_need alone. A relay to the
// hub is left as it is: the hub shows a card tagged ChatTag atrium_need alone.
func chatServers(servers string) string {
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal([]byte(servers), &doc) != nil {
		return servers
	}
	c, ok := doc.MCPServers["atrium-control"]
	if !ok {
		return servers
	}
	args, _ := c["args"].([]any)
	if len(args) == 0 || args[0] != "control" || slices.Contains(args, any("--chat")) {
		return servers
	}
	c["args"] = append(args, "--chat")
	b, err := json.Marshal(doc)
	if err != nil {
		return servers
	}
	return string(b)
}
