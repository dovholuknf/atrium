package daemon

import (
	"fmt"
	"strings"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/rest_client_zrok/share"
	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"
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

	root, err := d.zrokRoot()
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
		return "", zrokSays("could not reach the zrok api", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)

	create := share.NewCreateShareNameParams()
	create.Body = share.CreateShareNameBody{NamespaceToken: namespace, Name: name}
	if _, err := zrok.Share.CreateShareName(create, auth); err != nil {
		// Already existing is the ordinary case on the second press, and the
		// reservation below is what actually matters. Anything else is real.
		if !alreadyThere(err) {
			return "", zrokSays(fmt.Sprintf("could not create the name %q", name), err)
		}
	}

	reserve := share.NewUpdateShareNameParams()
	reserve.Body = share.UpdateShareNameBody{
		NamespaceToken: namespace, Name: name, Reserved: true,
	}
	if _, err := zrok.Share.UpdateShareName(reserve, auth); err != nil {
		return "", zrokSays(fmt.Sprintf("the name %q exists but could not be reserved", name), err)
	}

	// What the start path wants in its config, so the answer can be pasted
	// straight in rather than assembled by hand.
	return namespace + "/" + name, nil
}

// ReleaseZrokName gives a reserved name back.
//
// The other half of reserving, and the half that was missing. A reserved name
// is kept by the controller precisely so that unsharing does not delete it, so
// nothing on the ordinary path ever gets rid of one. An account that reserves
// a name per shared session and never releases any accumulates them for as
// long as it is used.
//
// A name that is already gone is NOT an error. Both callers are cleaning up,
// and there is exactly one state either of them wants: the name is not on the
// account. Reporting a failure for having arrived at that state early would
// leave the row behind and try again forever.
func (d *Daemon) ReleaseZrokName(namespace, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	namespace = strings.TrimSpace(namespace)

	root, err := d.zrokRoot()
	if err != nil {
		return fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		// Nothing to release against. Not an error for the same reason as
		// above: the name is not on an account this machine can reach.
		return nil
	}
	if namespace == "" {
		namespace, _ = root.DefaultNamespace()
		if namespace == "" {
			namespace = "public"
		}
	}

	zrok, err := root.Client()
	if err != nil {
		return zrokSays("could not reach the zrok api", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)

	del := share.NewDeleteShareNameParams()
	del.Body = share.DeleteShareNameBody{NamespaceToken: namespace, Name: name}
	if _, err := zrok.Share.DeleteShareName(del, auth); err != nil {
		if notThere(err) {
			return nil
		}
		return zrokSays(fmt.Sprintf("could not release the name %q", name), err)
	}
	return nil
}

// notThere reports whether an error means the thing was already gone.
//
// Matched on the message for the same reason as `alreadyThere`: the generated
// client has a type per status code and a type switch would name several and
// miss the one a later zrok adds.
func notThere(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "notfound") || strings.Contains(s, "not found") ||
		strings.Contains(s, "404")
}

// alreadyThere reports whether an error means the name was there before.
//
// Matched on the message rather than the type. The generated client returns a
// distinct type per status code, so a type switch here would name several and
// still miss whichever one a later zrok adds. A wrong answer costs one
// misleading error message, and the reservation that follows is what the
// caller is actually asking for.
//
// EXCEPT FOR THE NAME CEILING, which is the one 409 that must not be swallowed.
// zrok answers a refusal for the account's reserved name limit with the same
// status as a name that already exists, carrying `names limit reached`.
// Swallowed, it reaches the update, which refuses for the same reason and gets
// reported as a name collision, so somebody is told to try a longer name when
// no name of any length would have worked.
func alreadyThere(err error) bool {
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "limit reached") {
		return false
	}
	return strings.Contains(s, "conflict") || strings.Contains(s, "already")
}

