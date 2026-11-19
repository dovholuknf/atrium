//go:build integration

package cli

import (
	"net"
	"path/filepath"
	"testing"
)

func TestAHookWithNoAddressFilePostsNowhere(t *testing.T) {
	permEnv(t, "on")
	t.Setenv("ATRIUM_HUB_URL", "")
	t.Setenv("ATRIUM_LOCATION", filepath.Join(t.TempDir(), "none.json"))
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	// Another user's room would be listening here.
	ln, err := net.Listen("tcp", "127.0.0.1:7777")
	if err != nil {
		t.Skipf("default port unavailable: %v", err)
	}
	defer ln.Close()
	hit := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hit <- struct{}{}
			c.Close()
		}
	}()

	if w := reportActivity("", "pre", "tester"); w != "" {
		t.Errorf("activity hook warned %q, want nothing", w)
	}
	if out := permissionHook("", []byte(bashPayload), func() int { return 1 }); out != nil {
		t.Errorf("permission hook said %s, want nothing", out)
	}
	if out := reportSession("", "start", "tester", "claude"); out.warn != "" || out.context != "" {
		t.Errorf("session hook said %+v, want nothing", out)
	}
	if out := turnEnded("", "end", "tester", "claude"); out != keepGoing {
		t.Errorf("stop hook said %q, want nothing", out)
	}
	pingHooksChanged()
	select {
	case <-hit:
		t.Fatal("a hook with no address file connected to the default port")
	default:
	}
}
