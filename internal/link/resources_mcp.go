package link

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/base/resources"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
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
	"online, and where each toolchain (go, node, python, git, gh) is on an online room. A room is itself an entry.\n\n" +
	"Each room's `linux` is how it reaches Linux, from its own probes: the ssh hosts that answer and whether they " +
	"have docker, whether wslc is there, and whether docker answers (DOCKER_HOST or local). Use these before you " +
	"ask anyone for Linux or docker.\n\n" +
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
	// Tools is where each toolchain is on the room, from its hello. Online rooms only.
	Tools map[string]string `json:"tools,omitempty"`
	Linux *resourceLinux    `json:"linux,omitempty"`
}

// resourceLinux is how a room reaches Linux, read from its capability results: each `ssh:<host>` with its
// `docker:<host>`, `wslc`, and `docker`, which follows DOCKER_HOST. A state is pass, or fail or unknown with the reason.
type resourceLinux struct {
	SSH    []resourceSSH `json:"ssh,omitempty"`
	WSLC   string        `json:"wslc,omitempty"`
	Docker string        `json:"docker,omitempty"`
}

type resourceSSH struct {
	Host   string `json:"host"`
	State  string `json:"state"`
	Docker string `json:"docker,omitempty"`
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
	live := map[string]wire.Attached{}
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
					r.OS, r.Arch, r.Version, r.Tools = a.OS, a.Arch, a.Version, a.Tools
					delete(live, k.Name)
				}
				out = append(out, r)
			}
		}
	}
	for _, a := range live {
		out = append(out, resourceRoom{Name: a.Name, Online: true, OS: a.OS, Arch: a.Arch, Host: a.Host, Version: a.Version,
			Tools: a.Tools})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if st := p.capResults(); st != nil {
		if rows, err := st.CapabilityLatest(); err == nil {
			linux := linuxFromCaps(rows)
			for i := range out {
				out[i].Linux = linux[wire.KeyOf(out[i].Name)]
			}
		}
	}
	return out
}

func (p *Proxy) capResults() CapResults {
	p.cap.mu.Lock()
	defer p.cap.mu.Unlock()
	return p.cap.store
}

func capState(r hubstore.CapabilityRow) string {
	if r.State == "pass" || r.Reason == "" {
		return r.State
	}
	return r.State + ": " + r.Reason
}

// linuxFromCaps is each room's Linux reach by room key, and no entry for a room with none of those results.
func linuxFromCaps(rows []hubstore.CapabilityRow) map[string]*resourceLinux {
	out := map[string]*resourceLinux{}
	docker := map[string]map[string]string{}
	at := func(room string) *resourceLinux {
		k := wire.KeyOf(room)
		if out[k] == nil {
			out[k] = &resourceLinux{}
		}
		return out[k]
	}
	for _, r := range rows {
		switch {
		case r.Cap == "wslc":
			at(r.Room).WSLC = capState(r)
		case r.Cap == "docker":
			at(r.Room).Docker = capState(r)
		case strings.HasPrefix(r.Cap, "ssh:"):
			l := at(r.Room)
			l.SSH = append(l.SSH, resourceSSH{Host: strings.TrimPrefix(r.Cap, "ssh:"), State: capState(r)})
		case strings.HasPrefix(r.Cap, "docker:"):
			k := wire.KeyOf(r.Room)
			if docker[k] == nil {
				docker[k] = map[string]string{}
			}
			docker[k][strings.TrimPrefix(r.Cap, "docker:")] = capState(r)
		}
	}
	for k, l := range out {
		for i := range l.SSH {
			l.SSH[i].Docker = docker[k][l.SSH[i].Host]
		}
		sort.Slice(l.SSH, func(i, j int) bool { return l.SSH[i].Host < l.SSH[j].Host })
	}
	return out
}

func writeLinux(b *strings.Builder, l *resourceLinux) {
	if l == nil {
		return
	}
	b.WriteString("  - linux:\n")
	for _, s := range l.SSH {
		d := s.Docker
		if d == "" {
			d = "not probed (declare docker:" + s.Host + ")"
		}
		fmt.Fprintf(b, "    - ssh %s: %s. docker: %s\n", s.Host, s.State, d)
	}
	if l.WSLC != "" {
		fmt.Fprintf(b, "    - wslc: %s\n", l.WSLC)
	}
	if l.Docker != "" {
		fmt.Fprintf(b, "    - docker (DOCKER_HOST or local): %s\n", l.Docker)
	}
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
		names := make([]string, 0, len(r.Tools))
		for n := range r.Tools {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "  - %s: %s\n", n, r.Tools[n])
		}
		writeLinux(&b, r.Linux)
	}
	out.Text = b.String()
	return nil, out, nil
}

func (c *controlMCP) registerResources(s *mcp.Server, class ctl.Class) {
	addTool(s, class, &mcp.Tool{Name: "atrium_resources", Description: resourcesToolDesc}, c.resourcesHandler)
}
