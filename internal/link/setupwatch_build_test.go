package link

import (
	"context"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestARoomOnAnotherBuildRaisesNoSetupIssue(t *testing.T) {
	p := &Proxy{boardID: "bbbbbbbbbbbb"}
	for _, f := range []*wire.RoomSetup{{Build: "aaaaaaaaaaaa"}, {Build: "aaaaaaaaaaaa", Code: "beef", Commit: "1111111111111111111111111111111111111111"}} {
		f.Answering = true
		if got := p.setupIssues(context.Background(), f); len(got) != 0 {
			t.Fatalf("a room on another build raised %+v", got)
		}
	}
}
