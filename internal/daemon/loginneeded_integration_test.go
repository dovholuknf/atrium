//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"strings"
	"testing"
	"time"
)

const revokedFrame = "\x1b[H\x1b[2J> hello\r\n  OAuth token revoked · Please run /login\r\n" + frameRule + "\r\n❯ \r\n" + frameRule + "\r\n"

func TestARevokedLoginOnAnyScreenFlagsTheRoomAndRefusesClaudeLaunches(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := runningCard(t, d, revokedFrame, time.Minute)

	d.watchLoginNeeded(time.Now())
	l := d.loginNeeded()
	if l == nil || !strings.Contains(l.Line, "OAuth token revoked") {
		t.Fatalf("flag %+v, wanted the revoked line", l)
	}
	if d.LoginNeededReason() == "" || d.loginHealth() == nil || d.healthBody()["login_needed"] == nil {
		t.Fatal("the flag did not reach the reason, the health answer and the health event")
	}
	err := d.launchNeedsLogin(shared.LaunchRequest{Harness: "claude"})
	if err == nil || !strings.Contains(err.Error(), "/login") {
		t.Fatalf("launch answered %v, wanted a refusal naming /login", err)
	}
	if err := d.launchNeedsLogin(shared.LaunchRequest{Harness: "no-such-runner"}); err != nil {
		t.Fatalf("a harness that is not claude was refused: %v", err)
	}

	// A second look at the same screen changes nothing and says nothing again.
	since := l.Since
	d.watchLoginNeeded(time.Now().Add(time.Second))
	if d.loginNeeded().Since != since {
		t.Fatal("the spell restarted on a repeat sighting")
	}

	// The screen moves on after a /login: the flag comes down.
	_, _ = r.buf.Write([]byte("\x1b[H\x1b[2J" + idleFrame))
	r.lastOut.Store(time.Now().UnixNano())
	d.watchLoginNeeded(time.Now())
	if d.loginNeeded() != nil {
		t.Fatal("still flagged after the screen stopped showing the line")
	}
	if err := d.launchNeedsLogin(shared.LaunchRequest{Harness: "claude"}); err != nil {
		t.Fatalf("launch still refused: %v", err)
	}
}

func TestALoginFlagWithNoRunnerLeftOutlivesItsCardButNotTheTTL(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _ := runningCard(t, d, revokedFrame, time.Minute)
	now := time.Now()
	d.watchLoginNeeded(now)
	d.sup.remove(task.ID)

	d.watchLoginNeeded(now.Add(time.Minute))
	if d.loginNeeded() == nil {
		t.Fatal("cleared the moment its card went, which would let the next launch hit the same wall")
	}
	d.watchLoginNeeded(now.Add(loginNeededTTL + time.Minute))
	if d.loginNeeded() != nil {
		t.Fatal("still flagged past the TTL with nothing showing the line")
	}
}

func TestTheOperatorLiftsTheLoginFlag(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	runningCard(t, d, revokedFrame, time.Minute)
	d.watchLoginNeeded(time.Now())
	if !d.clearLoginNeeded() || d.loginNeeded() != nil {
		t.Fatal("the flag survived being lifted")
	}
	if d.clearLoginNeeded() {
		t.Fatal("lifting a flag that was down said it was up")
	}
}
