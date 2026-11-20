package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/docs"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What the docs package declares and the rest of this package reaches by its old name.
type (
	publishInput  = docs.PublishInput
	publishOutput = docs.PublishOutput
	docsRefusal   = docs.Refusal
)

const hubPublishPath = docs.PublishPath

// ParseDocPath reads /d/<slug> and /d/<slug>@<n>.
func ParseDocPath(p string) (slug string, n int, ok bool) { return docs.ParseDocPath(p) }

// AddRoomPublishTool is atrium_publish on a room's stdio server.
func AddRoomPublishTool(s *mcp.Server, call func(ctx context.Context, method, path string, body, out any) error, by string) {
	docs.AddRoomPublishTool(s, call, by)
}

// docsService is the documents and the file drop, made on first use so a Proxy built as a literal has them too.
func (p *Proxy) docsService() *docs.Service {
	p.docsOnce.Do(func() { p.docsSvc = docs.New(p.RecordAudit) })
	return p.docsSvc
}

// SetDocs wires the document store. Optional: a hub without one answers /_hub/docs 404.
func (p *Proxy) SetDocs(st *hubstore.Store) { p.docsService().SetDocs(st) }

func (p *Proxy) docStore() *hubstore.Store { return p.docsService().DocStore() }

// docsMCP is the document and file tools over this control server, reading its fields at each call.
func (c *controlMCP) docsMCP() *docs.MCP {
	return &docs.MCP{
		Caller: c,
		Docs: func() *hubstore.Store {
			if c.docs == nil {
				return nil
			}
			return c.docs()
		},
		Rooms: func() []docs.Room {
			if c.rooms == nil {
				return nil
			}
			var out []docs.Room
			for _, r := range c.rooms() {
				out = append(out, docs.Room{Name: r.Name, Online: r.Online})
			}
			return out
		},
		Board: c.board, Client: c.client, Timeout: controlTimeout,
	}
}

func (c *controlMCP) registerDocs(s *mcp.Server, class ctl.Class) { c.docsMCP().RegisterDocs(s, class) }

func (c *controlMCP) registerFiles(s *mcp.Server, class ctl.Class) {
	c.docsMCP().RegisterFiles(s, class)
}

func (c *controlMCP) callerCard(ctx context.Context, room, agent string) (*ctlCard, error) {
	return ctl.CallerCard(ctx, c, room, agent)
}

type (
	putInput     = docs.PutInput
	putOutput    = docs.PutOutput
	filesInput   = docs.FilesInput
	filesOutput  = docs.FilesOutput
	getInput     = docs.GetInput
	getOutput    = docs.GetOutput
	exportInput  = docs.ExportInput
	exportOutput = docs.ExportOutput
)

var attachmentHeader = docs.AttachmentHeader

// AddRoomDropTools is atrium_files and atrium_get on a room's stdio server.
func AddRoomDropTools(s *mcp.Server, call func(ctx context.Context, method, path string, body, out any) error) {
	docs.AddRoomDropTools(s, call)
}

// dropAsk answers a room's atrium_files or atrium_get. FROM THE LINK, as publishAsk: the room is the one that attached
// and the card is the sender it names on that room, set as the headers the HTTP control server reads.
func (c *controlMCP) dropAsk(ctx context.Context, from, base string, req wire.RelayRequest) RelayAnswer {
	agent := strings.TrimSpace(req.From)
	call := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: http.Header{
		ctl.AgentHeader: {agent}, wire.RoomHeader: {from}}}}
	dm := c.docsMCP()
	var out any
	var err error
	if base == docs.DropGetPath {
		var in docs.GetInput
		if json.Unmarshal(req.Body, &in) != nil {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "a get is a POST of a JSON object"}
		}
		_, out, err = ctl.Audited(dm, "ctl-get", docs.DescribeGet, dm.GetHandler)(ctx, call, in)
	} else {
		var in docs.FilesInput
		if len(req.Body) > 0 && json.Unmarshal(req.Body, &in) != nil {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "a files list is a POST of a JSON object"}
		}
		_, out, err = dm.FilesHandler(ctx, call, in)
	}
	if err != nil {
		var dr *docsRefusal
		if errors.As(err, &dr) {
			return RelayAnswer{Code: dr.Status, Error: err.Error()}
		}
		return RelayAnswer{Code: http.StatusBadRequest, Error: err.Error()}
	}
	data, _ := json.Marshal(out)
	return RelayAnswer{OK: true, Data: data}
}
