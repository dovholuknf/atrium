package transport

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/environment/env_core"
	"github.com/openziti/zrok/v2/rest_client_zrok/share"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
)

// The zrok access kind: any number of entries, each its own share on its own environment.
//
// A private share is created CLOSED, so only the account that owns the environment reaches it, and every other
// account is a grant by name. A public share is the board only, answers on a reserved `atrium-` name, and is never
// started without the sign-in guard in front of it, which the hub applies to every board exposure.

// The zrok entry's settings.
const (
	ZrokSettingEnv       = "env"
	ZrokSettingMode      = "mode"
	ZrokSettingTokenFile = "token_file"
	ZrokSettingName      = "name"
	ZrokSettingGrants    = "grants"
)

// ZrokPublicNamespace is the namespace a public name is reserved in.
const ZrokPublicNamespace = "public"

func init() { RegisterAccess(zrokKind{}) }

type zrokKind struct{}

func (zrokKind) Kind() string { return "zrok" }

func (zrokKind) Fields() []AccessField {
	return []AccessField{
		{Name: ZrokSettingMode, Label: "private or public"},
		{Name: ZrokSettingEnv, Label: "zrok environment directory (empty for this machine's)"},
		{Name: ZrokSettingTokenFile, Label: "account token file, to enable an environment that has none", Secret: true},
		{Name: ZrokSettingName, Label: "public name (atrium- is added)"},
		{Name: ZrokSettingGrants, Label: "other zrok accounts let in (open it up), comma separated"},
	}
}

// zrokMode reads an entry's mode and what it serves, refusing what zrok cannot carry: a share is one backend, so it
// serves the board or the link, and the link is never public.
func zrokMode(e Entry) (public bool, board bool, err error) {
	mode := strings.TrimSpace(e.Settings[ZrokSettingMode])
	switch mode {
	case "", "private":
	case "public":
		public = true
	default:
		return false, false, fmt.Errorf("a zrok entry is private or public, not %q", mode)
	}
	switch e.Serves {
	case ServesBoard:
		board = true
	case ServesLink:
	default:
		return false, false, fmt.Errorf("a zrok entry serves the board or the room link, one share each, not both")
	}
	if public && !board {
		return false, false, fmt.Errorf("the room link is never on a public share")
	}
	return public, board, nil
}

func (zrokKind) Hub(e Entry) (HubAccess, error) {
	public, board, err := zrokMode(e)
	if err != nil {
		return nil, err
	}
	return &zrokHub{e: e, public: public, board: board, z: zrokFor(e)}, nil
}

func (zrokKind) Room(e Entry) (RoomAccess, error) {
	if public, board, err := zrokMode(e); err != nil {
		return nil, err
	} else if public || board {
		return nil, nil
	}
	return zrokFor(e), nil
}

func zrokFor(e Entry) *Zrok {
	return &Zrok{Token: ZrokEntryToken(e.ID), EnvDir: e.Settings[ZrokSettingEnv], TokenFile: e.Settings[ZrokSettingTokenFile]}
}

// Mutual is false: the share proves an account, and the link's mTLS inside it proves the room.
func (z *Zrok) Mutual() bool { return false }

// zrokHash is the same lowercase letters and digits for the same entry, which zrok accepts as a token or a name.
func zrokHash(what, id string, n int) string {
	sum := sha256.Sum256([]byte("atrium-access-" + what + "|" + id))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))[:n]
}

// ZrokEntryToken is a private entry's share token, derived from its id so the hub and a room agree on it with nothing
// stored. Under closed mode the token is not the credential, the account is.
func ZrokEntryToken(id string) string { return "atrium-" + zrokHash("token", id, 16) }

// ZrokEntryName is a public entry's reserved name: the one typed, prefixed `atrium-`, or one derived from its id.
func ZrokEntryName(e Entry) string {
	n := strings.ToLower(strings.TrimSpace(e.Settings[ZrokSettingName]))
	if n == "" {
		return "atrium-" + zrokHash("name", e.ID, 12)
	}
	if !strings.HasPrefix(n, "atrium-") {
		n = "atrium-" + n
	}
	return n
}

