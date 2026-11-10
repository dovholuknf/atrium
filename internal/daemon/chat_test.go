package daemon

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
	args, err := leanArgs([]string{"hello"}, nil, "", nil, kit, "", nil, testHooks())
	if err != nil {
		t.Fatal(err)
	}
	var tools, prompt string
	for i, a := range args {
		if strings.HasPrefix(a, "--tools=") {
			tools = a
		}
		if a == "--append-system-prompt" {
			prompt = args[i+1]
		}
	}
	if tools != "--tools=" || prompt != chatSystemPrompt || !strings.Contains(prompt, "only atrium tool you have is atrium_need") {
		t.Fatalf("tools %q prompt %q", tools, prompt)
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

func needDaemon(restart func(string) (*store.Task, error)) *Daemon {
	d := &Daemon{act: newActivityTracker()}
	d.hk.needQuiet, d.hk.needQuietTick, d.hk.needRestart = 60*time.Millisecond, 5*time.Millisecond, restart
	return d
}

func TestNeedRestartWaitsForTheCardToGoQuiet(t *testing.T) {
	var at time.Time
	d := needDaemon(func(string) (*store.Task, error) { at = time.Now(); return nil, nil })
	d.act.set("c1", ActivityThinking, "")
	last := time.Now()
	d.restartForNeed("c1")
	if at.Sub(last) < d.hk.needQuiet {
		t.Fatalf("restarted %v after the last activity, want at least %v", at.Sub(last), d.hk.needQuiet)
	}
}

func TestNeedRestartRetriesAFailedRestartAndIsBounded(t *testing.T) {
	calls := 0
	d := needDaemon(func(string) (*store.Task, error) { calls++; return nil, errors.New("still mid-turn") })
	d.restartForNeed("c1")
	if calls != needRestartTries {
		t.Fatalf("%d tries, want %d", calls, needRestartTries)
	}
	calls = 0
	d = needDaemon(func(string) (*store.Task, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("still mid-turn")
		}
		return nil, nil
	})
	d.restartForNeed("c1")
	if calls != 3 {
		t.Fatalf("%d tries, want 3", calls)
	}
}
