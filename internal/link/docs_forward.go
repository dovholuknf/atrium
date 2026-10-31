package link

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/docs"
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
