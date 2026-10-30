package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/base/docshare"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The hub file drop's four tools: atrium_put, atrium_files, atrium_get and atrium_export. The store is
// internal/hubstore/files.go, the board's half is files_api.go and the design is docs/rnd/hub-file-drop.md.
//
// IN THE WORKER SET, because workers hand files between rooms. `workerTools` names them.
//
// ── what each one touches ───────────────────────────────
//
// PUT reads a file out of the caller's own card THROUGH ITS ROOM, as publish does, and the hub reads no disk of its
// own. GET writes only into the caller's own card, through the room's `PUT /v1/tasks/{id}/files/drop`, which owns
// containment. EXPORT reads each room's usage rows or task list the way the board already does and stores each answer
// as a drop file. Nothing here lets one card write into another's directory: the card is found from the
// X-Atrium-Agent name, and no input names a card.
//
// EVERY DROP FILE IS VISIBLE TO EVERY CARD AND TO THE BOARD. That header is one anything on loopback can set, so a
// recipient list would look like access control and not be one. The tool text says so.

// dropTimeout bounds one transfer to or from a room. 32 MiB over a link can take longer than c.client's eight seconds.
const dropTimeout = 2 * time.Minute

// dropHTTP carries a drop transfer. It has no timeout of its own, the context bounds the call.
var dropHTTP = &http.Client{}

const dropCaveats = "ANYTHING PUT HERE IS READABLE BY EVERY CARD AND BY ANYONE PAST THE BOARD'S PASSWORD. There is no " +
	"recipient list, so a private file does not belong in the drop. Say in `note` who it is for. A file is kept 7 " +
	"days unless `ttl_days` says up to 30, is at most 32 MiB, and can vanish sooner if the board deletes it. THE " +
	"SECRET CHECKS ARE A SPEED BUMP AND NOT A GUARANTEE: a file named like a secret (`.env`, `*.pem`, `id_rsa`) or " +
	"text holding a private key block, a token or an AWS key is refused, and a secret in a binary format, a " +
	"screenshot or prose is not seen."

const putToolDesc = "Leave a file on the hub for a card on another room, or for the board's Files page. Give `path` (a " +
	"file in YOUR OWN card's directory, read on your room) or `content` (text, which needs a `name`). Answers " +
	"`{name, size, expires}`. A name already taken gets a suffix, `usage-2.jsonl`, and the answer has the real " +
	"one: tell the other card that name, and it fetches it with `atrium_get`. A put is not a message and wakes " +
	"nobody.\n\n" + dropCaveats + " 30 puts an hour for your card."

const filesToolDesc = "List the files in the hub's drop, newest first, with who put each one, from which room, and " +
	"when it expires. `mine` lists only your card's puts. Answers `{files, usage: {bytes, cap}}`.\n\n" +
	dropCaveats

const getToolDesc = "Fetch a file from the hub's drop into YOUR OWN card's directory, and answer where it landed as " +
	"`{path, rel, size}`. Without `dest` it lands in `.atrium/incoming/`, under a time stamp. `dest` is a path " +
	"inside your own card (relative, no `..`), made if it is not there. A file already there is not replaced " +
	"unless `replace` is true. You cannot write into another card or outside yours.\n\n" + dropCaveats

const exportToolDesc = "Pull every online room's numbers into the hub's drop with one call, with no card needed on " +
	"those rooms. `what` is `usage` (one session_usage row per line, counts, models, causes and prices, no prompt " +
	"text) or `tasks` (each room's card list). `since` is an RFC 3339 time for usage, default 7 days ago. `rooms` " +
	"names some rooms and defaults to every online one. Answers `{files: [{room, name, size}], skipped: [{room, " +
	"why}]}`: a room that is offline or too old is skipped and the rest still land. Fetch each name with " +
	"`atrium_get`. IT ANSWERS ROWS AND NEVER A RAW DATABASE. To hand over a file you have, use `atrium_put`.\n\n" +
	dropCaveats

