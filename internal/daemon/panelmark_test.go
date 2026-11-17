package daemon

import (
	"testing"
	"time"
)

func TestAStartedReviewPanelMarksTheCardUntilItsTurnEnds(t *testing.T) {
	t.Parallel()
	tr := newActivityTracker()
	tr.set("w", ActivityTool, "Skill")
	tr.notePanel("w", "review-panel")
	tr.set("w", ActivityThinking, "")
	if a := tr.get("w"); a == nil || !a.Panel {
		t.Fatalf("a walker that started the panel reads %+v", a)
	}
	tr.set("w", ActivityIdle, "")
	if a := tr.get("w"); a == nil || a.Panel {
		t.Fatalf("the turn ended and the mark held: %+v", a)
	}
	tr.notePanel("w", "simplify")
	if a := tr.get("w"); a.Panel {
		t.Fatal("another skill marked a panel")
	}
}

func TestAStalePanelMarkDoesNotHoldTheCard(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	tr := newActivityTracker()
	tr.now = func() time.Time { return at }
	tr.set("w", ActivityTool, "Skill")
	tr.notePanel("w", "dotfiles:review-panel")
	at = at.Add(staleAfter + time.Second)
	if a := tr.get("w"); a != nil && a.Panel {
		t.Fatal("a walker whose hooks went quiet still holds the panel mark")
	}
	tr.set("x", ActivityTool, "Skill")
	tr.notePanel("x", "review-panel")
	tr.forget("x")
	tr.notePanel("x", "review-panel")
	if a := tr.get("x"); a != nil && a.Panel {
		t.Fatal("a session that ended still holds the panel mark")
	}
}
