// Package agent is the claude-side MCP server. It exposes a single tool
// `submit` that POSTs the agent's content to the atrium hub and returns the
// next prompt as the tool's structured output.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent holds the client config for the atrium-agent MCP server.
type Agent struct {
	URL    string
	Name   string
	Client *http.Client
}

// SubmitInput is the agent-facing schema for the `submit` tool.
type SubmitInput struct {
	Kind    string `json:"kind"`    // "greeting" on the first call, "response" thereafter
	Content string `json:"content"` // free-form text the human will see in the hub terminal
}

// SubmitOutput is what the tool returns. Prompt is always non-empty when this
// returns successfully: hub long-poll timeouts are absorbed by an internal
// keepalive retry so the LLM never sees them (no token burn on idle).
type SubmitOutput struct {
	Prompt string `json:"prompt"`
}

// agentInstructions is sent back in the MCP InitializeResult. claude-code
// includes this in the model's system prompt automatically.
//
// IMPORTANT activation model: the loop does NOT start automatically. The
// human must explicitly opt in by saying something like "atrium", "run
// atrium", "start atrium", "atrium go", or any phrase that clearly names
// atrium as a verb. Before that activation, behave as a normal claude
// session and ignore the atrium-agent tool entirely.
const agentInstructions = `You have an MCP tool called atrium-agent.submit. It is OPT-IN.
Default behavior: act like a normal claude session. Do NOT call submit, do not
mention atrium, do not loop.

Activate the atrium loop ONLY when the human explicitly asks for it with a
phrase like "atrium", "run atrium", "start atrium", "atrium go", "enter
atrium", or similar wording that names atrium as a verb. Phrases that merely
mention the word atrium in passing (e.g. "the atrium repo is at...") do NOT
activate it. Use judgment.

The activation message is a TRIGGER ONLY. Any additional content in the same
message (e.g. "atrium and then write a file" or "run atrium please do X") is
to be IGNORED. The work comes from the hub, not from the activation message.

Once activated, operate in a loop:

1. Call atrium-agent.submit with kind="greeting" and content set to a brief
   introduction (one sentence is fine). Do NOT do anything else on the
   activation turn -- no tool calls, no files, no shells. Just submit.
2. The tool blocks until the human at the hub terminal types a prompt. It
   returns a "prompt" field. Treat that text as your next instruction.
3. Do whatever the prompt asks (now you can use tools).
4. When finished, call atrium-agent.submit again with kind="response" and
   content set to your reply.
5. Loop forever. Every turn ends with another submit call. The only way out
   is the human stopping the session.

While in the loop, never narrate while waiting for a prompt. Do not generate
user-facing text without a prompt to act on. The submit tool handles polling
silently; you will only ever see real human prompts.

To exit the loop within an active session, the human can say "stop atrium",
"leave atrium", or similar. On that exit phrase, do NOT submit a final reply
to the hub; just acknowledge the exit in the normal claude session and return
to default behavior.`

// New builds the MCP server with the single `submit` tool.
func New(hubURL, name string) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "atrium-agent", Version: "0.0.0-dev"},
		&mcp.ServerOptions{Instructions: agentInstructions},
	)
	a := &Agent{
		URL:  hubURL,
		Name: name,
		Client: &http.Client{
			Timeout: 75 * time.Second, // hub default long-poll is 60s; leave headroom
		},
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "submit",
		Description: "Send your content to the atrium hub and receive the next human prompt back. " +
			"Call with kind='greeting' on your first turn (introduce yourself, ask what to work on). " +
			"On every later turn call with kind='response' and the content of your reply. " +
			"The tool blocks until the human types a prompt. It handles hub downtime and network " +
			"failures internally with silent retries -- you will not see connection errors. " +
			"If this tool ever appears to hang for a long time, the hub is offline; that is normal " +
			"and is not a problem for you to solve. Do not attempt to debug connectivity, do not " +
			"call other tools, do not narrate. Just wait. When the tool returns, 'prompt' is your " +
			"next task and you act on it.\n\n" +
			"FORMATTING: the hub TUI translates curly-brace sentinels in your content to ANSI " +
			"colors before printing. Use them freely in 'content'. Available sentinels: " +
			"{reset} {bold} {dim} {underline}, {black} {red} {green} {yellow} {blue} {magenta} " +
			"{cyan} {white} {gray}, plus background variants like {bgred} {bggreen} etc. The " +
			"hub auto-resets at end of message so you do not have to remember a trailing {reset}. " +
			"Example: 'tests {green}pass{reset} on {bold}main{reset} but {red}fail{reset} elsewhere'.\n\n" +
			"INTERACTIVE CHOICES: when you have a small set of options for the human, wrap them " +
			"in a {choices}...{/choices} block, one option per line. The TUI renders this as a " +
			"numbered picker and the human can press 1-9 to select. Example:\n" +
			"  How would you like to proceed?\n" +
			"  {choices}\n" +
			"  walk through the codebase\n" +
			"  add a new feature\n" +
			"  run the test suite\n" +
			"  something else (describe it)\n" +
			"  {/choices}\n" +
			"Use this whenever the natural reply is a small finite set. Two-option yes/no questions " +
			"are also fine here; do not invent ad-hoc '1) ... 2) ...' prose when {choices} is " +
			"available -- the TUI's picker beats prose for the human.",
	}, a.handleSubmit)

	return s
}

