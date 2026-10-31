package link

import (
	"context"
	"errors"
	"net/http"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/forge"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

type (
	CITarget  = forge.CITarget
	CIAnswer  = forge.CIAnswer
	Deliverer = forge.Deliverer
)

// ── the say path ───────────────────────────────────────

type proxyCIDeliverer struct{ p *Proxy }

// Deliverer is the hub's own say path, through the control server's resolution and post.
func (p *Proxy) Deliverer() Deliverer { return proxyCIDeliverer{p} }

func (d proxyCIDeliverer) ctl() *controlMCP {
	d.p.mu.Lock()
	defer d.p.mu.Unlock()
	return d.p.ctl
}

func (d proxyCIDeliverer) Resolve(ctx context.Context, to string) (CITarget, CIAnswer) {
	c := d.ctl()
	if c == nil {
		return CITarget{}, CIAnswer{State: hubstore.CIPending, Detail: "the hub's control server is not up"}
	}
	scope, id, handle, err := c.ResolveCard(ctx, "", to)
	if err != nil {
		var amb *errAmbiguous
		var be *ctl.BoardError
		switch {
		case errors.As(err, &amb):
			return CITarget{}, CIAnswer{State: hubstore.CIRefused, Detail: err.Error()}
		case errors.As(err, &be) && be.Code != http.StatusBadGateway && be.Code != http.StatusServiceUnavailable &&
			be.Code != http.StatusGatewayTimeout && be.Code != http.StatusNotFound:
			return CITarget{}, CIAnswer{State: hubstore.CIRefused, Detail: be.Msg}
		}
		return CITarget{}, CIAnswer{State: hubstore.CIPending, Detail: err.Error()}
	}
	return CITarget{Card: wire.TagFor(scope, id), Room: scope, Handle: handle, ID: id}, CIAnswer{}
}

func (d proxyCIDeliverer) Send(ctx context.Context, t CITarget, from, text, kind string, wake bool) CIAnswer {
	c := d.ctl()
	if c == nil {
		return CIAnswer{State: hubstore.CIPending, Detail: "the hub's control server is not up"}
	}
	ans := c.postSay(ctx, t.Room, t.ID, t.Handle, from, text, "", kind, wake, false)
	switch {
	case ans.OK && ans.Delivered == "parked":
		return CIAnswer{State: hubstore.CIPending, Detail: "the session is parked and the route does not wake it"}
	case ans.OK:
		return CIAnswer{State: hubstore.CIDelivered, Detail: ans.Delivered}
	case ans.Unconfirmed:
		return CIAnswer{State: hubstore.CIUnconfirmed, Detail: ans.Error}
	case ans.Unreachable:
		return CIAnswer{State: hubstore.CIPending, Detail: ans.Error}
	}
	return CIAnswer{State: hubstore.CIRefused, Detail: ans.Error, Gone: ans.Code == http.StatusNotFound}
}