type putInput struct {
	Path    string `json:"path,omitempty" jsonschema:"a file inside your own card's directory, read on your room. give this or content, not both"`
	Content string `json:"content,omitempty" jsonschema:"the text to leave. give this or path, not both. needs a name"`
	Name    string `json:"name,omitempty" jsonschema:"the file's name in the drop. needed with content. with path it defaults to the file's own name"`
	Note    string `json:"note,omitempty" jsonschema:"a line saying what it is and who it is for. at most 500 characters"`
	TTLDays int    `json:"ttl_days,omitempty" jsonschema:"days to keep it, 1 to 30. default 7"`
}

type putOutput struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Expires string `json:"expires"`
}

type filesInput struct {
	Mine bool `json:"mine,omitempty" jsonschema:"list only the files your card put"`
}

type dropFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	By        string `json:"by"`
	Card      string `json:"card"`
	Room      string `json:"room"`
	Origin    string `json:"origin"`
	At        string `json:"at"`
	Expires   string `json:"expires"`
	Note      string `json:"note"`
	Downloads int    `json:"downloads"`
}

type dropUsage struct {
	Bytes int64 `json:"bytes"`
	Cap   int64 `json:"cap"`
}

type filesOutput struct {
	Files []dropFile `json:"files"`
	Usage dropUsage  `json:"usage"`
}

type getInput struct {
	Name    string `json:"name" jsonschema:"the file's name in the drop, as atrium_files or atrium_put gave it"`
	Dest    string `json:"dest,omitempty" jsonschema:"a path inside your own card to write it to. relative, no .. segments. default .atrium/incoming/"`
	Replace bool   `json:"replace,omitempty" jsonschema:"replace a file already at dest"`
}

type getOutput struct {
	Path string `json:"path"`
	Rel  string `json:"rel"`
	Size int64  `json:"size"`
}

type exportInput struct {
	What  string   `json:"what" jsonschema:"usage or tasks"`
	Since string   `json:"since,omitempty" jsonschema:"RFC 3339 time. default 7 days ago"`
	Rooms []string `json:"rooms,omitempty" jsonschema:"rooms to pull from. default every online room"`
}

