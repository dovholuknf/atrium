package daemon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openziti/sdk-golang/ziti"
)

// What this identity is allowed to do, asked of the network rather than
// guessed at.
//
// Configuring a ziti overlay means typing a service name into a box. Whether
// that name exists, and whether this identity may BIND it rather than only
// dial it, are facts held on the controller, and atrium had no way to show
// either. So "is this going to work" was only answerable by pressing start and
// reading the failure, and the two failures look alike: a service that is not
// there and a service this identity may only dial both come back as the
// listener refusing.
//
// Read-only, and deliberately so. `CLAUDE.md` rules out atrium creating
// services, configs or policies: a network somebody administers is not one a
// board should be editing. Reporting what the network already says is the
// other side of that line and is the half that was missing.

// ZitiService is one service this identity can see.
type ZitiService struct {
	Name string `json:"name"`
	// Bind is whether this identity may host the service, which is the only
	// permission that matters here. Atrium listens; it never dials.
	Bind bool `json:"bind"`
	// Dial is reported because a service that is dial-only is the common
	// mistake, and saying "you can reach this but not host it" is a different
	// instruction from "no such service".
	Dial bool `json:"dial"`
}

// ZitiCapability is what the board shows next to the service box.
type ZitiCapability struct {
	// Identity is who the network thinks this is, so a wrong identity file is
	// visible before anything is started.
	Identity string `json:"identity,omitempty"`
	// Services this identity can see, bindable ones first.
	Services []ZitiService `json:"services"`
	// Bindable is how many of them can actually host, which is the number
	// worth reading if you read nothing else.
	Bindable int `json:"bindable"`
	// Err is why the question could not be answered. Carried rather than
	// returned as an error, because "the controller is unreachable" is
	// something to show beside the box rather than a failure of the request.
	Err string `json:"err,omitempty"`
}

// ZitiCapabilities asks the controller what this identity may bind.
//
// A fresh context each call rather than the one a running share holds. This is
// a question asked while nothing is running, which is when it is worth asking,
// and borrowing a live context would tie answering it to being already started.
func (d *Daemon) ZitiCapabilities() ZitiCapability {
	var out ZitiCapability

	cfg := d.zitiConfig()
	path := strings.TrimSpace(cfg.Identity)
	if path == "" {
		out.Err = "no identity yet. enroll one, or point atrium at an identity file."
		return out
	}

	zcfg, err := ziti.NewConfigFromFile(path)
	if err != nil {
		out.Err = fmt.Sprintf("that identity file could not be read: %v", err)
		return out
	}
	ctx, err := ziti.NewContext(zcfg)
	if err != nil {
		out.Err = fmt.Sprintf("that identity could not be used: %v", err)
		return out
	}
	// Closed on the way out. This context exists to ask one question, and
	// leaving it open would hold an api session per press of the button.
	defer ctx.Close()

	if err := ctx.Authenticate(); err != nil {
		out.Err = fmt.Sprintf("the network would not accept this identity: %v", err)
		return out
	}
	if id, err := ctx.GetCurrentIdentity(); err == nil && id != nil && id.Name != nil {
		out.Identity = *id.Name
	}

	svcs, err := ctx.GetServices()
	if err != nil {
		out.Err = fmt.Sprintf("could not list services: %v", err)
		return out
	}
	for _, s := range svcs {
		if s.Name == nil {
			continue
		}
		svc := ZitiService{Name: *s.Name}
		for _, p := range s.Permissions {
			switch strings.ToLower(string(p)) {
			case "bind":
				svc.Bind = true
			case "dial":
				svc.Dial = true
			}
		}
		if svc.Bind {
			out.Bindable++
		}
		out.Services = append(out.Services, svc)
	}
	// Bindable first, then by name. The list can run to dozens on a real
	// network and the handful that would work are the point of showing it.
	sort.SliceStable(out.Services, func(i, j int) bool {
		if out.Services[i].Bind != out.Services[j].Bind {
			return out.Services[i].Bind
		}
		return out.Services[i].Name < out.Services[j].Name
	})
	return out
}
