package testwait

import (
	"testing"
	"time"
)

func TestEventuallyReturnsWhenTheConditionHolds(t *testing.T) {
	n := 0
	Eventually(t, time.Second, "three polls", func() bool { n++; return n == 3 })
	if n != 3 {
		t.Fatalf("polled %d times", n)
	}
}