func zrokGrants(e Entry) []string {
	var out []string
	for _, g := range strings.Split(e.Settings[ZrokSettingGrants], ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

type zrokHub struct {
	e      Entry
	public bool
	board  bool
	z      *Zrok

	mu    sync.Mutex
	ln    net.Listener
	token string
	addr  string
	err   error
}

func (h *zrokHub) Listen(ctx context.Context) (net.Listener, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ln, err := h.listen()
	h.err = err
	return ln, err
}

func (h *zrokHub) listen() (net.Listener, error) {
	root, err := h.z.environmentRoot()
	if err != nil {
		return nil, err
	}
	api, err := h.z.api()
	if err != nil {
		return nil, err
	}
	backend := zroksdk.TcpTunnelBackendMode
	if h.board {
		backend = zroksdk.ProxyBackendMode
	}
	req := &zroksdk.ShareRequest{BackendMode: backend, Target: "atrium-" + h.e.ID}
	if h.public {
		name := ZrokEntryName(h.e)
		if err := ReserveZrokName(root, ZrokPublicNamespace, name); err != nil {
			return nil, err
		}
		req.ShareMode = zroksdk.PublicShareMode
		req.NameSelections = []zroksdk.NameSelection{{NamespaceToken: ZrokPublicNamespace, Name: name}}
	} else {
		req.ShareMode = zroksdk.PrivateShareMode
		req.PermissionMode = zroksdk.ClosedPermissionMode
		req.PrivateShareToken = ZrokEntryToken(h.e.ID)
		req.AccessGrants = zrokGrants(h.e)
		ok, err := api.Exists(req.PrivateShareToken)
		if err != nil {
			return nil, fmt.Errorf("could not ask zrok whether this entry's share is there: %w", err)
		}
		if ok {
			// Created by this kind, so closed. Its grants are brought to the entry's.
			if err := ZrokGrant(root, req.PrivateShareToken, req.AccessGrants, nil); err != nil {
				return nil, err
			}
			return h.answer(api, req.PrivateShareToken, "zrok access private "+req.PrivateShareToken)
		}
	}
	shr, err := zroksdk.CreateShare(root, req)
	if err != nil {
		return nil, fmt.Errorf("could not create the zrok share: %w", err)
	}
	addr := "zrok access private " + shr.Token
	if len(shr.FrontendEndpoints) > 0 {
		addr = shr.FrontendEndpoints[0]
	}
	return h.answer(api, shr.Token, addr)
}

func (h *zrokHub) answer(api ZrokAPI, token, addr string) (net.Listener, error) {
	ln, err := api.Listen(token)
	if err != nil {
		return nil, fmt.Errorf("the share exists and nothing could answer it: %w", err)
	}
	h.ln, h.token, h.addr = ln, token, addr
	return ln, nil
}

// Join is the share token a room dials, for a link entry. A board entry has none.
func (h *zrokHub) Join(string) (string, error) {
	if h.board {
		return "", nil
	}
	return ZrokEntryToken(h.e.ID), nil
}

func (h *zrokHub) Name(net.Conn) string { return "" }
func (h *zrokHub) Key(net.Conn) string  { return "" }

// Address is what the other end is given: the public URL, or the access command for a private share.
func (h *zrokHub) Address() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.addr
}

// Public says this entry is a public share, which the hub refuses to start without the sign-in guard.
func (h *zrokHub) Public() bool { return h.public }

// Revoke stops answering, deletes the share and gives a public name back, so nothing is left on the account.
func (h *zrokHub) Revoke(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ln != nil {
		_ = h.ln.Close()
		h.ln = nil
	}
	root, err := h.z.environmentRoot()
	if err != nil {
		return err
	}
	token := h.token
	if token == "" && !h.public {
		token = ZrokEntryToken(h.e.ID)
	}
	var errs []error
	if token != "" {
		if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: token}); err != nil && !zrokNotThere(err) {
			errs = append(errs, fmt.Errorf("could not delete the share: %w", err))
		}
	}
	if h.public {
		if err := ReleaseZrokName(root, ZrokPublicNamespace, ZrokEntryName(h.e)); err != nil {
			errs = append(errs, err)
		}
	}
	h.token, h.addr, h.err = "", "", nil
	return errors.Join(errs...)
}

