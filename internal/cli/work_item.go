package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// EVERY LAUNCH NAMES AN ITEM (item lifecycle L1). The two doors a session or a script has here, `atrium launch` and the
// stdio atrium_launch, both turn their item argument into a backlog id through link.ResolveItem and stamp the tag
// `atrium:item:<id>` on the launch. What an argument may say, and the refusal that names the flag, are in
// internal/link/work_launch.go so the doors cannot drift.

// roomHubCall reaches the hub the way the stdio server does everything on it: through this room's daemon, which asks
// the hub over its link and has the hub stamp who asked.
func roomHubCall(me string) link.HubCall {
	return func(ctx context.Context, method, path string, body, out any) error {
		return askFor(ctx, hubAskWait, http.MethodPost, "/v1/hub/ask",
			map[string]any{"from": me, "method": method, "path": path, "body": body}, out)
	}
}

// resolveLaunchItem is the stdio server's: the id a launch is attached to, and a sentence for the caller when the item was
// filed on the way. The department a `new:` is filed under is the launcher's own.
func resolveLaunchItem(ctx context.Context, spec string, tags []string) (id, note string, err error) {
	me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
	dept := link.DeptOfTags(append(append([]string{}, tags...), myTags(ctx, me)...))
	id, filed, err := link.ResolveItem(ctx, roomHubCall(me), spec, dept, me, strings.TrimSpace(os.Getenv("ATRIUM_ROOM")))
	if err != nil {
		return "", "", err
	}
	if filed {
		note = "filed backlog item " + id + " for --item new. "
	}
	return id, note, nil
}

// boardHubCall reaches the hub's own endpoints over the board address, plain HTTP, for a script with no session.
func boardHubCall(boardURL string) link.HubCall {
	client := &http.Client{Timeout: 15 * time.Second}
	return func(ctx context.Context, method, path string, body, out any) error {
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, boardAddress(boardURL)+path, rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 300 {
			var e struct{ Error string }
			msg := resp.Status
			if json.Unmarshal(raw, &e) == nil && e.Error != "" {
				msg = e.Error
			}
			return &peerToolErr{code: resp.StatusCode, msg: msg, bare: e.Error == ""}
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("could not read the hub's answer: %w", err)
			}
		}
		return nil
	}
}
