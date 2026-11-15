package zitijoin

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// JoinURL is what `atrium room join` is given for an OpenZiti network: ziti://<controller>/<service>, with room=<the
// name the hub gave the room>, signer=<the IdP's name>, and jwt=<a one-use enrollment token> when the hub minted one.
type JoinURL struct {
	Controller, Service, Room, Signer, JWT string
}

// IsJoinURL says whether a join argument is this form.
func IsJoinURL(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "ziti://") }

// ParseJoinURL reads one.
func ParseJoinURL(s string) (JoinURL, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "ziti" || u.Host == "" {
		return JoinURL{}, errors.New("that is not a ziti join URL. it looks like ziti://controller:1280/service?room=name")
	}
	q := u.Query()
	j := JoinURL{
		Controller: u.Host, Service: strings.Trim(u.Path, "/"),
		Room: q.Get("room"), Signer: q.Get("signer"), JWT: q.Get("jwt"),
	}
	if j.Service == "" || j.Room == "" {
		return j, errors.New("that ziti join URL names no service or no room")
	}
	return j, nil
}

// Join gets this room an identity on the network. A one-use JWT enrolls without asking. Otherwise the room signs in
// at the network's IdP by the device flow, with show saying where, and enrolls to cert. It answers the identity's path.
func Join(ctx context.Context, p Params, j JoinURL, show func(Prompt)) (string, error) {
	p.Controller = j.Controller
	if p.Name == "" {
		p.Name = j.Room
	}
	if j.JWT != "" {
		return EnrollJWT(p, j.JWT)
	}
	signers, err := Signers(ctx, p)
	if err != nil {
		return "", err
	}
	var pick *Signer
	for i := range signers {
		if j.Signer == "" || signers[i].Name == j.Signer {
			pick = &signers[i]
			break
		}
	}
	if pick == nil {
		return "", errors.New("the controller lists no sign-in called " + j.Signer)
	}
	issuer := pick.Issuer
	if issuer == "" {
		issuer = pick.ExternalAuthURL
	}
	tok, err := DeviceFlow(ctx, nil, issuer, pick.ClientID, pick.Scopes, show)
	if err != nil {
		return "", err
	}
	return EnrollToCert(ctx, p, tok)
}
