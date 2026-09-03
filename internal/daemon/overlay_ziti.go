package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Turning a JWT into an identity file the tunneler can use.
//
// An enrolment token is one use and short lived. It arrives by mail or from a
// console, and until now atrium asked for the identity file that comes out the
// far side without saying anything about how to get one, which is the step
// somebody is actually stuck on.
//
// Atrium runs `ziti enroll` and keeps the path it wrote. It never generates a
// key, never talks to a controller, and never opens the identity afterwards.

// zitiIdentityDir is where atrium puts an identity it enrolled.
//
// Beside the database rather than in the zrok or ziti directories: this one is
// atrium's, created by atrium, and deleting atrium's state should take it with
// it rather than leaving a credential behind in somebody else's folder.
func (d *Daemon) zitiIdentityDir() string {
	return filepath.ToSlash(filepath.Join(filepath.Dir(d.opts.DBPath), "identities"))
}

// ZitiEnrolment is what atrium can tell about the identity it was pointed at.
type ZitiEnrolment struct {
	// Path is the identity file, as configured.
	Path string `json:"path,omitempty"`
	// Present is whether that file is actually there. Configured and missing
	// is the common state after a machine is rebuilt, and it is worth telling
	// apart from never configured.
	Present bool `json:"present"`
	// Controller is the address the identity belongs to, read out of the file
	// so the board can say which network this is. Never the key.
	Controller string `json:"controller,omitempty"`
	// Err is why the file could not be read, when it is there and unreadable.
	Err string `json:"err,omitempty"`
}

// zitiIdentityFile is the part of an identity atrium looks at. The key
// material lives beside these and is deliberately not named here: a field that
// does not exist cannot be logged by accident.
type zitiIdentityFile struct {
	ZtAPI  string   `json:"ztAPI"`
	ZtAPIs []string `json:"ztAPIs"`
}

// zitiEnrolment reports on the configured identity without holding its
// contents.
func zitiEnrolment(path string) ZitiEnrolment {
	p := strings.TrimSpace(path)
	if p == "" {
		return ZitiEnrolment{}
	}
	out := ZitiEnrolment{Path: p}
	raw, err := os.ReadFile(p)
	if err != nil {
		// Not being there is a state, not a fault, so it is reported as
		// absent rather than as an error somebody has to read.
		if !os.IsNotExist(err) {
			out.Err = err.Error()
		}
		return out
	}
	out.Present = true

	var f zitiIdentityFile
	if err := json.Unmarshal(raw, &f); err != nil {
		out.Err = "that file is not an identity atrium can read: " + err.Error()
		return out
	}
	out.Controller = f.ZtAPI
	if out.Controller == "" && len(f.ZtAPIs) > 0 {
		out.Controller = f.ZtAPIs[0]
	}
	return out
}

// JWTClaims is the readable half of an enrolment token.
//
// Shown before enrolling so somebody can see which network a token is for and
// whether it has already expired. A JWT's payload is base64, not encryption:
// reading it proves nothing and grants nothing, and the signature is the
// controller's to check.
type JWTClaims struct {
	Issuer  string `json:"issuer,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Expires is when the token stops working, RFC3339, empty when it says.
	Expires string `json:"expires,omitempty"`
	// Expired is the answer to the question somebody is actually asking.
	Expired bool `json:"expired"`
}

// readJWT pulls the claims out of an enrolment token.
//
// The signature is NOT checked. Atrium is not the party that validates this,
// the controller is, and pretending otherwise would mean carrying a trust
// store for a network atrium knows nothing about.
func readJWT(token string) (JWTClaims, error) {
	var out JWTClaims
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return out, fmt.Errorf("that does not look like an enrolment token")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, fmt.Errorf("the middle of that token is not readable: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return out, fmt.Errorf("the middle of that token is not json: %w", err)
	}
	out.Issuer, out.Subject = claims.Iss, claims.Sub
	if claims.Exp > 0 {
		t := time.Unix(claims.Exp, 0)
		out.Expires = t.Format(time.RFC3339)
		out.Expired = time.Now().After(t)
	}
	return out, nil
}

// zitiEnrollArgs builds the enrolment command.
//
// The token is written to a file rather than passed as an argument, because an
// argument is visible to anything on the machine that can list processes, and
// this one is a credential.
func zitiEnrollArgs(jwtPath, outPath string) []string {
	return []string{"enroll", "identity", "--jwt", jwtPath, "--out", outPath}
}