func (a *Agent) handleSubmit(ctx context.Context, _ *mcp.CallToolRequest, in SubmitInput) (*mcp.CallToolResult, any, error) {
	// First call sends the real payload. Subsequent calls (only happen when
	// the hub long-poll times out before a human typed anything) are silent
	// keepalives: empty content + kind="keepalive". The hub does NOT display
	// keepalives, so the human sees one greeting/response per LLM turn, not
	// a stream of polls.
	kind, content := in.Kind, in.Content
	for {
		prompt, err := a.post(ctx, kind, content)
		if err != nil {
			return nil, nil, err
		}
		if prompt != "" {
			return nil, SubmitOutput{Prompt: prompt}, nil
		}
		// Empty prompt = long-poll timeout. Re-poll silently.
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}
		kind, content = "keepalive", ""
	}
}

// post sends one POST and returns the hub's prompt response. On any transport
// failure (hub down, refused, timeout, 5xx) it sleeps with exponential backoff
// and retries forever. The LLM never sees a connection error -- the tool call
// just stays parked until the hub is back. A stderr log fires no more often
// than ATRIUM_DISCONNECTED_LOG_INTERVAL (default 10m) so the operator can grep
// the MCP debug log to confirm the agent is alive and waiting.
func (a *Agent) post(ctx context.Context, kind, content string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"agent":   a.Name,
		"kind":    kind,
		"content": content,
	})
	if err != nil {
		return "", err
	}

	const (
		minBackoff = 5 * time.Second
		maxBackoff = 60 * time.Second
	)
	backoff := minBackoff
	logEvery := disconnectedLogInterval()
	var nextLog time.Time
	firstError := true

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL+"/submit", bytes.NewReader(body))
		if err != nil {
			return "", err // malformed request URL is a real bug, not a connectivity blip
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.Client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			out := struct {
				Prompt string `json:"prompt"`
			}{}
			decErr := json.NewDecoder(resp.Body).Decode(&out)
			_ = resp.Body.Close()
			if decErr == nil {
				if !firstError {
					fmt.Fprintf(os.Stderr, "[atrium-agent %s] hub reachable again -- resumed\n", a.Name)
				}
				return out.Prompt, nil
			}
			// fall through to retry on decode failure
			err = decErr
		}
		if resp != nil {
			_ = resp.Body.Close()
		}

		// Rate-limited stderr nag: once on first failure, then every logEvery.
		if firstError || (logEvery > 0 && time.Now().After(nextLog)) {
			fmt.Fprintf(os.Stderr, "[atrium-agent %s] hub %s unreachable (%v) -- retrying silently every %s\n",
				a.Name, a.URL, err, backoff)
			nextLog = time.Now().Add(logEvery)
			firstError = false
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// disconnectedLogInterval reads ATRIUM_DISCONNECTED_LOG_INTERVAL (Go duration
// syntax, e.g. "10m", "1h"). Default 10 minutes. Zero or negative disables
// periodic logging (still logs once on first error).
func disconnectedLogInterval() time.Duration {
	if v := os.Getenv("ATRIUM_DISCONNECTED_LOG_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Minute
}
