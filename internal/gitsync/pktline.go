package gitsync

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Just enough of git's pkt-line framing to read a receive-pack command list and an upload-pack request
// before they reach git, and to answer a refused push the way receive-pack would.

var errPkt = errors.New("that is not a git request")

// readPkt reads one pkt-line from b. flush is true for 0000 (and 0001, 0002 are treated as flush too, which
// only protocol v2 sends and which is never spoken here). rest is what follows.
func readPkt(b []byte) (data []byte, flush bool, rest []byte, err error) {
	if len(b) < 4 {
		return nil, false, nil, errPkt
	}
	n, err := strconv.ParseUint(string(b[:4]), 16, 32)
	if err != nil {
		return nil, false, nil, errPkt
	}
	switch {
	case n == 0:
		return nil, true, b[4:], nil
	case n < 4:
		return nil, true, b[4:], errPkt
	case int(n) > len(b) || n > 65520:
		return nil, false, nil, errPkt
	}
	return b[4:n], false, b[n:], nil
}

// pktLine is one pkt-line with data.
func pktLine(s string) []byte { return []byte(fmt.Sprintf("%04x%s", len(s)+4, s)) }

// flushPkt is 0000.
var flushPkt = []byte("0000")

// Update is one command of a push: old and new sha, and the ref.
type Update struct {
	Old, New, Ref string
}

const zeroSHA = "0000000000000000000000000000000000000000"

func isHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// pushRequest is what a receive-pack request says before its pack.
type pushRequest struct {
	Updates []Update
	Caps    []string
	// Pack is where the pack starts in the body.
	Pack int
	// Why is a sentence when the request is one the hub will not take whatever the rules say.
	Why string
}

func (p pushRequest) has(cap string) bool {
	for _, c := range p.Caps {
		if c == cap || strings.HasPrefix(c, cap+"=") {
			return true
		}
	}
	return false
}

// parsePush reads the command list at the front of a receive-pack body: pkt-lines up to the first flush.
func parsePush(body []byte) (pushRequest, error) {
	var req pushRequest
	rest := body
	for {
		data, flush, r, err := readPkt(rest)
		if err != nil {
			return req, err
		}
		rest = r
		if flush {
			break
		}
		line := strings.TrimSuffix(string(data), "\n")
		// A feature list rides after a NUL on ANY command line, as git's read_head_info reads it, and the checks
		// below are made on all of them: the hub's view of a push has to be git's.
		if i := strings.IndexByte(line, 0); i >= 0 {
			req.Caps = append(req.Caps, strings.Fields(line[i+1:])...)
			line = line[:i]
		}
		switch {
		case strings.HasPrefix(line, "shallow "):
			req.Why = "a push from a shallow clone is not taken. fetch the rest of the history first"
			continue
		case strings.HasPrefix(line, "push-cert"):
			req.Why = "signed pushes are not taken"
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 || !isHex40(f[0]) || !isHex40(f[1]) || f[2] == "" {
			return req, errPkt
		}
		req.Updates = append(req.Updates, Update{Old: f[0], New: f[1], Ref: f[2]})
		if len(req.Updates) > 1000 {
			req.Why = "a push of more than a thousand refs is not taken"
			break
		}
	}
	req.Pack = len(body) - len(rest)
	if len(req.Updates) == 0 && req.Why == "" {
		return req, errPkt
	}
	for _, c := range req.Caps {
		switch {
		case c == "push-options":
			req.Why = "push options are not taken here"
		case strings.HasPrefix(c, "object-format=") && c != "object-format=sha1":
			req.Why = "this hub's repositories are sha1"
		}
	}
	return req, nil
}

// fetchRefusal reads an upload-pack request and answers a sentence when it asks for something stage 1
// does not serve: a shallow clone, a deepening, or a filter. "" is a plain fetch.
func fetchRefusal(body []byte) (string, error) {
	rest := body
	for len(rest) > 0 {
		data, flush, r, err := readPkt(rest)
		if err != nil {
			return "", err
		}
		rest = r
		if flush {
			continue
		}
		line := strings.TrimSuffix(string(data), "\n")
		if strings.HasPrefix(line, "want ") {
			// The first want carries the client's capabilities after the sha.
			f := strings.Fields(line)
			for _, c := range f[min(2, len(f)):] {
				if c == "filter" || c == "deepen-relative" {
					return "this hub serves whole fetches only, with no filter or depth", nil
				}
			}
			continue
		}
		for _, bad := range []string{"shallow ", "deepen", "filter "} {
			if strings.HasPrefix(line, bad) {
				return "this hub serves whole fetches only, with no filter or depth", nil
			}
		}
	}
	return "", nil
}

// refusedPush is the body receive-pack would send for a push it refuses, so the user's git prints the
// sentence after `remote: ` and `! [remote rejected]` with a reason for each ref. NOTHING WAS MOVED.
//
// `why` is the sentence for the ref that broke a rule and `others` for the refs that were fine on their
// own, so the user can see the whole push was refused.
func refusedPush(req pushRequest, bad map[string]string, whole string) []byte {
	var report bytes.Buffer
	report.Write(pktLine("unpack ok\n"))
	for _, u := range req.Updates {
		reason := bad[u.Ref]
		if reason == "" {
			reason = "not pushed: another ref in the same push was refused"
		}
		report.Write(pktLine("ng " + u.Ref + " " + oneLine(reason) + "\n"))
	}
	report.Write(flushPkt)

	var out bytes.Buffer
	if req.has("side-band-64k") || req.has("side-band") {
		out.Write(sideband(2, "atrium: "+oneLine(whole)+"\n"))
		out.Write(sideband(1, report.String()))
		out.Write(flushPkt)
		return out.Bytes()
	}
	return report.Bytes()
}

// sideband frames msg on a band. Messages here are short, so there is no splitting.
func sideband(band byte, msg string) []byte {
	return pktLine(string([]byte{band}) + msg)
}

// oneLine keeps a sentence on one line, which a report-status line has to be.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\x00", " ")), " ")
}

// refusedAdvert is a ref advertisement that is only an error, which git prints as `fatal: remote error:`.
// For a refusal that comes before any push, where there is no report-status to put it in.
func refusedAdvert(service, why string) []byte {
	var out bytes.Buffer
	out.Write(pktLine("# service=" + service + "\n"))
	out.Write(flushPkt)
	out.Write(pktLine("ERR atrium: " + oneLine(why) + "\n"))
	return out.Bytes()
}
