package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/cardproc"
	"github.com/dovholuknf/atrium/internal/nowindow"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CARD'S OWN ZITI OVERLAY: a throwaway network the card owns for a test, never the operator's network and never
// the board's share. Design: docs/rnd/card-lifecycle-design.md section 10, Interview Q8 and Q9. Item f-card-overlay.
//
//	POST /v1/tasks/{id}/overlay {action: up, name}                          a quickstart on two of the card's ports
//	POST /v1/tasks/{id}/overlay {action: identity, overlay, name, roles}    an identity, created and enrolled
//	POST /v1/tasks/{id}/overlay {action: tunnel, overlay, identity, mode}   a tunneler, host or proxy
//	POST /v1/tasks/{id}/overlay {action: down, overlay}                     stop it and delete its folder
//
// A card has as many as it wants. Each lives in <data>/cards/<card>/overlay/<name>, its PKI, its database and its
// identities with it, and every part is a row of the card's inventory: the overlay (its folder), its ports, the
// quickstart and each tunneler as a proc, each identity. Closing the card stops the tunnelers, then the quickstart
// (procs stop newest first), then deletes the folder. The sweep frees a row whose folder or file is gone.
//
// THE ROOM NEVER ELEVATES. A tunneler in tun mode needs admin or root, so it is refused with the command to ask clint
// to run in an elevated shell (Q9). Host and proxy mode need nothing.
//
// The admin password is made here and written to the overlay's folder, never to the inventory. Identities are made
// through the controller's management API, so no `ziti edge login` touches the operator's own ziti config.

// CardsDir is where a card's own state goes, <data>/cards. Set by the daemon.
var CardsDir string

// zitiBin is the seam a test replaces.
var zitiBin = func() (string, error) { return exec.LookPath("ziti") }

// overlayReadyWait bounds how long an up waits for its controller to answer.
var overlayReadyWait = 2 * time.Minute

// overlayName is a name a folder, an identity and a log may carry.
var overlayName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// overlayStateFile is in the overlay's folder: which rows of the card are its own, so a down frees only them.
const overlayStateFile = "atrium-overlay.json"

// overlayMu serialises changes to the overlays' state files.
var overlayMu sync.Mutex

type overlayState struct {
	Name       string `json:"name"`
	Controller string `json:"controller"`
	RouterPort int    `json:"router_port"`
	// Rows is the seqs of the card's rows this overlay made: ports, the quickstart, tunnelers, identities.
	Rows []int `json:"rows"`
}

type overlayRequest struct {
	Action   string   `json:"action"`
	Name     string   `json:"name"`
	Overlay  string   `json:"overlay"`
	Identity string   `json:"identity"`
	Roles    []string `json:"roles"`
	Mode     string   `json:"mode"`
	Services []string `json:"services"`
}

// POST /v1/tasks/{id}/overlay
func (s *Server) postOverlay(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in overlayRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(CardsDir) == "" {
		writeErr(w, http.StatusServiceUnavailable, errors.New("this room has no cards folder, so it cannot hold an overlay"))
		return
	}
	defer s.publishTaskID(t.ID)
	switch strings.TrimSpace(in.Action) {
	case "up":
		s.overlayUp(w, r.Context(), t, strings.TrimSpace(in.Name))
	case "identity":
		s.overlayIdentity(w, r.Context(), t, in)
	case "tunnel":
		s.overlayTunnel(w, t, in)
	case "down":
		s.overlayDown(w, t, strings.TrimSpace(in.Overlay))
	default:
		writeErr(w, http.StatusBadRequest, errors.New("action is up, identity, tunnel or down"))
	}
}

// overlayHome is where a card's overlay of that name lives, inside CardsDir.
func overlayHome(card, name string) (string, error) {
	root := filepath.Clean(CardsDir)
	return safepath.Contained(root, filepath.Join(root, card, "overlay", name))
}

// inCards is the path when it is under CardsDir, so a close may delete it, else "".
func inCards(path string) string {
	if strings.TrimSpace(CardsDir) == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	root := filepath.Clean(CardsDir)
	real, err := safepath.Contained(root, filepath.FromSlash(path))
	if err != nil || eqPath(real, root) {
		return ""
	}
	return real
}