type exportedFile struct {
	Room string `json:"room"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type exportSkip struct {
	Room string `json:"room"`
	Why  string `json:"why"`
}

type exportOutput struct {
	Files   []exportedFile `json:"files"`
	Skipped []exportSkip   `json:"skipped"`
}

func (c *controlMCP) registerFiles(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_put", Description: putToolDesc},
		audited(c, "ctl-put", describePut, c.putHandler))
	// Not audited, as reads are not.
	addTool(s, class, &mcp.Tool{Name: "atrium_files", Description: filesToolDesc}, c.filesHandler)
	addTool(s, class, &mcp.Tool{Name: "atrium_get", Description: getToolDesc},
		audited(c, "ctl-get", describeGet, c.getHandler))
	addTool(s, class, &mcp.Tool{Name: "atrium_export", Description: exportToolDesc},
		audited(c, "ctl-export", describeExport, c.exportHandler))
}

func describePut(req *mcp.CallToolRequest, in putInput, out putOutput) (string, string, bool) {
	what := "put " + strings.TrimSpace(in.Name)
	switch {
	case out.Name != "":
		what = "put " + out.Name
	case strings.TrimSpace(in.Path) != "":
		what = "put " + strings.TrimSpace(in.Path)
	}
	return roomOf(req), what, true
}

func describeGet(req *mcp.CallToolRequest, in getInput, out getOutput) (string, string, bool) {
	return roomOf(req), "get " + strings.TrimSpace(in.Name), true
}

func describeExport(req *mcp.CallToolRequest, in exportInput, out exportOutput) (string, string, bool) {
	what := "export " + strings.TrimSpace(in.What)
	if len(out.Files) > 0 || len(out.Skipped) > 0 {
		what = fmt.Sprintf("%s, %d stored, %d skipped", what, len(out.Files), len(out.Skipped))
	}
	return roomOf(req), what, true
}

// dropStore is the hub's store, or the refusal for a hub without one.
func (c *controlMCP) dropStore() (*hubstore.Store, error) {
	if c.docs == nil || c.docs() == nil {
		return nil, &refusedError{"this hub has no file store"}
	}
	return c.docs(), nil
}

// dropCaller is the caller's room and card. A drop records the card that put a file, and a get writes into one.
func (c *controlMCP) dropCaller(ctx context.Context, req *mcp.CallToolRequest) (string, *ctlCard, error) {
	agent, room := agentOf(req), roomOf(req)
	if agent == "" {
		return "", nil, &refusedError{"this call carries no " + AgentHeader + ", so the hub cannot say which card " +
			"is calling. the drop records the card that put a file and a get writes into the caller's own card"}
	}
	card, err := c.callerCard(ctx, room, agent)
	return room, card, err
}

func (c *controlMCP) putHandler(ctx context.Context, req *mcp.CallToolRequest, in putInput) (
	*mcp.CallToolResult, putOutput, error) {

	var out putOutput
	st, err := c.dropStore()
	if err != nil {
		return nil, out, err
	}
	hasContent, hasPath := in.Content != "", strings.TrimSpace(in.Path) != ""
	switch {
	case hasContent && hasPath:
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "give content or path, not both"}
	case !hasContent && !hasPath:
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "give content or path"}
	case hasContent && strings.TrimSpace(in.Name) == "":
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "content needs a name"}
	}
	room, card, err := c.dropCaller(ctx, req)
	if err != nil {
		return nil, out, err
	}

	data, name := []byte(in.Content), strings.TrimSpace(in.Name)
	if hasPath {
		set, err := st.FileSettings()
		if err != nil {
			return nil, out, err
		}
		refuseName := func(msg string) error {
			return &docsRefusal{Status: http.StatusUnprocessableEntity, Rule: docshare.RuleFileName,
				Msg: msg + ". the drop is readable by everyone past the board's password. this check is a " +
					"speed bump and not a guarantee"}
		}
		// The name that was asked for is refused before the room is asked, so the answer does not hang on whether
		// the file is there.
		if docshare.SecretFileName(in.Path) {
			return nil, out, refuseName("that file is refused by name (" + path.Base(strings.ReplaceAll(in.Path, `\`, "/")) + ")")
		}
		var rel string
		if data, rel, err = c.readCardFileWith(ctx, dropHTTP, dropTimeout, room, card, strings.TrimSpace(in.Path),
			set.Caps.File); err != nil {
			return nil, out, err
		}
		// THE NAME CHECK on what the path resolved to: a link `notes.md -> .env` defeats the one above.
		if docshare.SecretFileName(rel) {
			msg := "that file is refused by name (" + path.Base(rel) + ")"
			if !strings.EqualFold(path.Base(rel), path.Base(strings.ReplaceAll(in.Path, `\`, "/"))) {
				msg = fmt.Sprintf("%s is a link to %s, which is refused by name", in.Path, rel)
			}
			return nil, out, refuseName(msg)
		}
		if name == "" {
			name = path.Base(rel)
		}
	}

	// Content is checked against the cap before it is hashed or scanned.
	if hasContent {
		set, err := st.FileSettings()
		if err != nil {
			return nil, out, err
		}
		if int64(len(data)) > set.Caps.File {
			return nil, out, &docsRefusal{Status: http.StatusRequestEntityTooLarge, Msg: fmt.Sprintf(
				"that is %d bytes, over the %d byte cap for a drop file", len(data), set.Caps.File)}
		}
	}
	row, err := st.FileAdd(hubstore.FileInput{
		Name: name, Note: in.Note, Origin: "card", By: callerName(req), Card: wire.TagFor(room, card.ID), Room: room,
		Data: data, TTLDays: in.TTLDays, Rate: true,
	})
	if err != nil {
		return nil, out, publishError(err)
	}
	return nil, putOutput{Name: row.Name, Size: row.Size, Expires: row.Expires}, nil
}

func (c *controlMCP) filesHandler(ctx context.Context, req *mcp.CallToolRequest, in filesInput) (
	*mcp.CallToolResult, filesOutput, error) {

	out := filesOutput{Files: []dropFile{}}
	st, err := c.dropStore()
	if err != nil {
		return nil, out, err
	}
	card := ""
	if in.Mine {
		room, cd, err := c.dropCaller(ctx, req)
		if err != nil {
			return nil, out, err
		}
		card = wire.TagFor(room, cd.ID)
	}
	rows, err := st.FileList(card)
	if err != nil {
		return nil, out, publishError(err)
	}
	for _, r := range rows {
		out.Files = append(out.Files, dropFile{Name: r.Name, Size: r.Size, By: r.By, Card: r.Card, Room: r.Room,
			Origin: r.Origin, At: r.At, Expires: r.Expires, Note: r.Note, Downloads: r.Downloads})
	}
	set, err := st.FileSettings()
	if err != nil {
		return nil, out, err
	}
	used, err := st.FileUsage()
	if err != nil {
		return nil, out, err
	}
	out.Usage = dropUsage{Bytes: used, Cap: set.Caps.Total}
	return nil, out, nil
}

func (c *controlMCP) getHandler(ctx context.Context, req *mcp.CallToolRequest, in getInput) (
	*mcp.CallToolResult, getOutput, error) {

	var out getOutput
	st, err := c.dropStore()
	if err != nil {
		return nil, out, err
	}
	name, dest := strings.TrimSpace(in.Name), strings.TrimSpace(in.Dest)
	if name == "" {
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "say which file, with name"}
	}
	room, card, err := c.dropCaller(ctx, req)
	if err != nil {
		return nil, out, err
	}
	if card.Worktree == "" {
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "your card has no directory to put a file in"}
	}
	if dest != "" && docshare.SecretFileName(dest) {
		return nil, out, &docsRefusal{Status: http.StatusUnprocessableEntity, Rule: docshare.RuleFileName,
			Msg: "that destination is named like a secret (" + path.Base(strings.ReplaceAll(dest, `\`, "/")) + "), " +
				"so nothing was written"}
	}
	f, row, err := st.FileOpen(name)
	if err != nil {
		return nil, out, publishError(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, out, fmt.Errorf("reading the file from the drop: %w", err)
	}

	q := "name=" + url.QueryEscape(row.Name) + "&dest=" + url.QueryEscape(dest)
	if in.Replace {
		q += "&replace=1"
	}
	ctx, cancel := context.WithTimeout(ctx, dropTimeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.board+"/v1/tasks/"+url.PathEscape(card.ID)+"/files/drop?"+q, bytes.NewReader(data))
	if err != nil {
		return nil, out, err
	}
	hr.Header.Set("Content-Type", "application/octet-stream")
	if room != "" {
		hr.Header.Set(wire.RoomHeader, room)
	}
	res, err := dropHTTP.Do(hr)
	if err != nil {
		return nil, out, &boardError{code: http.StatusBadGateway,
			msg: fmt.Sprintf("could not reach the board at %s: %v", c.board, err)}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg := readSentence(res.Body)
		switch res.StatusCode {
		case http.StatusForbidden:
			return nil, out, &docsRefusal{Status: http.StatusForbidden, Msg: orSentence(msg,
				"that path is outside where this card is allowed to reach")}
		case http.StatusNotFound:
			if msg == "" {
				return nil, out, &docsRefusal{Status: http.StatusBadGateway, Msg: docRoomName(room) +
					" is older than the file drop, restart it on a current build"}
			}
			return nil, out, &docsRefusal{Status: http.StatusNotFound, Msg: msg}
		case http.StatusConflict:
			return nil, out, &docsRefusal{Status: http.StatusConflict, Msg: orSentence(msg,
				"a file is already there, pass replace")}
		case http.StatusRequestEntityTooLarge:
			return nil, out, &docsRefusal{Status: http.StatusRequestEntityTooLarge, Msg: orSentence(msg,
				"the room would not take a file that large")}
		case http.StatusBadRequest:
			return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: orSentence(msg, "the room would not take that")}
		}
		return nil, out, &docsRefusal{Status: http.StatusBadGateway,
			Msg: fmt.Sprintf("the room answered %d: %s", res.StatusCode, orSentence(msg, res.Status))}
	}
	var got struct {
		Path string `json:"path"`
		Rel  string `json:"rel"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&got); err != nil {
		return nil, out, &docsRefusal{Status: http.StatusBadGateway, Msg: "the room's answer could not be read"}
	}
	return nil, getOutput{Path: got.Path, Rel: got.Rel, Size: int64(len(data))}, nil
}

