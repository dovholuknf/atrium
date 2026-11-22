// Package dictate keeps one whisper.cpp `whisper-server` warm on this room and asks it to turn a spoken clip into
// text. Audio goes only to that local process, never to a hosted service. It imports nothing from the daemon.
package dictate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// ErrOff is a room with no whisper-server or model set.
var ErrOff = errors.New("dictation is not set up on this room: set dictate_server and dictate_model")

// Engine is one supervised whisper-server. Conf is read on every call, so a changed setting restarts it.
type Engine struct {
	Conf func() (bin, model string)
	// URL is a shared whisper-server. When it answers one, clips go there and nothing is started.
	URL func() string

	mu      sync.Mutex
	bin     string
	model   string
	cmd     *exec.Cmd
	tree    *procTree
	base    string
	exited  chan struct{}
	logTail *tail
}

// Warm starts the server if it is configured and not running, and waits until it answers.
func (e *Engine) Warm(ctx context.Context) error {
	_, err := e.ready(ctx)
	return err
}

// Transcribe sends one WAV clip with the word list as whisper's initial prompt and answers the raw text.
func (e *Engine) Transcribe(ctx context.Context, wav []byte, prompt string) (string, error) {
	base, err := e.ready(ctx)
	if err != nil {
		return "", err
	}
	return ask(ctx, base, wav, prompt)
}

// Stop ends the server and everything it started.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
}

func (e *Engine) stopLocked() {
	if e.cmd == nil {
		return
	}
	if e.tree != nil {
		_ = e.tree.kill()
		e.tree.close()
	} else if e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	<-e.exited
	e.cmd, e.tree, e.base, e.exited = nil, nil, "", nil
}

func (e *Engine) ready(ctx context.Context) (string, error) {
	if e.URL != nil {
		if u := Base(e.URL()); u != "" {
			e.Stop()
			return u, nil
		}
	}
	bin, model := "", ""
	if e.Conf != nil {
		bin, model = e.Conf()
	}
	if bin == "" || model == "" {
		e.Stop()
		return "", ErrOff
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil {
		select {
		case <-e.exited:
			e.cmd, e.tree, e.base, e.exited = nil, nil, "", nil
		default:
			if e.bin == bin && e.model == model {
				return e.base, nil
			}
			e.stopLocked()
		}
	}
	if err := e.startLocked(bin, model); err != nil {
		return "", err
	}
	if err := e.waitUp(ctx); err != nil {
		e.stopLocked()
		return "", err
	}
	return e.base, nil
}

func (e *Engine) startLocked(bin, model string) error {
	port, err := freePort()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "-m", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port),
		"-t", strconv.Itoa(min(runtime.NumCPU(), 8)))
	e.logTail = &tail{}
	cmd.Stdout, cmd.Stderr = e.logTail, e.logTail
	nowindow.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	e.cmd, e.tree, e.exited = cmd, startTree(cmd), exited
	e.bin, e.model = bin, model
	e.base = "http://127.0.0.1:" + strconv.Itoa(port)
	return nil
}

// waitUp polls until the server answers. Loading large-v3-turbo onto a GPU takes a few seconds.
func (e *Engine) waitUp(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.base+"/", nil)
		if res, err := http.DefaultClient.Do(req); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-e.exited:
			return fmt.Errorf("whisper-server exited: %s", e.logTail.String())
		case <-ctx.Done():
			return fmt.Errorf("whisper-server did not come up: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func ask(ctx context.Context, base string, wav []byte, prompt string) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("response_format", "json")
	_ = mw.WriteField("temperature", "0")
	if prompt != "" {
		_ = mw.WriteField("prompt", prompt)
	}
	// The clip goes into the concrete buffer, not an io.Writer part, so it is not a flow into every Write in the program.
	body.WriteString("\r\n--" + mw.Boundary() + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"clip.wav\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n")
	body.Write(wav)
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/inference", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || res.StatusCode != http.StatusOK || out.Error != "" {
		if out.Error == "" {
			out.Error = string(bytes.TrimSpace(raw))
		}
		return "", fmt.Errorf("whisper-server %d: %s", res.StatusCode, out.Error)
	}
	return out.Text, nil
}

// Base is a dictate_url trimmed to the server's root, so a pasted ".../inference" still works. Empty when unset.
func Base(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	return strings.TrimSuffix(u, "/inference")
}

// CheckURL says why a dictate_url is not acceptable. Plain http to a host that is not this machine needs confirm.
func CheckURL(raw string, confirm bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("the speech to text url must start with http:// or https:// and name a host")
	}
	if u.Scheme == "http" && !confirm {
		if ip := net.ParseIP(u.Hostname()); !(u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
			return fmt.Errorf("%s is plain http, so the audio crosses the network unencrypted: confirm to use it", u.Hostname())
		}
	}
	return nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// tail keeps the last few KB the server printed, for the error when it will not start.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.TrimSpace(t.buf))
}
