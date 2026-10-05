package link

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/resources"
)

// atrium_resources, the control tool that lists the machines and environments an agent may use. See
// docs/fabric/f-003-resources-design.md.
//
// IN THE WORKER SET, since workers are the ones who need a build machine. Stage 1 reads `resources.md` from the
// state dir of the machine the HUB runs on, and the rooms the hub knows. A room on another machine has its own
// file there, which stage 2 fans out to. The file names hosts, identities and commands and never a credential.

const resourcesToolDesc = "List the machines and environments you may use: build machines reachable by ssh, what is " +
	"installed on them and where, OpenZiti networks and which identity to use, test servers. CALL THIS before you " +
	"look for a machine on your own or ask for one.\n\n" +
	"Answers `file`, the inventory clint keeps by hand in resources.md (one `## <name>` entry each, free prose), " +
	"and `rooms`, the machines running atrium that the hub knows, with their OS and arch and whether they are " +
	"online. A room is itself an entry.\n\n" +
	"READ-ONLY. No agent writes the file: if you find something that belongs in it, tell your director. It names " +
	"hosts and commands and never holds a credential. Where an entry names a working directory rule, build there, " +
	"so two cards do not collide."

type resourcesInput struct{}

type resourceRoom struct {
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	OS      string `json:"os,omitempty"`
	Arch    string `json:"arch,omitempty"`
	Host    string `json:"host,omitempty"`
	Version string `json:"version,omitempty"`
	Seen    string `json:"last_seen,omitempty"`
}

type resourcesOutput struct {
	// File is resources.md as written. Empty when there is none.
	File   string         `json:"file"`
	Exists bool           `json:"exists"`
	Rooms  []resourceRoom `json:"rooms"`
	Text   string         `json:"text"`
}

// SetResourcesDir names the state dir resources.md is read from. Without it the tool says there is no file.
func (p *Proxy) SetResourcesDir(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resourcesDir = dir
}

func (p *Proxy) resourcesStateDir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resourcesDir
}

// roomsForResources is the rooms the hub knows, with the live ones marked. The durable list when the hub keeps
// one, and the connections alone when it does not.
func (p *Proxy) roomsForResources() []resourceRoom {
	var out []resourceRoom
	live := map[string]Attached{}
	for _, a := range p.attachedView() {
		live[a.Name] = a
	}
	if stock := p.inventory(); stock != nil {
		if known, err := stock.Known(); err == nil {
			for _, k := range known {
				r := resourceRoom{Name: k.Name, Online: k.Attached, Host: k.Host}
				if k.LastSeen != nil && !k.Attached {
					r.Seen = k.LastSeen.UTC().Format(time.RFC3339)
				}
				if a, ok := live[k.Name]; ok {
					r.OS, r.Arch, r.Version = a.OS, a.Arch, a.Version
					delete(live, k.Name)
				}
				out = append(out, r)
			}
		}
	}
	for _, a := range live {
		out = append(out, resourceRoom{Name: a.Name, Online: true, OS: a.OS, Arch: a.Arch, Host: a.Host, Version: a.Version})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *controlMCP) resourcesHandler(ctx context.Context, req *mcp.CallToolRequest, in resourcesInput) (
	*mcp.CallToolResult, resourcesOutput, error) {

	out := resourcesOutput{Rooms: []resourceRoom{}}
	var dir string
	if c.resourcesDir != nil {
		dir = c.resourcesDir()
	}
	var note string
	if dir == "" {
		note = "this hub has no state dir to read resources.md from."
	} else {
		text, exists, cut, err := resources.Read(dir)
		if err != nil {
			return nil, out, fmt.Errorf("resources.md could not be read: %v", err)
		}
		out.File, out.Exists = text, exists
		switch {
		case !exists:
			note = "there is no resources.md. clint writes one with `atrium resources init`."
		case cut:
			note = "resources.md is longer than " + fmt.Sprint(resources.MaxBytes) + " bytes and was cut."
		}
	}
	if c.rooms != nil {
		out.Rooms = c.rooms()
	}

	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n\n")
	}
	if out.Exists {
		b.WriteString(strings.TrimRight(out.File, "\n") + "\n\n")
	}
	b.WriteString("rooms:\n")
	for _, r := range out.Rooms {
		state := "offline"
		if r.Online {
			state = "online"
		}
		fmt.Fprintf(&b, "- %s, %s, %s/%s %s\n", r.Name, state, r.OS, r.Arch, r.Version)
	}
	out.Text = b.String()
	return nil, out, nil
}

func (c *controlMCP) registerResources(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_resources", Description: resourcesToolDesc}, c.resourcesHandler)
}
