package daemon

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestChatStartsWithNoToolsAndGrowsByGrant(t *testing.T) {
	if got := superleanTools(leanKit{Superlean: true, Chat: true}); len(got) != 0 {
		t.Fatalf("chat floor: %v", got)
	}
	got := superleanTools(leanKit{Superlean: true, Chat: true, Tools: []string{"WebFetch", "Read", "Bogus"}})
	if want := []string{"Read", "WebFetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestChatLaunchIsSuperleanWithAPromptThatSaysSo(t *testing.T) {
	lean, mcp, kit, superlean := leanOptions(shared.LaunchRequest{Chat: true}, nil, "")
	if !lean || !superlean || !kit.Chat || len(mcp) != 0 {
		t.Fatalf("lean %v superlean %v kit %+v mcp %v", lean, superlean, kit, mcp)
	}
	if tags := leanTags(mcp, kit); !reflect.DeepEqual(tags, []string{LeanTag, SuperleanTag, ChatTag}) {
		t.Fatalf("tags %v", tags)
	}
	args, servers := chatArgs(t, kit, chatStdioMCP)
	if !slices.Contains(args, "--tools=") {
		t.Fatalf("want an empty --tools=: %q", args)
	}
	if chatFlag(args, "--system-prompt") != chatSystemPrompt || slices.Contains(args, "--append-system-prompt") {
		t.Fatalf("prompt flags %q", args)
	}
	if !strings.Contains(chatSystemPrompt, "only atrium tool you have is atrium_need") {
		t.Fatal(chatSystemPrompt)
	}
	if len(servers) != 1 || !reflect.DeepEqual(servers["atrium-control"]["args"], []any{"control", "--chat"}) {
		t.Fatalf("servers %v", servers)
	}
	if args[len(args)-1] != "hello" {
		t.Fatalf("prompt not last: %q", args)
	}
}

const chatStdioMCP = `{"mcpServers":{"atrium-control":{"type":"stdio","command":"C:/a/atrium.exe","args":["control"]},` +
	`"mercurius":{"type":"http","url":"http://127.0.0.1:8088/mcp"},"other":{"type":"stdio","command":"x"}}}`

func chatArgs(t *testing.T, kit leanKit, config string) ([]string, map[string]map[string]any) {
	t.Helper()
	read := func(string) ([]byte, error) { return []byte(config), nil }
	args, err := leanArgs([]string{"--mcp-config", "C:/t/mcp.json", "--model", "m", "hello"}, nil, "", nil, kit, "", read, testHooks())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(chatFlag(args, "--mcp-config")), &doc); err != nil {
		t.Fatal(err)
	}
	return args, doc.MCPServers
}

func chatFlag(args []string, flag string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, flag); ok && strings.HasSuffix(flag, "=") {
			return v
		}
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestChatAfterAGrantKeepsItsShapePlusTheGrant(t *testing.T) {
	task := &store.Task{Tags: []string{LeanTag, SuperleanTag, ChatTag, "atrium:tool:Read"}}
	_, _, kit, _ := leanOptions(shared.LaunchRequest{}, task, "")
	args, servers := chatArgs(t, kit, chatStdioMCP)
	if chatFlag(args, "--tools=") != "Read" || chatFlag(args, "--system-prompt") != chatSystemPrompt {
		t.Fatalf("args %q", args)
	}
	if len(servers) != 1 || !reflect.DeepEqual(servers["atrium-control"]["args"], []any{"control", "--chat"}) {
		t.Fatalf("servers %v", servers)
	}
}

func TestChatLeavesAHubRelayToTheHubsChatClass(t *testing.T) {
	relay := `{"mcpServers":{"atrium-control":{"type":"stdio","command":"atrium","args":["mcp-relay"]}}}`
	_, servers := chatArgs(t, leanKit{Superlean: true, Chat: true}, relay)
	if !reflect.DeepEqual(servers["atrium-control"]["args"], []any{"mcp-relay"}) {
		t.Fatalf("servers %v", servers)
	}
}

func TestSuperleanIsUntouchedByChat(t *testing.T) {
	args, servers := chatArgs(t, leanKit{Superlean: true}, chatStdioMCP)
	if chatFlag(args, "--append-system-prompt") != superleanSystemPrompt || slices.Contains(args, "--system-prompt") {
		t.Fatalf("args %q", args)
	}
	if !reflect.DeepEqual(servers["atrium-control"]["args"], []any{"control"}) {
		t.Fatalf("servers %v", servers)
	}
}

func TestChatCardKeepsItsGrantsOnReopen(t *testing.T) {
	task := &store.Task{Tags: []string{LeanTag, SuperleanTag, ChatTag, "atrium:tool:Read"}}
	_, _, kit, superlean := leanOptions(shared.LaunchRequest{}, task, "")
	if !superlean || !kit.Chat || !reflect.DeepEqual(superleanTools(kit), []string{"Read"}) {
		t.Fatalf("kit %+v superlean %v", kit, superlean)
	}
}

func TestNeedGrantsBuiltInToolsAndRefusesTheRest(t *testing.T) {
	tags, refused := needTags([]string{"tool:Read", "tool:WebFetch", "tool:Bogus"})
	if !reflect.DeepEqual(tags, []string{"atrium:tool:Read", "atrium:tool:WebFetch"}) || !reflect.DeepEqual(refused, []string{"tool:Bogus"}) {
		t.Fatalf("tags %v refused %v", tags, refused)
	}
}

func TestChatTagIsStrippedWithTheLeanTags(t *testing.T) {
	got, cut := withoutLeanTags([]string{"keep", LeanTag, SuperleanTag, ChatTag, "atrium:tool:Read"})
	if !cut || !reflect.DeepEqual(got, []string{"keep"}) {
		t.Fatalf("got %v cut %v", got, cut)
	}
}