// boardShareName is the reserved address the BOARD's public share answers on.
//
// THE BOARD'S SHARE IS ALWAYS RESERVED, whether or not anybody typed a name.
// This used to be three manual steps: type a name, press `reserve it`, press
// `start sharing`. Skipping any of them meant zrok invented an address, the
// board handed it out, and the controller deleted it at the next stop. The
// link was dead and nothing said so.
//
// The name is generated once and WRITTEN BACK TO THE CONFIG, which is what
// makes a restart land on the same address rather than on a fresh reservation
// nobody asked for. `newShareName` is sixty bits of randomness, so an address
// nobody chose is still not one anybody guesses.
//
// Reserved on every start rather than only when it is created, because the two
// failures are indistinguishable from here: a name that was never created and
// a name created ephemeral both end up gone after the first stop.
// `ReserveZrokName` is idempotent for exactly that reason.
// boardShareNameLen is how long an invented board address is.
//
// Eight rather than the twelve a lent session gets. This one is read off a
// screen and typed, and it is not the secret: the board behind it is either
// deliberately public or behind the sign-in. A lent session's address IS the
// credential, which is why the two differ.
const boardShareNameLen = 8

// parseShareName turns what is in the settings box into what zrok wants.
//
// IT IS A COLON, and getting that wrong broke public sharing outright.
// `zroksdk.ParseNameSelection` documents its input as
// `<namespaceToken>[:<name>]` and splits on a colon, so it put the WHOLE of
// `public/atrium-4pcddxxx9aez` into the namespace and left the name empty. The
// name then reached `ReserveZrokName`, which refused with "a reservation needs
// a name", and every attempt to start a public share failed on a config that
// looked correct in the box.
//
// A BARE NAME IS ALSO WRONG THROUGH THAT FUNCTION, and that is the half nobody
// would have found by reading. `atrium-4pcddxxx9aez` on its own parses as a
// NAMESPACE with no name, which is the same empty name by a shorter route. The
// code below it tried to cover this with `if sel.NamespaceToken == ""`, which
// can never fire: the parse always fills the namespace, because the namespace
// is the part it keeps.
//
// So three forms are accepted and all of them mean the same thing. A colon is
// zrok's own spelling, a slash is what atrium wrote into this box for months,
// and a bare name is what anybody types. The namespace defaults to `public`,
// which is the only one a board is shared in.
func parseShareName(s string) (zroksdk.NameSelection, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return zroksdk.NameSelection{}, fmt.Errorf("a share needs a name")
	}
	ns, name := publicNamespace, s
	for _, sep := range []string{":", "/"} {
		if i := strings.Index(s, sep); i >= 0 {
			ns, name = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
			break
		}
	}
	if ns == "" {
		ns = publicNamespace
	}
	if name == "" {
		return zroksdk.NameSelection{}, fmt.Errorf(
			"%q names a namespace and no share. it should be a name, or namespace:name", s)
	}
	return zroksdk.NameSelection{NamespaceToken: ns, Name: name}, nil
}

func (d *Daemon) boardShareName(cfg *ZrokConfig) (zroksdk.NameSelection, error) {
	var sel zroksdk.NameSelection

	if n := strings.TrimSpace(cfg.Name); n != "" {
		parsed, err := parseShareName(n)
		if err != nil {
			return sel, err
		}
		sel = parsed
	} else {
		name, err := newShareNameOf(boardShareNameLen)
		if err != nil {
			return sel, err
		}
		sel = zroksdk.NameSelection{NamespaceToken: publicNamespace, Name: name}
		// STORED BARE, without the namespace. `public/atrium-4pcddxxx9aez` is
		// what zrok wants and not what anybody wants to read in a settings box,
		// and the namespace is the default one anyway: the parse above fills it
		// in when it is missing, so the short form is complete.
		//
		// SAVED BEFORE IT IS RESERVED, on purpose. A name reserved on the
		// account and not written down here is a leak: nothing on this machine
		// knows it exists, so nothing ever releases it. Saved and not reserved
		// is the harmless direction, since the next start reserves it.
		cfg.Name = name
		if err := d.saveOverlayConfig(SettingOverlayZrok, *cfg); err != nil {
			return sel, fmt.Errorf("could not remember the address: %w", err)
		}
	}

	// The namespace is filled in by both paths above, so nothing defaults it
	// here. A guard that did used to sit at this spot and could never fire.
	d.overlayStep("zrok", "name", "reserving the address "+sel.Name)
	if _, err := d.ReserveZrokName(sel.NamespaceToken, sel.Name); err != nil {
		return sel, err
	}
	return sel, nil
}
