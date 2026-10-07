package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// atrium_publish, the control tool that puts a document on the hub. See docs_api.go for the
// store's HTTP half and docs/rnd/hub-documents-design.md for why it exists.
//
// IN THE WORKER SET, because workers write the reports. `workerTools` names it.
//
// ── three ways bytes leave a card, one door ─────────────
//
// `content` is text the agent passes. `path` is a file in the card's own directory, READ BY THE
// ROOM: the hub never touches a room's disk, it asks the room for `GET /v1/tasks/{id}/files`,
// which is bounded by internal/safepath and answers 403 for anything outside the card, links
// that lead outside included. Either way the bytes reach hubstore.DocAdd, which runs the secret
// content check, so an agent that reads `.env` itself and passes it as `content` meets the same
// refusal as one that names the file.
//
// The name check is on the RESOLVED target. The room says what the path resolved to in
// X-Atrium-Real-Path, and `notes.md -> .env` is refused as `.env`. A room that does not send the
// header is a room from before this tool, and a path publish from it is refused rather than
// guessed at: `content` still works.
//
// ── who is publishing ───────────────────────────────────
//
// The origin is `card <room~id>`, found by asking the caller's room which card answers to the
// X-Atrium-Agent name, the way ctlclass.go does. That header is one anything on loopback can set,
// like every identity on the control server, so a `card` origin means "a session on this machine
// that said it was this card" and never "the operator". Only a `local` origin may be called the
// operator, and that is decided from an HTTP request, which this call is not.

// realPathHeader is the room's api.RealPathHeader. Spelled here because this package does not
// import the room's API.
const realPathHeader = "X-Atrium-Real-Path"

const publishToolDesc = "Publish a document on the hub: a report, a summary, notes. It gets a stable link, " +
	"`/d/<slug>`, that opens on the board and on the phone, and it outlives this card.\n\n" +
	"Give `title`, and either `content` (text, markdown is rendered) or `path` (a file in YOUR OWN card's " +
	"directory, read on your room). Give `slug` to add a NEW VERSION to a document that already exists, " +
	"and the older versions keep their bytes. Answers `{url, slug, version}`.\n\n" +
	"Publishing is not a message and wakes nobody. Say where it is in a `say` or a `report` if somebody " +
	"should read it.\n\n" +
	"THE SECRET CHECKS ARE A SPEED BUMP AND NOT A GUARANTEE. A file named like a secret (`.env`, `*.pem`, " +
	"`id_rsa`, `.npmrc`, anything under `.git/` or `.ssh/`, even through a link) is refused by name, and " +
	"text holding a private key block, a GitHub or Slack token, an AWS key, a JWT or a zrok token is " +
	"refused by content, whether it came as `path` or as `content`. They cannot see a secret in a " +
	"screenshot, in prose, or split in two. Read what you publish. Anyone past the board's password reads " +
	"it, and there is no way for you to override a refusal.\n\n" +
	"Limits: 5 MiB of text, 20 MiB of anything else, and 30 publishes an hour for your card. HTML and SVG " +
	"are stored and offered as text, never rendered."

type publishInput struct {
	Title   string `json:"title,omitempty" jsonschema:"the document's title. needed for a new document. a new version keeps the title it has"`
	Content string `json:"content,omitempty" jsonschema:"the text to publish. give this or path, not both"`
	Path    string `json:"path,omitempty" jsonschema:"a file inside your own card's directory, read on your room. give this or content, not both"`
	Slug    string `json:"slug,omitempty" jsonschema:"to add a new version to the document with this slug. leave out for a new document"`
}

type publishOutput struct {
	URL     string `json:"url"`
	Slug    string `json:"slug"`
	Version int    `json:"version"`
}

func (c *controlMCP) registerDocs(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_publish", Description: publishToolDesc},
		audited(c, "ctl-publish", describePublish, c.publishHandler))
}

func describePublish(req *mcp.CallToolRequest, in publishInput, out publishOutput) (string, string, bool) {
	what := "publish " + strings.TrimSpace(in.Title)
	if out.Slug != "" {
		what = fmt.Sprintf("publish %s v%d", out.Slug, out.Version)
	} else if in.Slug != "" {
		what = "publish a version of " + in.Slug
	}
	return roomOf(req), what, true
}