func (c *controlMCP) exportHandler(ctx context.Context, req *mcp.CallToolRequest, in exportInput) (
	*mcp.CallToolResult, exportOutput, error) {

	out := exportOutput{Files: []exportedFile{}, Skipped: []exportSkip{}}
	st, err := c.dropStore()
	if err != nil {
		return nil, out, err
	}
	what := strings.TrimSpace(in.What)
	if what != "usage" && what != "tasks" {
		return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "what is usage or tasks"}
	}
	since := time.Now().UTC().Add(-7 * 24 * time.Hour)
	if v := strings.TrimSpace(in.Since); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, out, &docsRefusal{Status: http.StatusBadRequest, Msg: "since is not an RFC 3339 time"}
		}
		since = t
	}
	if c.rooms == nil {
		return nil, out, &refusedError{"this hub cannot list its rooms"}
	}
	online := map[string]bool{}
	var all []string
	for _, r := range c.rooms() {
		online[r.Name] = r.Online
		if r.Online {
			all = append(all, r.Name)
		}
	}
	rooms := in.Rooms
	if len(rooms) == 0 {
		rooms = all
	}
	_, card, err := c.dropCaller(ctx, req)
	if err != nil {
		return nil, out, err
	}
	set, err := st.FileSettings()
	if err != nil {
		return nil, out, err
	}
	callerTag := wire.TagFor(roomOf(req), card.ID)
	stamp := time.Now().UTC().Format("20060102-1504")

	for _, room := range rooms {
		room = strings.TrimSpace(room)
		skip := func(why string) { out.Skipped = append(out.Skipped, exportSkip{Room: room, Why: why}) }
		on, known := online[room]
		switch {
		case !known:
			skip("no room by that name")
			continue
		case !on:
			skip("room is offline")
			continue
		}
		p, ext := "/v1/tasks", "json"
		if what == "usage" {
			p, ext = "/v1/usage/export?since="+url.QueryEscape(since.Format(time.RFC3339)), "jsonl"
		}
		body, why := c.exportFetch(ctx, room, p, set.Caps.File)
		if why != "" {
			skip(why)
			continue
		}
		if len(body) == 0 {
			skip("no rows since " + since.Format(time.RFC3339))
			continue
		}
		row, err := st.FileAdd(hubstore.FileInput{
			Name: fmt.Sprintf("%s-%s-%s.%s", what, room, stamp, ext), Origin: "card", By: callerName(req),
			Card: callerTag, Room: room, Data: body,
			Note: fmt.Sprintf("export of %s from %s since %s", what, room, since.Format(time.RFC3339)),
		})
		if err != nil {
			if _, ok := hubstore.AsDocError(err); !ok {
				return nil, out, err
			}
			skip(err.Error())
			continue
		}
		out.Files = append(out.Files, exportedFile{Room: room, Name: row.Name, Size: row.Size})
	}
	return nil, out, nil
}

// exportFetch reads one room's route through the hub's own board, answering the bytes or why the room is skipped.
func (c *controlMCP) exportFetch(ctx context.Context, room, p string, limit int64) ([]byte, string) {
	ctx, cancel := context.WithTimeout(ctx, dropTimeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, c.board+p, nil)
	if err != nil {
		return nil, err.Error()
	}
	hr.Header.Set(wire.RoomHeader, room)
	res, err := dropHTTP.Do(hr)
	if err != nil {
		return nil, "could not reach the room: " + err.Error()
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg := readSentence(res.Body)
		if res.StatusCode == http.StatusNotFound && msg == "" {
			return nil, "room is older than the file drop"
		}
		return nil, fmt.Sprintf("the room answered %d: %s", res.StatusCode, orSentence(msg, res.Status))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "reading from the room: " + err.Error()
	}
	if int64(len(b)) > limit {
		return nil, "over the file cap, narrow since"
	}
	return b, ""
}