func (s *Server) overlayUp(w http.ResponseWriter, ctx context.Context, t *store.Task, name string) {
	rows, err := s.st.Resources(t.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if name == "" {
		n := 1
		for _, r := range rows {
			if r.Kind == store.ResOverlay {
				n++
			}
		}
		name = "o" + strconv.Itoa(n)
	}
	if !overlayName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, errors.New("an overlay's name is lowercase letters, digits and dashes, up to 32"))
		return
	}
	bin, err := zitiBin()
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("ziti is not on this room's PATH, so it cannot start an overlay"))
		return
	}
	home, err := overlayHome(t.ID, name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := os.Stat(home); err == nil {
		writeErr(w, http.StatusConflict, fmt.Errorf("this card has an overlay called %s already", name))
		return
	}

	// The rows first, then the things: the ports, the overlay's folder, then the process.
	ports, _, err := s.takePorts(t.ID, 2)
	if err != nil {
		s.freeTaken(t.ID, ports, "the overlay did not start")
		writeErr(w, http.StatusConflict, err)
		return
	}
	ctrl := "https://127.0.0.1:" + strconv.Itoa(ports[0])
	row, err := s.st.AddResource(t.ID, store.ResOverlay, filepath.ToSlash(home), ctrl)
	if err != nil {
		s.freeTaken(t.ID, ports, "the overlay did not start")
		s.fail(w, err)
		return
	}
	st := &overlayState{Name: name, Controller: ctrl, RouterPort: ports[1]}
	for _, p := range ports {
		if seq := s.portSeq(t.ID, p); seq > 0 {
			st.Rows = append(st.Rows, seq)
		}
	}
	fail := func(code int, err error) {
		s.overlayFree(t.ID, home, st, err.Error())
		_ = s.st.FreeResource(t.ID, row.Seq, err.Error())
		writeErr(w, code, err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	pw, err := randomHex(16)
	if err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	pwFile := filepath.Join(home, "admin-password")
	if err := os.WriteFile(pwFile, []byte(pw), 0o600); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	logFile := filepath.Join(home, "quickstart.log")
	pid, seq, err := s.startOwned(t.ID, home, logFile, bin, "edge", "quickstart", "--home", home,
		"--ctrl-address", "127.0.0.1", "--ctrl-port", strconv.Itoa(ports[0]),
		"--router-address", "127.0.0.1", "--router-port", strconv.Itoa(ports[1]), "--password", pw)
	if err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	st.Rows = append(st.Rows, seq)
	if err := writeOverlayState(home, st); err != nil {
		fail(http.StatusInternalServerError, err)
		return
	}
	ready := waitController(ctx, home, ctrl, pid, overlayReadyWait) == nil
	out := map[string]any{"name": name, "home": filepath.ToSlash(home), "controller": ctrl,
		"router": "127.0.0.1:" + strconv.Itoa(ports[1]), "username": "admin",
		"password_file": filepath.ToSlash(pwFile), "log": filepath.ToSlash(logFile), "pid": pid, "ready": ready}
	if !ready {
		out["note"] = "the controller did not answer yet. read the log, and ask for an identity once it does"
	}
	writeJSON(w, http.StatusCreated, out)
}

// startOwned starts a process in its own group, its output to logFile, and records it on the card as a proc.
func (s *Server) startOwned(card, dir, logFile, bin string, args ...string) (pid, seq int, err error) {
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, 0, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, lf, lf
	nowindow.Hide(cmd)
	cardproc.OwnGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return 0, 0, err
	}
	go func() {
		_ = cmd.Wait()
		_ = lf.Close()
	}()
	pid = cmd.Process.Pid
	at, err := cardproc.StartTime(pid)
	if err != nil {
		return pid, 0, fmt.Errorf("%s exited at once: %s", filepath.Base(bin), tailOf(logFile))
	}
	row, err := s.st.AddResource(card, store.ResProc, strconv.Itoa(pid), at)
	if err != nil {
		_ = cardproc.StopTree(pid)
		return pid, 0, err
	}
	return pid, row.Seq, nil
}

// waitController waits for the overlay's admin to log in over its own CA, or for the quickstart to end. The controller
// answers a while before the quickstart has made its admin, so an answer alone is not ready.
func waitController(ctx context.Context, home, ctrl string, pid int, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if _, err := cardproc.StartTime(pid); errors.Is(err, cardproc.ErrGone) {
			return errors.New("the quickstart exited")
		}
		if c, err := overlayClient(home); err == nil {
			rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			_, err := overlayLogin(rctx, c, home, ctrl)
			cancel()
			if err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("the controller did not answer")
}

// overlayClient trusts the overlay's own root CA and nothing else.
func overlayClient(home string) (*http.Client, error) {
	pem, err := os.ReadFile(filepath.Join(home, "pki", "root-ca", "certs", "root-ca.cert"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("the overlay's root CA does not read")
	}
	return &http.Client{Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}, nil
}

// liveOverlay finds a live overlay of the card by name, or its only one when name is "".
func (s *Server) liveOverlay(card, name string) (*store.CardResource, error) {
	rows, err := s.st.Resources(card)
	if err != nil {
		return nil, err
	}
	var live []*store.CardResource
	for _, r := range rows {
		if r.Kind == store.ResOverlay && r.Live() && (name == "" || filepath.Base(filepath.FromSlash(r.Ref)) == name) {
			live = append(live, r)
		}
	}
	switch {
	case len(live) == 1:
		return live[0], nil
	case len(live) == 0 && name == "":
		return nil, errors.New("this card has no overlay. start one with action up")
	case len(live) == 0:
		return nil, fmt.Errorf("this card has no overlay called %s", name)
	}
	return nil, errors.New("this card has more than one overlay. say which")
}

func (s *Server) overlayIdentity(w http.ResponseWriter, ctx context.Context, t *store.Task, in overlayRequest) {
	ov, err := s.liveOverlay(t.ID, strings.TrimSpace(in.Overlay))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(in.Name)
	if !overlayName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, errors.New("an identity's name is lowercase letters, digits and dashes, up to 32"))
		return
	}
	bin, err := zitiBin()
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("ziti is not on this room's PATH"))
		return
	}
	home := filepath.FromSlash(ov.Ref)
	dir := filepath.Join(home, "identities")
	file := filepath.Join(dir, name+".json")
	if _, err := os.Stat(file); err == nil {
		writeErr(w, http.StatusConflict, fmt.Errorf("the overlay has an identity called %s already", name))
		return
	}
	row, err := s.st.AddResource(t.ID, store.ResIdentity, filepath.ToSlash(file), ov.Ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	id, err := makeIdentity(ctx, bin, home, ov.Detail, dir, name, in.Roles)
	if err != nil {
		_ = s.st.FreeResource(t.ID, row.Seq, err.Error())
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if err := addOverlayRows(home, row.Seq); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "id": id, "file": filepath.ToSlash(file),
		"overlay": filepath.Base(home)})
}

