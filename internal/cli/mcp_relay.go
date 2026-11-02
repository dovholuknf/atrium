package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// A stdio relay in front of the hub's HTTP control MCP.
//
// WHY. Claude Code connects an HTTP MCP server once. When the hub goes down, every
// live runner keeps running (the pty host holds them) but its atrium-control tools
// fail with ECONNREFUSED for the rest of the session, because Claude Code never
// redials. A stdio server is the one transport it keeps for the life of the
// session, so this process holds the stdio side and redials the hub per request
// with backoff. A hub restart then costs a slow tool call, not a dead session.
//
// WHAT IT IS NOT. It does not interpret MCP. The hub's control server is stateless
// (every POST is read on its own), so each line in is one POST and each message
// back is one line out. It adds X-Atrium-Agent and X-Atrium-Room from the
// environment atrium sets in every session, which is what the HTTP config's
// `${ATRIUM_AGENT_NAME}` did.

const defaultRelayURL = "http://127.0.0.1:7778/_hub/mcp"

func newMCPRelay() *cobra.Command {
	var url, agent, room string
	var retryFor time.Duration
	c := &cobra.Command{
		Use:   "mcp-relay",
		Short: "Stdio MCP server that relays to the hub's control MCP and survives a hub restart.",
		Long: "Register this as a stdio MCP server instead of the hub's HTTP URL. Claude Code never reconnects a " +
			"dead HTTP MCP server, so a hub restart would leave every live session without atrium tools. " +
			"This keeps the stdio side open and redials the hub, with backoff, for each request.\n\n" +
			"Sends X-Atrium-Agent from ATRIUM_AGENT_NAME and X-Atrium-Room from ATRIUM_ROOM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if url == "" {
				url = os.Getenv("ATRIUM_HUB_MCP")
			}
			if url == "" {
				url = defaultRelayURL
			}
			if agent == "" {
				agent = os.Getenv("ATRIUM_AGENT_NAME")
			}
			if room == "" {
				room = os.Getenv("ATRIUM_ROOM")
			}
			r := &mcpRelay{
				url: url, client: relayClient(), retryFor: retryFor,
				headers: map[string]string{"X-Atrium-Agent": agent, "X-Atrium-Room": room},
				out:     os.Stdout,
			}
			return r.run(cmd.Context(), os.Stdin)
		},
	}
	c.Flags().StringVar(&url, "url", "", "the hub's control MCP (default $ATRIUM_HUB_MCP, else "+defaultRelayURL+")")
	c.Flags().StringVar(&agent, "agent", "", "X-Atrium-Agent (default $ATRIUM_AGENT_NAME)")
	c.Flags().StringVar(&room, "room", "", "X-Atrium-Room (default $ATRIUM_ROOM)")
	c.Flags().DurationVar(&retryFor, "retry-for", 10*time.Minute, "how long one request keeps redialing a hub that is down")
	return c
}

// relayClient bounds the dial only, so a hub that does not take the connection is retried as a dial failure. A tool
// call the hub has taken may run for minutes.
func relayClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 3 * time.Second}).DialContext
	return &http.Client{Transport: t}
}

type mcpRelay struct {
	url      string
	client   *http.Client
	headers  map[string]string
	retryFor time.Duration
	// backoff is the first wait and cap the longest. Zero means 250ms and 5s.
	backoff, cap time.Duration

	out io.Writer
	wmu sync.Mutex

	smu sync.Mutex
	sid string // Mcp-Session-Id, only if the hub ever hands one out
}

func (r *mcpRelay) run(ctx context.Context, in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var wg sync.WaitGroup
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		msg := append([]byte(nil), line...)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.handle(ctx, msg)
		}()
	}
	wg.Wait()
	return sc.Err()
}

func (r *mcpRelay) emit(b []byte) {
	var c bytes.Buffer
	if json.Compact(&c, b) != nil {
		return
	}
	c.WriteByte('\n')
	r.wmu.Lock()
	defer r.wmu.Unlock()
	_, _ = r.out.Write(c.Bytes())
}

func (r *mcpRelay) handle(ctx context.Context, msg []byte) {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(msg, &head)
	hasID := len(head.ID) > 0 && head.Method != ""
	err := r.post(ctx, msg)
	if err != nil && hasID {
		e, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": head.ID,
			"error": map[string]any{"code": -32000, "message": "atrium hub unreachable: " + err.Error()},
		})
		r.emit(e)
	}
}

// post sends one message, redialing while the hub is down, and emits what comes back.
func (r *mcpRelay) post(ctx context.Context, msg []byte) error {
	wait, cap := r.backoff, r.cap
	if wait <= 0 {
		wait = 250 * time.Millisecond
	}
	if cap <= 0 {
		cap = 5 * time.Second
	}
	deadline := time.Now().Add(r.retryFor)
	for {
		retry, err := r.once(ctx, msg)
		if err == nil || !retry {
			return err
		}
		if ctx.Err() != nil || time.Now().Add(wait).After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		if wait *= 2; wait > cap {
			wait = cap
		}
	}
}

// once is one POST. retry says the failure is the hub being away, not an answer.
func (r *mcpRelay) once(ctx context.Context, msg []byte) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(msg))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range r.headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	r.smu.Lock()
	sid := r.sid
	r.smu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Only a request that never reached the hub is sent again. One the hub read and then died on may
		// have run (atrium_launch, atrium_say), so it comes back as the error.
		var op *net.OpError
		return errors.As(err, &op) && op.Op == "dial", err
	}
	defer resp.Body.Close()
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		r.smu.Lock()
		r.sid = id
		r.smu.Unlock()
	}
	switch {
	case resp.StatusCode == http.StatusNotFound && sid != "":
		// A session the restarted hub does not know. Drop it and send again without.
		r.smu.Lock()
		if r.sid == sid {
			r.sid = ""
		}
		r.smu.Unlock()
		return true, fmt.Errorf("hub forgot the session")
	case resp.StatusCode == http.StatusServiceUnavailable:
		return true, fmt.Errorf("hub answered %s", resp.Status)
	case resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent:
		return false, nil
	case resp.StatusCode >= 400:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("hub answered %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				if d = strings.TrimSpace(d); d != "" {
					r.emit([]byte(d))
				}
			}
		}
		return false, nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if b = bytes.TrimSpace(b); len(b) > 0 {
		r.emit(b)
	}
	return false, nil
}
