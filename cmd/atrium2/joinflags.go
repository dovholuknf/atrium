package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// The transport half of `atrium2 join`, split out so it can be tested without a
// hub, a network or a cobra command.
//
// The design this implements is `docs/ziti-zrok-flow-design.md`, "The room link:
// `atrium2 join` with a transport". The token names the room and pins the hub's
// trust anchor. These flags say HOW to reach the hub and what to prove the right
// with, which the token used to have to carry itself:
//
//	--mtls <hub url>            the direct transport, mutual TLS over TCP
//	--zrok-private <token>      a private zrok share, the access token is the gate
//	--openziti <jwt-or-json>    an OpenZiti service, plus --service to name it
//
// There is deliberately no --zrok-public. The room link is never fronted by a
// public URL: reachability would be the authorisation and a public URL is
// reachable by anyone. See the design's transport table.

// joinFlags are the transport choices on `atrium2 join`, layered on the token.
type joinFlags struct {
	mtls        string // direct: a plain hub url or host:port
	zrokPrivate string // zrok private: the private share access token
	openziti    string // ziti: a jwt, or a path to a jwt or an enrolled .json
	service     string // ziti: the service to dial, default "atrium"
	identity    string // legacy alias for an enrolled ziti .json
}

// chosen names the one transport flag set, "" for none, and refuses more than
// one. A join reaches a hub exactly one way, so two flags is a question nobody
// can answer for the operator.
func (f joinFlags) chosen() (string, error) {
	set := map[string]bool{
		"--mtls":         strings.TrimSpace(f.mtls) != "",
		"--zrok-private": strings.TrimSpace(f.zrokPrivate) != "",
		"--openziti":     strings.TrimSpace(f.openziti) != "",
	}
	var picked []string
	for name, on := range set {
		if on {
			picked = append(picked, name)
		}
	}
	sort.Strings(picked)
	if len(picked) > 1 {
		return "", fmt.Errorf("give one transport, not %s", strings.Join(picked, " and "))
	}
	if len(picked) == 1 {
		return picked[0], nil
	}
	return "", nil
}

// resolve merges the parsed token with the flags into the effective join and, for
// ziti, the identity file to use. jwtToEnrol is a JWT the operator handed that
// must be enrolled into an identity before the room can dial; it is empty unless
// that is the case, and the caller is responsible for the enrolment.
//
// With no transport flag the token decides, exactly as it did before this
// existed, so a string minted by a hub that still encodes the transport keeps
// working unchanged.
func (f joinFlags) resolve(j link.Join) (out link.Join, identity, jwtToEnrol string, err error) {
	out = j
	pick, err := f.chosen()
	if err != nil {
		return out, "", "", err
	}
	// A legacy --identity with no new flag keeps the old ziti-by-token join
	// working: the token said ziti and this points at the .json.
	identity = strings.TrimSpace(f.identity)

	switch pick {
	case "":
		return out, identity, "", nil

	case "--mtls":
		if j.Transport != "" && j.Transport != "direct" {
			return out, "", "", conflictErr(pick, j.Transport)
		}
		out.Transport = "direct"
		if addr := hostPort(f.mtls); addr != "" {
			out.Addr = addr
		}
		// The token carries the one-time secret and the CA pin; --mtls only says
		// where. A token with neither is not a direct join token, and no URL can
		// supply what the hub has to have signed.
		if out.Addr == "" || out.Pin == "" || out.Secret == "" {
			return out, "", "", fmt.Errorf(
				"--mtls needs a direct join token. the token carries the one-time secret and " +
					"the hub's fingerprint, and --mtls only says where to reach the hub")
		}
		return out, "", "", nil

	case "--zrok-private":
		if j.Transport != "" && j.Transport != "zrok" {
			return out, "", "", conflictErr(pick, j.Transport)
		}
		out.Transport = "zrok"
		out.ShareToken = strings.TrimSpace(f.zrokPrivate)
		return out, "", "", nil

	case "--openziti":
		if j.Transport != "" && j.Transport != "ziti" {
			return out, "", "", conflictErr(pick, j.Transport)
		}
		out.Transport = "ziti"
		out.Service = firstNonEmpty(strings.TrimSpace(f.service), j.Service, "atrium")
		material := strings.TrimSpace(f.openziti)
		if isIdentityFile(material) {
			return out, material, "", nil
		}
		// A JWT, which has to be enrolled into an identity before a dial. The
		// caller does that and hands back the .json path. See the design's
		// follow-up "atrium2 join enrolling a JWT in place".
		return out, "", material, nil
	}
	return out, identity, "", nil
}

// conflictErr is the message when a flag names one transport and the token
// already carries another. Editing the token is the likely cause, so the
// sentence says both.
func conflictErr(flag, tokenTransport string) error {
	return fmt.Errorf("%s says one transport but this join string is for %q. "+
		"paste the string the hub printed, and pick the flag that matches it",
		flag, tokenTransport)
}

// hostPort takes what --mtls was given and returns a host:port, tolerating a
// url with a scheme so an operator who pasted `https://hub:7443` is not refused
// for being helpful.
func hostPort(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	return strings.TrimRight(s, "/")
}

// isIdentityFile is how --openziti tells a ready identity from a token to enrol.
// An enrolled OpenZiti identity is a `.json`; anything else is treated as a JWT.
func isIdentityFile(s string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(s)), ".json")
}

// firstNonEmpty returns the first argument that is not blank.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