// docsRefusal is a refusal with the status the HTTP API would have given it, so the same rule
// reads the same on both doors. It is a refusedError underneath, for the audit line.
type docsRefusal struct {
	Status int
	Rule   string
	Msg    string
}

func (e *docsRefusal) Error() string {
	if e.Rule != "" {
		return fmt.Sprintf("%s (%d, rule %s)", e.Msg, e.Status, e.Rule)
	}
	return fmt.Sprintf("%s (%d)", e.Msg, e.Status)
}

func (e *docsRefusal) Unwrap() error { return &refusedError{e.Msg} }

// publishError turns a store refusal into one with its status, and passes the rest through.
func publishError(err error) error {
	if de, ok := hubstore.AsDocError(err); ok {
		return &docsRefusal{Status: docStatus[de.Kind], Rule: de.Rule, Msg: de.Msg}
	}
	return err
}

func (c *controlMCP) publishHandler(ctx context.Context, req *mcp.CallToolRequest, in publishInput) (
	*mcp.CallToolResult, publishOutput, error) {

	var out publishOutput
	if c.docs == nil || c.docs() == nil {
		return nil, out, &refusedError{"this hub has no document store"}
	}
	st := c.docs()
	hasContent, hasPath := in.Content != "", strings.TrimSpace(in.Path) != ""
	switch {
	case hasContent && hasPath:
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "give content or path, not both"}
	case !hasContent && !hasPath:
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "give content or path"}
	}
	slug := strings.TrimSpace(in.Slug)
	if slug != "" && !hubstore.ValidDocSlug(slug) {
		return nil, out, &docsRefusal{Status: http.StatusBadRequest,
			Msg: "a slug is lower case letters, digits and hyphens, at most 60"}
	}
	if slug == "" && strings.TrimSpace(in.Title) == "" {
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "a new document needs a title"}
	}
	agent, room := agentOf(req), roomOf(req)
	if agent == "" {
		return nil, out, &refusedError{"this call carries no " + AgentHeader + ", so the hub cannot say which card " +
			"is publishing. a document records the card that wrote it"}
	}
	card, err := c.callerCard(ctx, room, agent)
	if err != nil {
		return nil, out, err
	}

	data, name := []byte(in.Content), ""
	if hasPath {
		set, err := st.DocSettings()
		if err != nil {
			return nil, out, err
		}
		limit := set.Caps.Text
		if set.Caps.Other > limit {
			limit = set.Caps.Other
		}
		refuseName := func(msg string) error {
			return &docsRefusal{Status: http.StatusUnprocessableEntity, Rule: hubstore.RuleFileName,
				Msg: msg + ". a document is readable by everyone past the board's password. this check is a " +
					"speed bump and not a guarantee"}
		}
		// The name that was asked for is refused before the room is asked, so the answer does not hang on whether
		// the file is there: `.Env` is `.env` on Windows and no file at all on Linux, and both are refused.
		if hubstore.SecretFileName(in.Path) {
			return nil, out, refuseName("that file is refused by name (" + path.Base(strings.ReplaceAll(in.Path, `\`, "/")) + ")")
		}
		var rel string
		if data, rel, err = c.readCardFile(ctx, room, card, strings.TrimSpace(in.Path), limit); err != nil {
			return nil, out, err
		}
		// THE NAME CHECK on what the path resolved to, case-insensitively: a link defeats the one above.
		if hubstore.SecretFileName(rel) {
			msg := "that file is refused by name (" + path.Base(rel) + ")"
			if !strings.EqualFold(path.Base(rel), path.Base(strings.ReplaceAll(in.Path, `\`, "/"))) {
				msg = fmt.Sprintf("%s is a link to %s, which is refused by name", in.Path, rel)
			}
			return nil, out, refuseName(msg)
		}
		name = path.Base(rel)
	}

	res, err := st.DocAdd(hubstore.DocInput{
		Slug: slug, Title: in.Title, Name: name, Data: data,
		Origin: "card", By: callerName(req), Card: tagFor(room, card.ID),
		Rate: true,
	})
	if err != nil {
		return nil, out, publishError(err)
	}
	return nil, publishOutput{URL: "/d/" + res.Slug, Slug: res.Slug, Version: res.Version}, nil
}

// callerCard is the card the X-Atrium-Agent name belongs to on the caller's room.
func (c *controlMCP) callerCard(ctx context.Context, room, agent string) (*ctlCard, error) {
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return nil, fmt.Errorf("could not find your card on %s: %w", docRoomName(room), err)
	}
	card := pickCard(body.Tasks, agent)
	if card == nil {
		return nil, &refusedError{fmt.Sprintf("no card on %s answers to %q, so nothing can be published as it",
			docRoomName(room), agent)}
	}
	return card, nil
}

func docRoomName(room string) string {
	if room == "" {
		return "this hub's room"
	}
	return "room " + room
}

// readCardFile asks the card's room for one file and answers its bytes and what the path
// resolved to, relative to the card. The hub reads nothing from a disk itself: containment and
// the symlink rules are the room's, in internal/safepath, and the room's 403 for a path outside
// the card or through a link to outside is passed on as a 403.
func (c *controlMCP) readCardFile(ctx context.Context, room string, card *ctlCard, p string, limit int64) (
	[]byte, string, error) {

	if card.Worktree == "" {
		return nil, "", &docsRefusal{Status: http.StatusBadRequest, Msg: "your card has no directory to read a path from. pass content"}
	}
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	u := "/v1/tasks/" + url.PathEscape(card.ID) + "/files?path=" + url.QueryEscape(p)
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, c.board+u, nil)
	if err != nil {
		return nil, "", err
	}
	if room != "" {
		hr.Header.Set(RoomHeader, room)
	}
	res, err := c.client.Do(hr)
	if err != nil {
		return nil, "", &boardError{code: http.StatusBadGateway,
			msg: fmt.Sprintf("could not reach the board at %s: %v", c.board, err)}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg := readSentence(res.Body)
		switch res.StatusCode {
		case http.StatusForbidden:
			return nil, "", &docsRefusal{Status: http.StatusForbidden, Msg: orSentence(msg,
				"that path is outside where this card is allowed to reach")}
		case http.StatusNotFound:
			return nil, "", &docsRefusal{Status: http.StatusNotFound, Msg: orSentence(msg, "no such file")}
		case http.StatusBadRequest:
			return nil, "", &docsRefusal{Status: http.StatusBadRequest, Msg: orSentence(msg, "the room would not read that")}
		}
		return nil, "", &docsRefusal{Status: http.StatusBadGateway,
			Msg: fmt.Sprintf("the room answered %d: %s", res.StatusCode, orSentence(msg, res.Status))}
	}
	raw := res.Header.Get(realPathHeader)
	if raw == "" {
		// A ROOM FROM BEFORE THIS TOOL cannot say what a path resolved to, and a name check on the
		// name that was asked for is the one that a link defeats. Refused, not guessed.
		return nil, "", &docsRefusal{Status: http.StatusBadGateway, Msg: "this room is older than the publish tool and cannot " +
			"say what that path resolves to, which the secret check needs. pass content instead, or have the room " +
			"restarted on a current build"}
	}
	rel, err := url.PathUnescape(raw)
	if err != nil || rel == "" {
		return nil, "", &docsRefusal{Status: http.StatusBadGateway, Msg: "the room sent a resolved path that could not be read"}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading the file from the room: %w", err)
	}
	if int64(len(b)) > limit {
		return nil, "", &docsRefusal{Status: http.StatusRequestEntityTooLarge,
			Msg: fmt.Sprintf("that file is over the %d byte cap", limit)}
	}
	return b, rel, nil
}

func readSentence(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4<<10))
	s := strings.TrimSpace(string(b))
	if i := strings.Index(s, `"error"`); i >= 0 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil {
			return strings.TrimSpace(e.Error)
		}
	}
	return ""
}

func orSentence(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
