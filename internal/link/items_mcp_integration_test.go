//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type itemCall struct{ method, path, body string }

func fakeItemTools(calls *[]itemCall, answer string) ownerItemTools {
	return ownerItemTools{
		call: func(_ context.Context, method, path string, body, out any) error {
			b, _ := json.Marshal(body)
			*calls = append(*calls, itemCall{method, path, string(b)})
			if out != nil && answer != "" {
				return json.Unmarshal([]byte(answer), out)
			}
			return nil
		},
		by: func(*mcp.CallToolRequest) string { return "orch@sg4" },
	}
}

func TestItemToolActionsAskTheHub(t *testing.T) {
	var calls []itemCall
	it := fakeItemTools(&calls, `{"items":[{"item_id":"it-1","state":"blocked"}],"item_id":"it-1","state":"blocked"}`)
	ctx := context.Background()

	_, out, err := it.itemHandler(ctx, nil, itemInput{Action: "list"})
	if err != nil || len(out.Items) != 1 {
		t.Fatalf("list: %+v %v", out, err)
	}
	if c := calls[0]; c.method != http.MethodGet || !strings.Contains(c.path, "owner=orch%40sg4") {
		t.Fatalf("a list is what the caller owns: %+v", c)
	}
	it.itemHandler(ctx, nil, itemInput{Action: "list", All: true, State: "blocked"})
	if c := calls[1]; strings.Contains(c.path, "owner=") || !strings.Contains(c.path, "state=blocked") {
		t.Fatalf("all: %+v", c)
	}
	if _, out, err = it.itemHandler(ctx, nil, itemInput{Action: "get", ID: "it-1"}); err != nil || out.Item == nil ||
		calls[2].path != "/_hub/items/it-1" {
		t.Fatalf("get: %+v %v %+v", out, err, calls[2])
	}
	if _, _, err = it.itemHandler(ctx, nil, itemInput{Action: "unblock", ID: "it-1", Answer: "use the key"}); err != nil {
		t.Fatal(err)
	}
	if c := calls[3]; c.method != http.MethodPost || c.path != "/_hub/items/it-1" ||
		!strings.Contains(c.body, `"do":"unblock"`) || !strings.Contains(c.body, `"by":"orch@sg4"`) ||
		!strings.Contains(c.body, "use the key") {
		t.Fatalf("unblock: %+v", c)
	}
	it.itemHandler(ctx, nil, itemInput{Action: "cancel", ID: "it-1"})
	if c := calls[4]; !strings.Contains(c.body, `"do":"cancel"`) {
		t.Fatalf("cancel: %+v", c)
	}
}

func TestItemToolRefusesWhatIsMissing(t *testing.T) {
	var calls []itemCall
	it := fakeItemTools(&calls, "")
	for _, in := range []itemInput{
		{Action: "get"}, {Action: "unblock", ID: "it-1"}, {Action: "unblock", Answer: "x"}, {Action: "cancel"},
		{Action: "dance", ID: "it-1"},
	} {
		if _, _, err := it.itemHandler(context.Background(), nil, in); err == nil {
			t.Errorf("%+v was taken", in)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("a refused call reached the hub: %+v", calls)
	}
}

// THE TOOL IS FOR OWNERS, who are launchers and the like, and never for an item worker.
func TestItemToolIsNotInTheWorkerSets(t *testing.T) {
	if workerTools["atrium_item"] {
		t.Fatal("atrium_item is in the worker set")
	}
}

func TestItemToolSubmitQueuesOnAPool(t *testing.T) {
	var calls []itemCall
	it := fakeItemTools(&calls, `{"ok":true,"item_id":"it-9","state":"running"}`)
	_, out, err := it.itemHandler(context.Background(), nil, itemInput{Action: "submit", Pool: "reviewer", Brief: "review it"})
	if err != nil || !strings.Contains(out.Note, "it-9") {
		t.Fatalf("submit: %+v %v", out, err)
	}
	if c := calls[0]; c.method != http.MethodPost || c.path != "/_hub/items" || !strings.Contains(c.body, `"do":"submit"`) ||
		!strings.Contains(c.body, `"pool":"reviewer"`) || !strings.Contains(c.body, `"by":"orch@sg4"`) {
		t.Fatalf("submit: %+v", c)
	}
	if _, _, err := it.itemHandler(context.Background(), nil, itemInput{Action: "submit", Pool: "reviewer"}); err == nil {
		t.Fatal("a submit with no brief was accepted")
	}
}