// makeIdentity creates an identity through the management API, then enrols it with `ziti edge enroll`.
func makeIdentity(ctx context.Context, bin, home, ctrl, dir, name string, roles []string) (string, error) {
	c, err := overlayClient(home)
	if err != nil {
		return "", fmt.Errorf("the overlay is not ready: %v", err)
	}
	token, err := overlayLogin(ctx, c, home, ctrl)
	if err != nil {
		return "", fmt.Errorf("the controller did not log in: %v", err)
	}
	if roles == nil {
		roles = []string{}
	}
	var made struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := mgmt(ctx, c, token, http.MethodPost, ctrl+"/edge/management/v1/identities",
		map[string]any{"name": name, "type": "Device", "isAdmin": false, "enrollment": map[string]bool{"ott": true},
			"roleAttributes": roles}, &made); err != nil {
		return "", fmt.Errorf("the identity was not made: %v", err)
	}
	var got struct {
		Data struct {
			Enrollment struct {
				OTT struct {
					JWT string `json:"jwt"`
				} `json:"ott"`
			} `json:"enrollment"`
		} `json:"data"`
	}
	if err := mgmt(ctx, c, token, http.MethodGet,
		ctrl+"/edge/management/v1/identities/"+url.PathEscape(made.Data.ID), nil, &got); err != nil {
		return "", fmt.Errorf("the identity's enrolment token did not read: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	jwt := filepath.Join(dir, name+".jwt")
	if err := os.WriteFile(jwt, []byte(got.Data.Enrollment.OTT.JWT), 0o600); err != nil {
		return "", err
	}
	defer os.Remove(jwt)
	ectx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	enroll := exec.CommandContext(ectx, bin, "edge", "enroll", "--jwt", jwt,
		"--out", filepath.Join(dir, name+".json"))
	nowindow.Hide(enroll)
	out, err := enroll.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("the enrolment failed: %s", strings.TrimSpace(tailLines(string(out))))
	}
	return made.Data.ID, nil
}

// overlayLogin logs the overlay's admin in and answers the session token.
func overlayLogin(ctx context.Context, c *http.Client, home, ctrl string) (string, error) {
	pw, err := os.ReadFile(filepath.Join(home, "admin-password"))
	if err != nil {
		return "", err
	}
	var auth struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := mgmt(ctx, c, "", http.MethodPost, ctrl+"/edge/management/v1/authenticate?method=password",
		map[string]string{"username": "admin", "password": strings.TrimSpace(string(pw))}, &auth); err != nil {
		return "", err
	}
	return auth.Data.Token, nil
}

// mgmt is one call to an overlay's management API.
func mgmt(ctx context.Context, c *http.Client, token, method, u string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("zt-session", token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

// elevatedTunnel is the command to ask clint to run for a tunneler in tun mode, which needs admin or root.
func elevatedTunnel(file string) string {
	if runtime.GOOS == "windows" {
		return `ziti-edge-tunnel run -i "` + filepath.FromSlash(file) + `"   (in an Administrator shell)`
	}
	return "sudo ziti-edge-tunnel run -i '" + file + "'"
}

func (s *Server) overlayTunnel(w http.ResponseWriter, t *store.Task, in overlayRequest) {
	ov, err := s.liveOverlay(t.ID, strings.TrimSpace(in.Overlay))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(in.Identity)
	if !overlayName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, errors.New("say which identity runs the tunneler"))
		return
	}
	home := filepath.FromSlash(ov.Ref)
	file := filepath.Join(home, "identities", name+".json")
	if _, err := os.Stat(file); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("the overlay has no identity called %s. make it with action identity", name))
		return
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = "host"
	}
	if mode == "tun" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "needs_elevation", "command": elevatedTunnel(file),
			"error": "a tunneler in tun mode needs admin or root, and atrium never elevates. stop and ask clint to run " +
				"the command below in an elevated shell on this machine, saying what it is for, and the command that " +
				"stops it. or use host or proxy mode, which need neither"})
		return
	}
	bin, err := zitiBin()
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("ziti is not on this room's PATH"))
		return
	}
	args := []string{"tunnel", mode, "--identity", file, "--cli-agent=false"}
	got := map[string]int{}
	var portSeqs []int
	switch mode {
	case "host":
	case "proxy":
		if len(in.Services) == 0 || len(in.Services) > maxPortsAsked {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("proxy mode takes 1 to %d services", maxPortsAsked))
			return
		}
		for _, svc := range in.Services {
			if strings.TrimSpace(svc) == "" || strings.ContainsAny(svc, ":\r\n") {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("%q is not a service name", svc))
				return
			}
		}
		ports, _, err := s.takePorts(t.ID, len(in.Services))
		if err != nil {
			s.freeTaken(t.ID, ports, "the tunneler did not start")
			writeErr(w, http.StatusConflict, err)
			return
		}
		for i, svc := range in.Services {
			got[strings.TrimSpace(svc)] = ports[i]
			args = append(args, strings.TrimSpace(svc)+":"+strconv.Itoa(ports[i]))
			portSeqs = append(portSeqs, s.portSeq(t.ID, ports[i]))
		}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("mode is host, proxy or tun"))
		return
	}
	logFile := filepath.Join(home, "tunnel-"+name+"-"+mode+".log")
	pid, seq, err := s.startOwned(t.ID, home, logFile, bin, args...)
	if err != nil {
		for _, ps := range portSeqs {
			_ = s.st.FreeResource(t.ID, ps, "the tunneler did not start")
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := addOverlayRows(home, append(portSeqs, seq)...); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := map[string]any{"pid": pid, "mode": mode, "identity": name, "log": filepath.ToSlash(logFile)}
	if mode == "proxy" {
		out["ports"] = got
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) overlayDown(w http.ResponseWriter, t *store.Task, name string) {
	ov, err := s.liveOverlay(t.ID, name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	home := inCards(ov.Ref)
	if home == "" {
		writeErr(w, http.StatusBadRequest, errors.New(ov.Ref+" is outside the cards folder, so it is not deleted here"))
		return
	}
	overlayMu.Lock()
	st, _ := readOverlayState(home)
	overlayMu.Unlock()
	if st == nil {
		st = &overlayState{}
	}
	if err := s.overlayFree(t.ID, home, st, ""); err != nil {
		_ = s.st.ResourceFailed(t.ID, ov.Seq, err.Error())
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	_ = s.st.FreeResource(t.ID, ov.Seq, "")
	writeJSON(w, http.StatusOK, map[string]any{"overlay": filepath.Base(home), "down": true})
}

// overlayFree stops the overlay's processes newest first, frees its ports, deletes its folder, and marks its
// identities freed. The overlay's own row is the caller's.
func (s *Server) overlayFree(card, home string, st *overlayState, why string) error {
	rows, _ := s.st.Resources(card)
	mine := map[int]bool{}
	for _, seq := range st.Rows {
		mine[seq] = true
	}
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if !r.Live() || !mine[r.Seq] {
			continue
		}
		switch r.Kind {
		case store.ResProc:
			if procStillIt(r) {
				pid, _ := strconv.Atoi(r.Ref)
				if err := cardproc.StopTree(pid); err != nil {
					_ = s.st.ResourceFailed(card, r.Seq, err.Error())
					return fmt.Errorf("process %s did not stop: %v", r.Ref, err)
				}
			}
			_ = s.st.FreeResource(card, r.Seq, why)
		case store.ResPort:
			_ = s.st.FreeResource(card, r.Seq, why)
		}
	}
	if err := removeAllSoon(home); err != nil {
		return fmt.Errorf("%s did not delete: %v", filepath.ToSlash(home), err)
	}
	s.freeIdentitiesOf(card, rows, home)
	return nil
}

// freeIdentitiesOf marks an overlay's identities freed once its folder is gone.
func (s *Server) freeIdentitiesOf(card string, rows []*store.CardResource, home string) {
	for _, r := range rows {
		if r.Kind == store.ResIdentity && r.Live() && eqPath(filepath.FromSlash(r.Detail), home) {
			_ = s.st.FreeResource(card, r.Seq, "")
		}
	}
}

// removeAllSoon deletes a folder, trying a few times: a process just stopped on Windows lets go of its files a moment
// after it is gone.
func removeAllSoon(dir string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

// freeTaken frees ports takePorts recorded for something that then did not start.
func (s *Server) freeTaken(card string, ports []int, why string) {
	for _, p := range ports {
		if seq := s.portSeq(card, p); seq > 0 {
			_ = s.st.FreeResource(card, seq, why)
		}
	}
}

// portSeq is the seq of a card's live row for a port, or 0.
func (s *Server) portSeq(card string, port int) int {
	rows, _ := s.st.Resources(card)
	for i := len(rows) - 1; i >= 0; i-- {
		if r := rows[i]; r.Kind == store.ResPort && r.Live() && r.Ref == strconv.Itoa(port) {
			return r.Seq
		}
	}
	return 0
}

func readOverlayState(home string) (*overlayState, error) {
	raw, err := os.ReadFile(filepath.Join(home, overlayStateFile))
	if err != nil {
		return nil, err
	}
	var st overlayState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func writeOverlayState(home string, st *overlayState) error {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, overlayStateFile), raw, 0o600)
}

// addOverlayRows adds rows to an overlay's own list.
func addOverlayRows(home string, seqs ...int) error {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	st, err := readOverlayState(home)
	if err != nil {
		return err
	}
	st.Rows = append(st.Rows, seqs...)
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, overlayStateFile), raw, 0o600)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tailOf is the last lines of a log file, for an error.
func tailOf(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(tailLines(string(raw)))
}
