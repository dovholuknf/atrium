package link

import (
	"net/http"
)

// hubHandlers are the handlers serveHubAPI dispatches to for the features that leave this package. The chain in
// serveHubAPI picks the branch, as it always did, and each branch calls through one of these.
type hubHandlers struct {
	Files         func(w http.ResponseWriter, r *http.Request, sub string)
	Docs          func(w http.ResponseWriter, r *http.Request, sub string)
	Push          func(w http.ResponseWriter, r *http.Request, sub string)
	Growls        func(w http.ResponseWriter, r *http.Request, sub string)
	CIHook        func(w http.ResponseWriter, r *http.Request, sub string)
	ForgeAdmin    func(w http.ResponseWriter, r *http.Request, sub string)
	PRClaims      func(w http.ResponseWriter, r *http.Request, sub string)
	Backlog       func(w http.ResponseWriter, r *http.Request, sub string)
	Items         func(w http.ResponseWriter, r *http.Request, sub string)
	Flow          func(w http.ResponseWriter, r *http.Request, sub string)
	Merge         func(w http.ResponseWriter, r *http.Request, sub string)
	RoomSpecBoard func(w http.ResponseWriter, r *http.Request, sub string)
	Git           func(w http.ResponseWriter, r *http.Request, sub string)
	Notify        func(w http.ResponseWriter, r *http.Request, sub string)
	DeployReady   func(w http.ResponseWriter, r *http.Request, sub string)
	Deps          func(w http.ResponseWriter, r *http.Request, sub string)
	Work          func(w http.ResponseWriter, r *http.Request, sub string)
	LaunchCaps    func(w http.ResponseWriter, r *http.Request)
	Hosts         func(w http.ResponseWriter, r *http.Request)
	Transports    func(w http.ResponseWriter, r *http.Request)
	DeployOwner   func(w http.ResponseWriter, r *http.Request)
	DeployQueue   func(w http.ResponseWriter, r *http.Request)
	SSHKeysBoard  func(w http.ResponseWriter, r *http.Request)
	GrowlLadder   func(w http.ResponseWriter, r *http.Request)
}

// hubRoutes is the Proxy's own handlers, made on first use so a Proxy built as a literal has them too.
func (p *Proxy) hubRoutes() *hubHandlers {
	p.handlersOnce.Do(func() {
		p.handlers = hubHandlers{
			Files:         p.docsService().ServeFiles,
			Docs:          p.docsService().ServeDocs,
			Push:          p.servePush,
			Growls:        p.serveGrowls,
			CIHook:        p.serveCIHook,
			ForgeAdmin:    p.serveForgeAdmin,
			PRClaims:      p.servePRClaims,
			Backlog:       p.serveBacklog,
			Items:         p.serveItems,
			Flow:          p.serveFlow,
			Merge:         p.serveMerge,
			RoomSpecBoard: p.serveRoomSpecBoard,
			Git:           p.serveGit,
			Notify:        p.serveNotify,
			DeployReady:   p.serveDeployReady,
			Deps:          p.serveDeps,
			Work:          p.serveWork,
			LaunchCaps:    p.serveLaunchCaps,
			Hosts:         p.serveHosts,
			Transports:    p.serveTransports,
			DeployOwner:   p.serveDeployOwner,
			DeployQueue:   p.serveDeployQueue,
			SSHKeysBoard:  p.serveSSHKeysBoard,
			GrowlLadder:   p.serveGrowlLadder,
		}
	})
	return &p.handlers
}
