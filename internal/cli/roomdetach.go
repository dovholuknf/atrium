package cli

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// detachWait is how long `atrium room --detach` waits for the room it started
// to answer before saying it did not.
var detachWait = 30 * time.Second

// detachRoom is `atrium room --detach`: start the room in the background and
// return once its own board answers.
//
// FOR A ROOM STARTED OVER SSH, which is how scripts/provision-room.ps1 starts
// one when it installs no autostart. The room has to outlive the ssh session
// that started it, and startDetached is the spawn that does: a new session on
// Unix, a detached process that breaks away from the ssh job on Windows.
//
// A ROOM ALREADY ANSWERING IS LEFT ALONE, so running this twice starts one room.
func detachRoom(l roomLaunch) error {
	if _, err := (link.Keys{Dir: l.dir}).Joined(); err != nil {
		return err
	}
	health := "http://" + l.human + "/v1/health"
	if answers(health) {
		fmt.Printf("  a room already answers at http://%s. left it running.\n", l.human)
		return nil
	}
	p, logPath, err := startRoom(l)
	if err != nil {
		return fmt.Errorf("could not start the room: %w", err)
	}
	pid := p.Pid
	_ = p.Release()
	deadline := time.Now().Add(detachWait)
	for time.Now().Before(deadline) {
		if answers(health) {
			fmt.Printf("  started the room, pid %d, answering at http://%s\n", pid, l.human)
			fmt.Printf("  it logs to %s\n", logPath)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("started the room as pid %d but it did not answer at http://%s within %s. "+
		"its log is %s", pid, l.human, detachWait, logPath)
}

func answers(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
