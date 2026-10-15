//go:build integration

package link

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A LAUNCH THAT NAMES NO ITEM IS REFUSED before anything is posted to the room, naming the argument.
func TestALaunchWithNoItemIsRefusedAndPostsNothing(t *testing.T) {
	h := newClassHarness(t, classTasks)
	s := h.connect(t, "boss", "alpha")
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_launch_worker",
		Arguments: map[string]any{"cwd": "/work/dir", "prompt": "do the thing"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("a launch with no item was not refused: %+v", res)
	}
	if len(*h.launches) != 0 {
		t.Fatalf("launches = %d, want none", len(*h.launches))
	}
}

// THE ATTACHMENT IS A TAG on the launch, and it is not the isolated tag.
func TestALaunchCarriesTheAttachmentTag(t *testing.T) {
	h := newClassHarness(t, classTasks)
	body := itemLaunchBody(t, h, map[string]any{"cwd": "/work/dir", "prompt": "x", "item": "f-thing"})
	if WorkOfTags(tagsOf(body)) != "f-thing" || hasTag(tagsOf(body), ItemTag) {
		t.Fatalf("tags = %v", tagsOf(body))
	}
}
