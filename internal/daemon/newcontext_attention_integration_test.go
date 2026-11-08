//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestACycleThatCannotClearRaisesAttention(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	until(t, "the card to ask for attention", func() bool {
		got, err := d.st.Get(task.ID)
		return err == nil && got.Status == store.StatusNeedsInput && got.WaitingReason == store.WaitingAsked
	})
}
