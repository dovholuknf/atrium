package gitsync

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cgi"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Backend serves git-upload-pack, and only that, out of repositories it is told about.
//
// It is `git http-backend` behind net/http/cgi, and nearly everything in this file is a
// refusal, because http-backend on its own will do far more than fetch:
//
//   - RECEIVE-PACK is off (http.receivepack=false), and refused here as well.
//   - THE DUMB PROTOCOL is off (http.getanyfile=false), and refused here as well. With
//     GIT_HTTP_EXPORT_ALL, http-backend otherwise serves objects/info/packs and loose objects
//     by path, which ignore hideRefs entirely.
//   - THE ARCHIVE SERVICE is off (http.uploadarch=false).
//   - PROTOCOL v2 IS NOT SPOKEN. The Git-Protocol header is dropped, so upload-pack answers
//     in v0, where a want that is not an advertised tip is refused. Protocol v2 is looser
//     about wants and nothing here relies on it.
//
// ALL SERVER CONFIG RIDES THE CGI ENVIRONMENT (GIT_CONFIG_COUNT and friends) and none of
// it is ever written into a repository, so a repository carries no policy of ours.
type Backend struct {
	// Prefix is the URL path in front of `<name>.git/`, with no trailing slash. Empty for
	// the hub, which serves on its own connection, and `/v1/git` for a room.
	Prefix string
	// Resolve turns a repository name into the git directory to serve. False is a 404. The
	// name has already been checked to hold no `..`, no backslash and no drive letter.
	Resolve func(name string) (gitDir string, ok bool)
	// Hide is each uploadpack.hideRefs value, in order.
	Hide []string
	// HideFor, when set, is the hideRefs for one request and replaces Hide. It is asked for the repository name and
	// its git directory, and the answer is written into the CGI environment for that request alone.
	HideFor func(name, gitDir string) []string
	// Git is the git executable. Empty finds `git` on PATH.
	Git string
}

// inherited is what the CGI child is given from this process. net/http/cgi passes a short
// platform list and NOT PATH, and git-http-backend has to find git-upload-pack. No GIT_*
// variable is ever on this list.
var inherited = []string{
	"PATH", "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "TEMP", "TMP", "TMPDIR",
	"SystemRoot", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT",
	"LOCALAPPDATA", "APPDATA", "PROGRAMDATA", "ProgramFiles", "ProgramFiles(x86)",
}

// maxRequest bounds an upload-pack request body, which is a list of wants and haves.
const maxRequest = 64 << 20

const (
	svcRefs = "/info/refs"
	svcPack = "/git-upload-pack"
)

