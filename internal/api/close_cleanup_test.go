package api

import (
	"reflect"
	"sync"
	"testing"
)

func TestWithoutCleanupTags(t *testing.T) {
	got := withoutCleanupTags([]string{"link:x", "cleanup:when-done", "cleanup:offered", "pr"})
	if !reflect.DeepEqual(got, []string{"link:x", "pr"}) {
		t.Fatalf("got %v", got)
	}
}

// Two boards both see a marked card reach done and both ask to offer the close. Exactly one is told yes.
func TestCleanupOfferIsClaimedByExactlyOneCaller(t *testing.T) {
	oh, card, _, _ := openForClose(t)
	task, _ := oh.srv.st.Get(card)
	if err := oh.srv.st.SetTags(card, append(task.Tags, "cleanup:when-done")); err != nil {
		t.Fatal(err)
	}
	const boards = 8
	codes := make([]int, boards)
	var wg sync.WaitGroup
	for i := 0; i < boards; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = post(t, oh.h, "/v1/tasks/"+card+"/cleanup-offer", map[string]any{}).Code
		}(i)
	}
	wg.Wait()
	won, lost := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			won++
		case 409:
			lost++
		default:
			t.Fatalf("codes %v", codes)
		}
	}
	if won != 1 || lost != boards-1 {
		t.Fatalf("claims: %d won, %d lost: %v", won, lost, codes)
	}
	got, _ := oh.srv.st.Get(card)
	var offered, pending bool
	for _, g := range got.Tags {
		offered = offered || g == "cleanup:offered"
		pending = pending || g == "cleanup:when-done"
	}
	if !offered || pending {
		t.Fatalf("tags %v", got.Tags)
	}
}

// The operator's own close takes the marks off, so the card is never offered a close it already had.
func TestCloseTakesTheCleanupMarksOff(t *testing.T) {
	oh, card, _, _ := openForClose(t)
	task, _ := oh.srv.st.Get(card)
	if err := oh.srv.st.SetTags(card, append(task.Tags, "cleanup:when-done")); err != nil {
		t.Fatal(err)
	}
	if code, out := closeCall(t, oh, card, map[string]any{"confirm": true}); code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	got, _ := oh.srv.st.Get(card)
	for _, g := range got.Tags {
		if g == "cleanup:when-done" || g == "cleanup:offered" {
			t.Fatalf("tags %v", got.Tags)
		}
	}
}
