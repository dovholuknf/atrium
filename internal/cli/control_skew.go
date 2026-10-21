package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
)

// buildID is what `/v1/health` says about a build: the code fingerprint with the board left out, and the commit.
type buildID struct {
	Code   string
	Commit string
}

// controlSkewNote says, in plain words, that the control exe is not the build its room runs. It answers "" when
// the two match or either side does not say what it is, so a dev build never warns.
//
// The tool schemas this server advertises are compiled in, and nothing updates an installed copy on its own, so
// an old one silently lacks every parameter added since.
func controlSkewNote(own, room buildID) string {
	switch {
	case own.Code != "" && room.Code != "":
		if own.Code == room.Code {
			return ""
		}
	case own.Commit != "" && room.Commit != "":
		if own.Commit == room.Commit {
			return ""
		}
	default:
		return ""
	}
	return fmt.Sprintf("atrium control: this atrium-control.exe (%s) is older than the room it talks to (%s). "+
		"Its tools may lack parameters the room reads, such as scratch. Replace it with the room's atrium build.",
		shortCommit(own.Commit), shortCommit(room.Commit))
}

func shortCommit(c string) string {
	if c == "" {
		return "unknown commit"
	}
	if len(c) > 12 {
		c = c[:12]
	}
	return c
}

// warnIfControlOlderThanRoom asks the local room once and writes the note to w, which is stderr: stdout is the MCP
// channel. A room that is not there, or does not answer, is not a warning.
func warnIfControlOlderThanRoom(w io.Writer) {
	loc, err := readLocation()
	if err != nil || strings.TrimSpace(loc.Board) == "" {
		return
	}
	res, err := (&http.Client{Timeout: 3 * time.Second}).Get(loc.Board + "/v1/health")
	if err != nil {
		return
	}
	defer res.Body.Close()
	var h struct {
		Code   string `json:"code"`
		Commit string `json:"commit"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&h) != nil {
		return
	}
	_, commit, _ := versionInfo()
	if note := controlSkewNote(buildID{Code: api.CodeID, Commit: commit}, buildID{Code: h.Code, Commit: h.Commit}); note != "" {
		fmt.Fprintln(w, note)
	}
}
