package gitsync

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

// cgiHandler runs `git http-backend` as a CGI, the way net/http/cgi does, with one difference that is the
// reason it exists: the child is built here, so it goes through nowindow.Hide.
//
// WHY NOT net/http/cgi. Its Handler makes the exec.Cmd itself and offers no hook for SysProcAttr. The hub
// and the rooms have no console, so every fetch they served started a git that opened a console window on
// the operator's desktop, and the hub serves the rooms' fetches all day.
//
// It is net/http/cgi's host side cut down to what git needs: no Root (PATH_INFO is the whole path), no
// internal redirect, and a chunked body is refused as there, which every caller already turns into one with
// a length. The environment, the header parsing and the answers to a bad CGI are the same.
type cgiHandler struct {
	Path       string   // the git executable
	Args       []string // `http-backend`
	Env        []string // extra environment, as "key=value"
	InheritEnv []string // environment variables taken from this process, by name
}

// cgiDefaultEnv is what net/http/cgi always takes from the host on Windows. Elsewhere it is a loader
// path, which git does not need.
var cgiDefaultEnv = []string{"SystemRoot", "COMSPEC", "PATHEXT", "WINDIR"}

var trailingPort = regexp.MustCompile(`:([0-9]+)$`)

// cgiStarted is told each child ServeHTTP started. Nil except in a test.
var cgiStarted func(*exec.Cmd)

// command is the child for one request, before its streams are set.
func (h *cgiHandler) command(req *http.Request) *exec.Cmd {
	cmd := exec.Command(h.Path, h.Args...)
	// Run in git's own directory, as net/http/cgi does.
	if filepath.IsAbs(h.Path) {
		cmd.Dir = filepath.Dir(h.Path)
	}
	cmd.Env = h.env(req)
	nowindow.Hide(cmd)
	return cmd
}

func (h *cgiHandler) env(req *http.Request) []string {
	port := "80"
	if req.TLS != nil {
		port = "443"
	}
	if m := trailingPort.FindStringSubmatch(req.Host); m != nil {
		port = m[1]
	}
	env := []string{
		"SERVER_SOFTWARE=go",
		"SERVER_PROTOCOL=HTTP/1.1",
		"HTTP_HOST=" + req.Host,
		"GATEWAY_INTERFACE=CGI/1.1",
		"REQUEST_METHOD=" + req.Method,
		"QUERY_STRING=" + req.URL.RawQuery,
		"REQUEST_URI=" + req.URL.RequestURI(),
		"PATH_INFO=" + req.URL.Path,
		"SCRIPT_NAME=",
		"SCRIPT_FILENAME=" + h.Path,
		"SERVER_PORT=" + port,
	}
	if ip, p, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		env = append(env, "REMOTE_ADDR="+ip, "REMOTE_HOST="+ip, "REMOTE_PORT="+p)
	} else {
		env = append(env, "REMOTE_ADDR="+req.RemoteAddr, "REMOTE_HOST="+req.RemoteAddr)
	}
	if host, _, err := net.SplitHostPort(req.Host); err == nil {
		env = append(env, "SERVER_NAME="+host)
	} else {
		env = append(env, "SERVER_NAME="+req.Host)
	}
	if req.TLS != nil {
		env = append(env, "HTTPS=on")
	}
	for k, v := range req.Header {
		k = strings.Map(cgiHeaderName, k)
		if k == "PROXY" { // httpoxy
			continue
		}
		join := ", "
		if k == "COOKIE" {
			join = "; "
		}
		env = append(env, "HTTP_"+k+"="+strings.Join(v, join))
	}
	if req.ContentLength > 0 {
		env = append(env, "CONTENT_LENGTH="+strconv.FormatInt(req.ContentLength, 10))
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		env = append(env, "CONTENT_TYPE="+ct)
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/bin:/usr/bin:/usr/ucb:/usr/bsd:/usr/local/bin"
	}
	env = append(env, "PATH="+path)
	for _, e := range h.InheritEnv {
		if v := os.Getenv(e); v != "" {
			env = append(env, e+"="+v)
		}
	}
	for _, e := range cgiDefaultEnv {
		if v := os.Getenv(e); v != "" {
			env = append(env, e+"="+v)
		}
	}
	env = append(env, h.Env...)
	return lastWins(env)
}

// lastWins drops every entry a later one of the same name overrides, so Env can override what came before.
func lastWins(env []string) []string {
	var out []string
	for i, e := range env {
		over := false
		if eq := strings.IndexByte(e, '='); eq != -1 {
			for _, later := range env[i+1:] {
				if strings.HasPrefix(later, e[:eq+1]) {
					over = true
					break
				}
			}
		}
		if !over {
			out = append(out, e)
		}
	}
	return out
}

func cgiHeaderName(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z':
		return r - ('a' - 'A')
	case r == '-', r == '=':
		return '_'
	}
	return r
}

func (h *cgiHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if len(req.TransferEncoding) > 0 && req.TransferEncoding[0] == "chunked" {
		rw.WriteHeader(http.StatusBadRequest)
		rw.Write([]byte("Chunked request bodies are not supported by CGI."))
		return
	}
	cmd := h.command(req)
	cmd.Stderr = os.Stderr
	if req.ContentLength != 0 {
		cmd.Stdin = req.Body
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Printf("CGI error: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Printf("CGI error: %v", err)
		return
	}
	if hook := cgiStarted; hook != nil {
		hook(cmd)
	}
	defer cmd.Wait()
	defer stdout.Close()

	body := bufio.NewReaderSize(stdout, 1024)
	headers := make(http.Header)
	status, lines, blank := 0, 0, false
	for {
		line, isPrefix, err := body.ReadLine()
		if isPrefix {
			rw.WriteHeader(http.StatusInternalServerError)
			log.Printf("cgi: long header line from subprocess.")
			return
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			log.Printf("cgi: error reading headers: %v", err)
			return
		}
		if len(line) == 0 {
			blank = true
			break
		}
		lines++
		name, val, ok := strings.Cut(string(line), ":")
		if !ok {
			log.Printf("cgi: bogus header line: %s", line)
			continue
		}
		if !httpguts.ValidHeaderFieldName(name) {
			log.Printf("cgi: invalid header name: %q", name)
			continue
		}
		val = textproto.TrimString(val)
		if name != "Status" {
			headers.Add(name, val)
			continue
		}
		if len(val) < 3 {
			log.Printf("cgi: bogus status (short): %q", val)
			return
		}
		code, err := strconv.Atoi(val[0:3])
		if err != nil {
			log.Printf("cgi: bogus status: %q", val)
			return
		}
		status = code
	}
	if lines == 0 || !blank {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Printf("cgi: no headers")
		return
	}
	if headers.Get("Location") != "" && status == 0 {
		status = http.StatusFound
	}
	if status == 0 && headers.Get("Content-Type") == "" {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Printf("cgi: missing required Content-Type in headers")
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	for k, vv := range headers {
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(status)
	if _, err := io.Copy(rw, body); err != nil {
		log.Printf("cgi: copy error: %v", err)
		// The client went away, or the child died. Either way the deferred Wait must not hang on it.
		cmd.Process.Kill()
	}
}
