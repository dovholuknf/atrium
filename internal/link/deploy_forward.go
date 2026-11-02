package link

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/deploy"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What the deploy package declares and the rest of this package reaches by its old name.
const (
	SettingDeployBinary = deploy.SettingDeployBinary
	SettingDeployScript = deploy.SettingDeployScript
	SettingDeployOwner  = deploy.SettingDeployOwner
)

// deployService is the deploy readiness, queue and owner, made on first use so a Proxy built as a literal has them too.
func (p *Proxy) deployService() *deploy.Service {
	p.deployOnce.Do(func() {
		p.deploySvc = deploy.New(deploy.Calls{
			Settings: func() deploy.Settings {
				p.mu.Lock()
				defer p.mu.Unlock()
				if p.capStore == nil {
					return nil
				}
				return p.capStore
			},
			RepoFor:   p.repoFor,
			Runner:    func() *gitsync.Runner { return runnerOf(p.git()) },
			Watchers:  func() int { return p.feeds.watchers() },
			Broadcast: func(ev wire.Event) { p.feeds.broadcast(ev) },
			Rooms: func() []wire.Attached {
				if p.hub == nil {
					return nil
				}
				return p.hub.Rooms()
			},
			Attached:    p.attachedView,
			RoomGet:     p.roomGet,
			RecordAudit: p.RecordAudit,
		})
	})
	return p.deploySvc
}

// SetDeployReady turns the state on. A hub that never calls it answers /_hub/deploy-ready with 404.
func (p *Proxy) SetDeployReady() { p.deployService().SetDeployReady() }

func (p *Proxy) deployReady() *deploy.ReadyState { return p.deployService().DeployReady() }

// SetRunningCommit says which commit this hub's own binary was built from.
func (p *Proxy) SetRunningCommit(commit string) { p.deployService().SetRunningCommit(commit) }

func (p *Proxy) deployReadyTick(ctx context.Context) { p.deployService().DeployReadyTick(ctx) }

// deployMCP is the deploy tool over this control server, reading its fields at each call.
func (c *controlMCP) deployMCP() *deploy.MCP {
	m := &deploy.MCP{
		Caller: c,
		Mu:     &c.deployMu,
		Say: func(ctx context.Context, req *mcp.CallToolRequest, to, text string) error {
			_, _, err := c.sayHandler(ctx, req, sayInput{To: to, Text: text})
			return err
		},
	}
	if c.settings != nil {
		m.Settings = func() deploy.Settings { return c.settings() }
	}
	return m
}

func (c *controlMCP) registerDeploy(s *mcp.Server, class ctl.Class) {
	c.deployMCP().RegisterDeploy(s, class)
}