// ValidName says whether a repository name is safe to look up at all: slash separated
// segments, none empty, `.` or `..`, and no backslash, colon (a drive letter), NUL or
// percent. It is a check before a lookup, and the lookup is against a list.
func ValidName(name string) bool {
	if name == "" || len(name) > 300 || strings.HasPrefix(name, "/") {
		return false
	}
	if strings.ContainsAny(name, "\\:%\x00") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The raw path as well: a `%2e%2e` that some layer decodes later is still refused.
	if strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2e") ||
		strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%5c") ||
		strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") {
		http.NotFound(w, r)
		return
	}
	p := r.URL.Path
	if b.Prefix != "" {
		if !strings.HasPrefix(p, b.Prefix+"/") {
			http.NotFound(w, r)
			return
		}
		p = strings.TrimPrefix(p, b.Prefix)
	}
	p = strings.TrimPrefix(p, "/")

	var svc string
	switch {
	case strings.HasSuffix(p, svcRefs):
		svc = svcRefs
	case strings.HasSuffix(p, svcPack):
		svc = svcPack
	case strings.HasSuffix(p, "/git-receive-pack"):
		http.Error(w, "this serves fetch and nothing else", http.StatusForbidden)
		return
	default:
		// The dumb protocol, HEAD, objects/..., anything else.
		http.NotFound(w, r)
		return
	}
	repo := strings.TrimSuffix(p, svc)
	if !strings.HasSuffix(repo, ".git") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSuffix(repo, ".git")
	if !ValidName(name) || b.Resolve == nil {
		http.NotFound(w, r)
		return
	}
	dir, ok := b.Resolve(name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch svc {
	case svcRefs:
		// Without the service query this is the dumb protocol's ref list.
		if r.Method != http.MethodGet || r.URL.Query().Get("service") != "git-upload-pack" {
			http.Error(w, "this serves fetch and nothing else", http.StatusForbidden)
			return
		}
	case svcPack:
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-git-upload-pack-request" {
			http.Error(w, "this serves fetch and nothing else", http.StatusForbidden)
			return
		}
	}

	exe := b.Git
	if exe == "" {
		var err error
		if exe, err = exec.LookPath("git"); err != nil {
			http.Error(w, "git is not installed on this machine", http.StatusServiceUnavailable)
			return
		}
	}
	dir = filepath.Clean(dir)
	cfg := [][2]string{
		{"http.receivepack", "false"},
		{"http.getanyfile", "false"},
		{"http.uploadarch", "false"},
		// A want is an advertised tip and nothing else, and no partial clone.
		{"uploadpack.allowFilter", "false"},
		{"uploadpack.allowAnySHA1InWant", "false"},
		{"uploadpack.allowTipSHA1InWant", "false"},
		{"uploadpack.allowReachableSHA1InWant", "false"},
	}
	hide := b.Hide
	if b.HideFor != nil {
		hide = b.HideFor(name, dir)
	}
	for _, h := range hide {
		cfg = append(cfg, [2]string{"uploadpack.hideRefs", h})
	}

	// PROTOCOL v0: the header never reaches the CGI, so HTTP_GIT_PROTOCOL is never set.
	out := r.Clone(r.Context())
	out.Header.Del("Git-Protocol")
	// Rewritten to the one directory, so PATH_INFO names nothing but it.
	out.URL.Path = "/" + filepath.Base(dir) + svc
	out.URL.RawPath = ""
	// A CHUNKED BODY IS MADE INTO ONE WITH A LENGTH. A client whose want list is past its
	// post buffer sends chunked, and net/http/cgi sets no CONTENT_LENGTH for that, which
	// http-backend reads as a truncated request and answers 400. The body is a want list,
	// bounded here, and never a pack.
	if out.ContentLength < 0 || len(out.TransferEncoding) > 0 {
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequest+1))
		if err != nil || len(raw) > maxRequest {
			http.Error(w, "that request is too large", http.StatusRequestEntityTooLarge)
			return
		}
		out.Body = io.NopCloser(bytes.NewReader(raw))
		out.ContentLength = int64(len(raw))
		out.TransferEncoding = nil
		out.Header.Del("Transfer-Encoding")
		out.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	}

	gitCGI(exe, dir, cfg, cgiEnv()).ServeHTTP(w, out)
}

// gitCGI is `git http-backend` for one repository directory. The request it is given has its path
// rewritten to `/<dir name>/<service>`. Every config value rides the environment, and nothing is written to
// a repository. `more` is further environment, such as GIT_CONFIG_GLOBAL.
func gitCGI(exe, dir string, cfg [][2]string, more []string) *cgi.Handler {
	env := []string{
		"GIT_PROJECT_ROOT=" + filepath.Dir(dir),
		"GIT_HTTP_EXPORT_ALL=1",
	}
	env = append(env, more...)
	// The global config is hidden, and with it the operator's safe.directory, so a clone owned by another
	// account is refused as dubious before a CGI header is written. The directory was resolved by the
	// caller from its own list, so it is trusted here and nowhere else.
	cfg = append(cfg[:len(cfg):len(cfg)], [2]string{"safe.directory", filepath.ToSlash(dir)})
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(cfg)))
	for i, kv := range cfg {
		env = append(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+kv[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+kv[1])
	}
	return &cgi.Handler{
		Path:       exe,
		Args:       []string{"http-backend"},
		Root:       "",
		Env:        env,
		InheritEnv: inherited,
	}
}
