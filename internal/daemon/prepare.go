package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// The environment dump is fenced, because it is not the only thing on stdout.
//
// The shell loads the operator's profile on purpose, and a profile may print
// anything: `Write-Host "docker -> ..."` in a child pwsh lands on stdout, and so
// does an `echo` in a login shell's .profile. The prepare command may print too.
// Reading all of stdout as the dump turned one line of profile chatter into a
// failed launch that said "invalid character 'd'".
//
// The fence is a nonce per call, so nothing printed before it can forge it. The
// script builds the fence line from pieces, so a shell echoing its own script
// back (set -v, a transcript) never prints the whole marker either.

// envFencePrefix and envFenceSuffix wrap the nonce. The script prints them
// joined with the nonce, and never writes the joined line out literally.
const (
	envFencePrefix = "<<atrium-env:"
	envFenceSuffix = ">>"
)

// maxStrayOutput bounds how much of what the shell printed goes into an error.
const maxStrayOutput = 300

// envNonce is the part of the fence nothing else can know in advance.
func envNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// fenced returns what the shell printed between the two fence lines for nonce.
//
// Missing fences say what was printed instead, since that is what the operator
// has to go and fix: a profile that failed, or a shell that never ran the dump.
func fenced(out []byte, nonce string) ([]byte, error) {
	marker := []byte(envFencePrefix + nonce + envFenceSuffix)
	start := bytes.Index(out, marker)
	if start < 0 {
		return nil, fmt.Errorf("the shell did not print the environment. It printed: %s",
			strayOutput(out))
	}
	body := out[start+len(marker):]
	end := bytes.Index(body, marker)
	if end < 0 {
		return nil, fmt.Errorf("the shell stopped part way through the environment. It printed: %s",
			strayOutput(out))
	}
	return body[:end], nil
}

// strayOutput is what went to stdout, bounded and quoted so an empty answer and
// a control character both read as what they are.
func strayOutput(out []byte) string {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return "nothing"
	}
	if len(out) > maxStrayOutput {
		return fmt.Sprintf("%q...", out[:maxStrayOutput])
	}
	return fmt.Sprintf("%q", out)
}
