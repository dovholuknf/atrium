package zitictx

import (
	"errors"
	"testing"

	"github.com/openziti/sdk-golang/ziti"
)

func idle() *ziti.Config {
	return &ziti.Config{ZtAPI: "https://127.0.0.1:1"}
}

func TestNewRefusesPastTheCapAndCloseGivesTheRoomBack(t *testing.T) {
	var open []ziti.Context
	t.Cleanup(func() {
		for _, c := range open {
			c.Close()
		}
	})
	for i := 0; i < MaxLive; i++ {
		c, err := New("test", idle())
		if err != nil {
			t.Fatalf("context %d: %v", i+1, err)
		}
		open = append(open, c)
	}
	if _, err := New("test", idle()); !errors.Is(err, ErrTooMany) {
		t.Fatalf("context %d: got %v, want ErrTooMany", MaxLive+1, err)
	}
	if got := Live(); got != MaxLive {
		t.Fatalf("a refused context was counted: %d live, want %d", got, MaxLive)
	}
	open[0].Close()
	open[0].Close()
	if got := Live(); got != MaxLive-1 {
		t.Fatalf("after one close, %d live, want %d", got, MaxLive-1)
	}
	c, err := New("test", idle())
	if err != nil {
		t.Fatalf("a closed context did not free its place: %v", err)
	}
	open[0] = c
}
