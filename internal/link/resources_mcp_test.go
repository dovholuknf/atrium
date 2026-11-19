package link

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

func TestLinuxFromCapsShowsSSHHostsWithDockerAndWSLC(t *testing.T) {
	rows := []hubstore.CapabilityRow{
		{Room: "SG4", Cap: "ssh:zbox", State: "fail", Reason: "timeout"},
		{Room: "SG4", Cap: "ssh:agentwsl", State: "pass", Reason: "ok"},
		{Room: "SG4", Cap: "docker:agentwsl", State: "pass"},
		{Room: "SG4", Cap: "wslc", State: "pass"},
		{Room: "SG4", Cap: "docker", State: "fail", Reason: "docker not on PATH"},
		{Room: "SG4", Cap: "go", State: "pass"},
		{Room: "mac", Cap: "go", State: "pass"},
	}
	got := linuxFromCaps(rows)
	if _, ok := got["mac"]; ok {
		t.Fatalf("a room with no linux results has an entry: %+v", got["mac"])
	}
	l := got["sg4"]
	if l == nil || len(l.SSH) != 2 {
		t.Fatalf("sg4 linux wrong: %+v", l)
	}
	if l.SSH[0] != (resourceSSH{Host: "agentwsl", State: "pass", Docker: "pass"}) ||
		l.SSH[1] != (resourceSSH{Host: "zbox", State: "fail: timeout"}) {
		t.Fatalf("ssh hosts wrong: %+v", l.SSH)
	}
	if l.WSLC != "pass" || l.Docker != "fail: docker not on PATH" {
		t.Fatalf("wslc or docker wrong: %+v", l)
	}

	var b strings.Builder
	writeLinux(&b, l)
	for _, want := range []string{"  - linux:\n", "    - ssh agentwsl: pass. docker: pass\n",
		"    - ssh zbox: fail: timeout. docker: not probed (declare docker:zbox)\n", "    - wslc: pass\n",
		"    - docker (DOCKER_HOST or local): fail: docker not on PATH\n"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("text has no %q:\n%s", want, b.String())
		}
	}
}

func TestResourcesPassesItsOutputSchema(t *testing.T) {
	ctx := context.Background()
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	c := &controlMCP{rooms: func() []resourceRoom {
		return []resourceRoom{{Name: "sg4", Online: true, OS: "windows", Arch: "amd64", Host: "h", Version: "v",
			Seen: "2026-11-19T00:00:00Z", Tools: map[string]string{"go": "go.exe"},
			Linux: &resourceLinux{SSH: []resourceSSH{{Host: "agentwsl", State: "pass", Docker: "pass"}}, WSLC: "pass",
				Docker: "fail: no"}}}
	}}
	c.registerResources(s, ctl.ClassFull)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 {
		t.Fatalf("%v %+v", err, tools)
	}
	sb, _ := json.Marshal(tools.Tools[0].OutputSchema)
	var schema jsonschema.Schema
	if err := json.Unmarshal(sb, &schema); err != nil {
		t.Fatal(err)
	}
	rs, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "atrium_resources"})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	b, _ := json.Marshal(res.StructuredContent)
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err := rs.Validate(got); err != nil {
		t.Fatalf("output fails the listed schema: %v\n%s", err, b)
	}
	if !strings.Contains(string(b), `"wslc":"pass"`) || !strings.Contains(string(b), `"last_seen"`) {
		t.Fatalf("a room field is missing: %s", b)
	}
}
