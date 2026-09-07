package daemon

import (
	"strings"
	"time"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/rest_client_zrok/metadata"
)

// What the account is already using, so a refusal is not the first news of it.
//
// A zrok account has a ceiling on environments, on shares, on reserved names
// and on how much traffic it will carry in a period. Hitting one is not rare
// and the way you find out is a button that thinks for several seconds and
// then refuses inside somebody else's API. This is the panel that says so
// first.
//
// WHAT ZROK WILL AND WILL NOT TELL AN ACCOUNT TOKEN, because the shape of this
// is decided entirely by that:
//
//   - THE USE IS READABLE. `GET /overview` returns every environment on the
//     account with its shares, and every name with whether it is reserved.
//     Those are the three counters `controller/limits/agent.go` compares
//     against a ceiling before it refuses, so counting them here counts
//     exactly the thing that gets checked.
//   - THE CEILINGS ARE NOT. They live on a limit class, and the two endpoints
//     that read one, `/limit-class/list` and `/applied-limit-class/list`, are
//     tagged admin and answer 401 to an ordinary account token. Worse, an
//     account with no limit class applied is measured against the CONTROLLER'S
//     CONFIGURATION, which no endpoint exposes at all, so even an admin token
//     would not answer for the ordinary case.
//
// So this reports use and never a fraction. "6 of 10 shares" would be an
// invented denominator, and a number somebody plans around that atrium made up
// is worse than no number. The one thing zrok does say outright is
// `accountLimited`, which is the TRANSFER allowance and not the counts, and
// that is carried through as its own flag.
//
// ATRIUM'S OWN SHARE OF IT IS SEPARATED OUT. Every name atrium reserves starts
// with `atrium-`, so the panel can say how many of the reserved names on the
// account are its doing. That is the counter atrium grows on its own, one per
// board address and one per lent session, and an account that fills up because
// of atrium should be able to see that from here.

// zrokLimitsTimeout bounds the call. Read on the way into a dialog, so a zrok
// instance that has stopped answering has to fail rather than hold the panel
// open. Longer than a page load and shorter than anybody's patience.
const zrokLimitsTimeout = 15 * time.Second

// atriumNamePrefix is what `newShareNameOf` puts on the front of every name
// atrium invents. Matched here to attribute reservations, and nothing else
// depends on it: a name somebody typed by hand is theirs whatever it starts
// with, and simply does not count towards atrium's share.
const atriumNamePrefix = "atrium-"

// ZrokAccountUse is what the zrok panel shows about the account behind it.
type ZrokAccountUse struct {
	// Limited is zrok's own `accountLimited`, and it is THE TRANSFER ALLOWANCE
	// AND NOTHING ELSE. `isAccountLimited` in `controller/util.go` reads the
	// bandwidth limit journal, so a count above being at its ceiling does not
	// set this and never will. Reading it as "the account is full" would send
	// somebody to delete shares over a bandwidth block.
	//
	// True means the next share is refused rather than may be: `CanCreateShare`
	// checks the same journal before it allocates anything.
	Limited bool `json:"limited"`
	// Environments on the account, everywhere, not just this machine. The
	// limit is per account and every machine that ran `zrok enable` is one.
	Environments int `json:"environments"`
	// Shares open across all of those environments, which is how the
	// controller counts them: it walks every environment and adds up.
	Shares int `json:"shares"`
	// Names on the account, and how many of those are reserved. Only the
	// reserved ones are counted against a limit, so both are shown: a large
	// gap between them is ordinary and means nothing is wrong.
	Names         int `json:"names"`
	ReservedNames int `json:"reserved_names"`
	// AtriumNames is how many reserved names atrium made. See the header.
	AtriumNames int `json:"atrium_names"`
	// HereShares is how many of the shares belong to the environment THIS
	// daemon uses, which is the only number a person can act on directly from
	// this machine.
	HereShares int `json:"here_shares"`
	// Here is whether this machine's environment was found on the account at
	// all. False with no error means the environment on disk is not one this
	// instance knows about, which is what a token from a different instance
	// looks like from here.
	Here bool `json:"here"`
	// Endpoint is which zrok answered, because the whole report is about one
	// account on one instance and the panel offers to change both.
	Endpoint string `json:"endpoint,omitempty"`
	// Err is why the question could not be answered, carried rather than
	// returned. Like `ZitiCapabilities`, this belongs beside the panel as
	// something to read, not as a failed request that paints a generic error.
	Err string `json:"err,omitempty"`
}

// ZrokAccount reports what the configured zrok account is using.
func (d *Daemon) ZrokAccount() ZrokAccountUse {
	var out ZrokAccountUse

	root, err := d.zrokRoot()
	if err != nil {
		out.Err = "could not read the zrok environment: " + err.Error()
		return out
	}
	if !root.IsEnabled() {
		out.Err = "this machine is not enabled with zrok yet, so there is no account to report on"
		return out
	}
	env := root.Environment()
	out.Endpoint = env.ApiEndpoint

	zrok, err := root.Client()
	if err != nil {
		out.Err = zrokSays("could not reach the zrok api", err).Error()
		return out
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", env.AccountToken)

	params := metadata.NewOverviewParams().WithTimeout(zrokLimitsTimeout)
	ovr, err := zrok.Metadata.Overview(params, auth)
	if err != nil {
		out.Err = zrokSays("could not read the zrok account", err).Error()
		return out
	}
	if ovr.Payload == nil {
		out.Err = "the zrok instance answered with nothing to read"
		return out
	}

	// The environment this daemon shares from, named by its ziti identity.
	// That string IS the environment's zId on the controller, which is what
	// the overview keys environments by, and it is the only handle atrium has
	// for "which of these is me".
	here := strings.TrimSpace(env.ZitiIdentity)

	out.Limited = ovr.Payload.AccountLimited
	for _, e := range ovr.Payload.Environments {
		if e == nil {
			continue
		}
		out.Environments++
		out.Shares += len(e.Shares)
		if e.Environment != nil && here != "" && e.Environment.ZID == here {
			out.Here = true
			out.HereShares = len(e.Shares)
		}
	}
	for _, n := range ovr.Payload.Names {
		if n == nil {
			continue
		}
		out.Names++
		if !n.Reserved {
			continue
		}
		out.ReservedNames++
		if strings.HasPrefix(n.Name, atriumNamePrefix) {
			out.AtriumNames++
		}
	}
	return out
}
