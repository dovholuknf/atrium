package link

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type fakeStatus struct {
	code int
	bare bool
}

func (e fakeStatus) Error() string   { return "hub answered" }
func (e fakeStatus) HTTPStatus() int { return e.code }
func (e fakeStatus) Bare() bool      { return e.bare }

func TestALaunchWithNoItemIsRefusedNamingTheFlag(t *testing.T) {
	_, _, err := ResolveItem(context.Background(), nil, "  ", "fabric", "me", "r")
	if err == nil || !strings.Contains(err.Error(), "--item") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnItemThatIsNotAnIdIsRefused(t *testing.T) {
	_, _, err := ResolveItem(context.Background(), nil, "Do The Thing", "fabric", "me", "r")
	if err == nil || !strings.Contains(err.Error(), "not a backlog id") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnIdTheHubHoldsIsAccepted(t *testing.T) {
	call := func(_ context.Context, method, path string, _, out any) error { return nil }
	id, filed, err := ResolveItem(context.Background(), call, "f-thing", "", "me", "r")
	if err != nil || id != "f-thing" || filed {
		t.Fatalf("id=%q filed=%v err=%v", id, filed, err)
	}
}

func TestAnIdTheHubDoesNotHoldIsRefusedButAMissingRouteIsNot(t *testing.T) {
	missing := func(context.Context, string, string, any, any) error { return fakeStatus{code: http.StatusNotFound} }
	if _, _, err := ResolveItem(context.Background(), missing, "f-nope", "", "me", "r"); err == nil ||
		!strings.Contains(err.Error(), "no backlog item") {
		t.Fatalf("err = %v", err)
	}
	noRoute := func(context.Context, string, string, any, any) error {
		return fakeStatus{code: http.StatusNotFound, bare: true}
	}
	if id, _, err := ResolveItem(context.Background(), noRoute, "f-nope", "", "me", "r"); err != nil || id != "f-nope" {
		t.Fatalf("a route that is not there refused the launch: %q %v", id, err)
	}
	down := func(context.Context, string, string, any, any) error { return http.ErrHandlerTimeout }
	if id, _, err := ResolveItem(context.Background(), down, "f-nope", "", "me", "r"); err != nil || id != "f-nope" {
		t.Fatalf("a hub that is down refused the launch: %q %v", id, err)
	}
}

func TestNewFilesTheBacklogItemUnderTheLaunchersDept(t *testing.T) {
	var posted map[string]any
	call := func(_ context.Context, method, path string, body, out any) error {
		if method != http.MethodPost || path != "/_hub/backlog" {
			t.Fatalf("call %s %s", method, path)
		}
		posted = body.(map[string]any)
		*(out.(*hubstore.BacklogItem)) = hubstore.BacklogItem{ID: "f-filed"}
		return nil
	}
	id, filed, err := ResolveItem(context.Background(), call, `new:a title`, "fabric", "me@r", "r")
	if err != nil || id != "f-filed" || !filed {
		t.Fatalf("id=%q filed=%v err=%v", id, filed, err)
	}
	if posted["dept"] != "fabric" || posted["title"] != "a title" {
		t.Fatalf("posted %v", posted)
	}
	if _, _, err := ResolveItem(context.Background(), call, "new:x", "", "me", "r"); err == nil {
		t.Fatal("new: with no department was not refused")
	}
}

func TestTheWorkTagRoundTrips(t *testing.T) {
	tags := WithWorkTag([]string{"a"}, "f-thing")
	if WorkOfTags(tags) != "f-thing" || len(WithWorkTag(tags, "f-other")) != len(tags) {
		t.Fatalf("tags = %v", tags)
	}
	if WorkOfTags([]string{ItemTag}) != "" {
		t.Fatal("the isolated tag is not an attachment")
	}
}
