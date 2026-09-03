package daemon

import (
	"fmt"
	"strings"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/rest_client_zrok/share"
)

// Reserving an address, so the link you gave somebody still works tomorrow.
//
// This took reading zrok's own source to get right, and the shape is not what
// the SDK suggests. `sdk.ShareRequest` carries a `Reserved` field and NOTHING
// IN ZROK READS IT. Setting it, which atrium used to do, changed nothing.
//
// In v2 reserving moved off the share and onto the NAME. Three separate facts,
// and an address that survives a restart needs all three:
//
//  1. The name exists. `zrok2 create name <name> -n <namespace>`, which is
//     `CreateShareName` here.
//  2. The name is reserved rather than ephemeral. `zrok2 modify name <name>
//     --reserved`, which is `UpdateShareName`. Without this the controller
//     deletes the name when the share is unshared, in
//     `cleanupShareNameMappings`: it keeps reserved names and drops the rest.
//  3. A share asks for that name when it starts, which is the `NameSelections`
//     the start path already sends.
//
// A private share reaches the same place by a different route and needs
// nothing here. Its token is requested rather than owned, and deleting the
// share puts the token back on the shelf, so the next start asks for the same
// one and gets it back as long as nobody else took it in between.

// ReserveZrokName creates a name if it is not there and marks it reserved.
//
// Both steps every time, because the two failures look identical from outside:
// a name that was never created and a name created ephemeral both end up gone
// after the first stop. Creating one that exists is not an error worth
// reporting, so a conflict on step one is carried into step two rather than
// returned.
func (d *Daemon) ReserveZrokName(namespace, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("a reservation needs a name")
	}
	namespace = strings.TrimSpace(namespace)

	root, err := environment.LoadRoot()
	if err != nil {
		return "", fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return "", fmt.Errorf("this machine is not enabled with zrok yet, so there is no account to reserve a name on")
	}
	if namespace == "" {
		// The environment's own default, which is what the CLI uses when no
		// namespace is given. The second return is where that value came
		// from, for a message, and is not needed here. `public` only as a
		// last resort, matching what zrok falls back to.
		namespace, _ = root.DefaultNamespace()
		if namespace == "" {
			namespace = "public"
		}
	}

	zrok, err := root.Client()
	if err != nil {
		return "", fmt.Errorf("could not reach the zrok api: %w", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)

	create := share.NewCreateShareNameParams()
	create.Body = share.CreateShareNameBody{NamespaceToken: namespace, Name: name}
	if _, err := zrok.Share.CreateShareName(create, auth); err != nil {
		// Already existing is the ordinary case on the second press, and the
		// reservation below is what actually matters. Anything else is real.
		if !alreadyThere(err) {
			return "", fmt.Errorf("could not create the name %q: %w", name, err)
		}
	}

	reserve := share.NewUpdateShareNameParams()
	reserve.Body = share.UpdateShareNameBody{
		NamespaceToken: namespace, Name: name, Reserved: true,
	}
	if _, err := zrok.Share.UpdateShareName(reserve, auth); err != nil {
		return "", fmt.Errorf("the name %q exists but could not be reserved: %w", name, err)
	}

	// What the start path wants in its config, so the answer can be pasted
	// straight in rather than assembled by hand.
	return namespace + "/" + name, nil
}

// alreadyThere reports whether an error means the name was there before.
//
// Matched on the message rather than the type. The generated client returns a
// distinct type per status code, so a type switch here would name several and
// still miss whichever one a later zrok adds. A wrong answer costs one
// misleading error message, and the reservation that follows is what the
// caller is actually asking for.
func alreadyThere(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "conflict") || strings.Contains(s, "already")
}