// Grant lets more accounts in to a private entry, the "open it up" action. Ungrant takes them out again.
func (h *zrokHub) Grant(accounts []string) error   { return h.grant(accounts, nil) }
func (h *zrokHub) Ungrant(accounts []string) error { return h.grant(nil, accounts) }

func (h *zrokHub) grant(add, remove []string) error {
	if h.public {
		return fmt.Errorf("a public share has no grants: the sign-in is its gate")
	}
	root, err := h.z.environmentRoot()
	if err != nil {
		return err
	}
	return ZrokGrant(root, ZrokEntryToken(h.e.ID), add, remove)
}

func (h *zrokHub) State() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.err != nil:
		return "failed: " + h.err.Error()
	case h.ln == nil:
		return "off"
	case h.public:
		return "public at " + h.addr
	}
	s := "private, closed to this account"
	if g := zrokGrants(h.e); len(g) > 0 {
		s += " and " + strings.Join(g, ", ")
	}
	return s
}

// ZrokGrant adds and removes accounts on a closed share. An add removes first, so granting twice is not an error.
func ZrokGrant(root env_core.Root, token string, add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	zrok, err := root.Client()
	if err != nil {
		return fmt.Errorf("could not reach the zrok api: %w", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)
	for _, body := range []share.UpdateShareBody{
		{ShareToken: token, RemoveAccessGrants: append(append([]string{}, add...), remove...)},
		{ShareToken: token, AddAccessGrants: add},
	} {
		if len(body.AddAccessGrants)+len(body.RemoveAccessGrants) == 0 {
			continue
		}
		p := share.NewUpdateShareParams()
		p.Body = body
		if _, err := zrok.Share.UpdateShare(p, auth); err != nil {
			return fmt.Errorf("could not change who may access the share: %w", err)
		}
	}
	return nil
}

// ReserveZrokName creates a name and marks it reserved, so a public address survives a restart. Both steps every
// time, since a name never created and a name created ephemeral look the same from here.
func ReserveZrokName(root env_core.Root, namespace, name string) error {
	zrok, err := root.Client()
	if err != nil {
		return fmt.Errorf("could not reach the zrok api: %w", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)
	create := share.NewCreateShareNameParams()
	create.Body = share.CreateShareNameBody{NamespaceToken: namespace, Name: name}
	if _, err := zrok.Share.CreateShareName(create, auth); err != nil {
		s := strings.ToLower(err.Error())
		if strings.Contains(s, "limit reached") || !(strings.Contains(s, "conflict") || strings.Contains(s, "already")) {
			return fmt.Errorf("could not create the name %q: %w", name, err)
		}
	}
	reserve := share.NewUpdateShareNameParams()
	reserve.Body = share.UpdateShareNameBody{NamespaceToken: namespace, Name: name, Reserved: true}
	if _, err := zrok.Share.UpdateShareName(reserve, auth); err != nil {
		return fmt.Errorf("the name %q exists but could not be reserved: %w", name, err)
	}
	return nil
}

// ReleaseZrokName gives a reserved name back. One already gone is not an error.
func ReleaseZrokName(root env_core.Root, namespace, name string) error {
	zrok, err := root.Client()
	if err != nil {
		return fmt.Errorf("could not reach the zrok api: %w", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)
	del := share.NewDeleteShareNameParams()
	del.Body = share.DeleteShareNameBody{NamespaceToken: namespace, Name: name}
	if _, err := zrok.Share.DeleteShareName(del, auth); err != nil && !zrokNotThere(err) {
		return fmt.Errorf("could not release the name %q: %w", name, err)
	}
	return nil
}

func zrokNotThere(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "notfound") || strings.Contains(s, "not found") || strings.Contains(s, "404")
}
