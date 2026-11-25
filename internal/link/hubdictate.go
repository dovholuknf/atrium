package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/daemon/dictate"
	"github.com/dovholuknf/atrium/internal/link/conn"
)

// HubDictation turns a clip into text on the hub's own whisper-server. On is false until the operator sets one.
type HubDictation interface {
	On() bool
	Transcribe(ctx context.Context, wav []byte, words []string) (string, error)
}

// SetDictation gives the proxy the hub's speech to text.
func (p *Proxy) SetDictation(d HubDictation) {
	p.mu.Lock()
	p.dict = d
	p.mu.Unlock()
	if p.hub != nil {
		p.hub.Dictate = p.roomClip
		p.hub.DictateOn = func() bool { d := p.dictation(); return d != nil && d.On() }
	}
}

// SetDictateInstall gives the proxy the enable switch: downloads into dir, then fills in the two paths and warms the
// server. The hub owns the whisper-server it starts, in a job that dies with it (internal/daemon/dictate).
func (p *Proxy) SetDictateInstall(dir string) {
	inst := &dictate.Installer{Dir: dir}
	inst.Done = func(bin, model string) {
		if st, ok := p.inventory().(interface {
			SetHubSetting(name, value string) error
		}); ok {
			_ = st.SetHubSetting("dictate_server", bin)
			_ = st.SetHubSetting("dictate_model", model)
		}
		if d, ok := p.dictation().(interface{ Warm(context.Context) error }); ok {
			t0 := time.Now()
			if err := d.Warm(context.Background()); err != nil {
				log.Printf("[hub] speech to text: %v", err)
			} else {
				log.Printf("[hub] speech to text: whisper-server warm in %d ms", time.Since(t0).Milliseconds())
			}
		}
	}
	p.mu.Lock()
	p.inst = inst
	p.mu.Unlock()
}

func (p *Proxy) dictateInstaller() *dictate.Installer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inst
}

// enableDictation is the switch: on starts the download after the operator confirmed its size and source, off clears
// the two paths and leaves the files.
func (p *Proxy) enableDictation(on, confirmed bool, st interface {
	SetHubSetting(name, value string) error
}) error {
	inst := p.dictateInstaller()
	if inst == nil {
		return errors.New("this hub has nowhere to keep the download")
	}
	if !on {
		for _, k := range []string{"dictate_server", "dictate_model"} {
			if err := st.SetHubSetting(k, ""); err != nil {
				return err
			}
		}
		return nil
	}
	if !confirmed {
		return errors.New("confirm the download of " + dictate.Size(inst.Plan().Total) + " from " + inst.Plan().Source)
	}
	return inst.Start()
}

// roomClip is a clip a room sent up its link, answered from the hub's whisper, or ErrDictateOff when there is none.
func (p *Proxy) roomClip(ctx context.Context, _ string, wav []byte, words []string) (string, error) {
	d := p.dictation()
	if d == nil || !d.On() {
		return "", conn.ErrDictateOff
	}
	return d.Transcribe(ctx, wav, words)
}

func (p *Proxy) dictation() HubDictation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dict
}

const (
	maxHubClip   = 8 << 20
	maxHubWords  = 300
	dictateWords = "X-Atrium-Words"
)

// hubDictate answers `POST /v1/tasks/<id>/dictate` from the hub's own whisper-server, so a card on any room can be
// dictated into from the hub board. With no hub speech to text it answers false and the room gets the clip, as before.
// The board sends the card names it knows in X-Atrium-Words, each path-escaped, since the hub holds no card list.
func (p *Proxy) hubDictate(w http.ResponseWriter, r *http.Request) bool {
	if (r.Method != http.MethodPost && r.Method != http.MethodGet) || !strings.HasPrefix(r.URL.Path, "/v1/tasks/") ||
		!strings.HasSuffix(r.URL.Path, "/dictate") {
		return false
	}
	d := p.dictation()
	if d == nil || !d.On() {
		return false
	}
	if r.Method == http.MethodGet {
		writeJSONBody(w, http.StatusOK, map[string]any{"ready": true})
		return true
	}
	t0 := time.Now()
	wav, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHubClip))
	if err != nil {
		writeErrBody(w, http.StatusRequestEntityTooLarge, "the clip is over 8 MB")
		return true
	}
	if len(wav) < 44 || !bytes.HasPrefix(wav, []byte("RIFF")) || string(wav[8:12]) != "WAVE" {
		writeErrBody(w, http.StatusBadRequest, "send the clip as WAV")
		return true
	}
	t1 := time.Now()
	text, err := d.Transcribe(r.Context(), wav, hubWords(r.Header.Get(dictateWords)))
	if err != nil {
		writeErrBody(w, http.StatusServiceUnavailable, err.Error())
		return true
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("read;dur=%d, whisper;dur=%d",
		t1.Sub(t0).Milliseconds(), time.Since(t1).Milliseconds()))
	first, _ := d.(interface{ FirstClip() bool })
	dictate.LogClip("hub", len(wav), t1.Sub(t0), time.Since(t1), time.Since(t0), r.Header.Get(dictate.LastTrip),
		first != nil && first.FirstClip())
	writeJSONBody(w, http.StatusOK, map[string]any{"text": text})
	return true
}

// hostOf is the host a clip is sent to, for the mic's tooltip and the settings line.
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func hubWords(h string) []string {
	var out []string
	for _, part := range strings.Split(h, ",") {
		if w, err := url.PathUnescape(part); err == nil && strings.TrimSpace(w) != "" {
			out = append(out, w)
		}
		if len(out) == maxHubWords {
			break
		}
	}
	return out
}

// hubWhisper is the hub's engine and the post-pass, reading the two paths on every call like a room does.
type hubWhisper struct {
	eng  *dictate.Engine
	conf func() (string, string)
	url  func() string
}

// NewHubDictation is speech to text on the hub, with conf answering the whisper-server and model paths. A url func,
// when given, names a shared whisper-server and wins over the paths.
func NewHubDictation(conf func() (string, string), url ...func() string) HubDictation {
	h := &hubWhisper{conf: conf, url: func() string { return "" }}
	if len(url) > 0 && url[0] != nil {
		h.url = url[0]
	}
	h.eng = &dictate.Engine{Conf: conf, URL: h.url}
	return h
}

// Warm starts the server now, so the first clip does not wait for the model.
func (h *hubWhisper) Warm(ctx context.Context) error { return h.eng.Warm(ctx) }

func (h *hubWhisper) FirstClip() bool { return h.eng.FirstClip() }

func (h *hubWhisper) On() bool {
	if dictate.Base(h.url()) != "" {
		return true
	}
	bin, model := h.conf()
	return bin != "" && model != ""
}

func (h *hubWhisper) Transcribe(ctx context.Context, wav []byte, words []string) (string, error) {
	raw, err := h.eng.Transcribe(ctx, wav, dictate.Prompt(words))
	if err != nil {
		return "", err
	}
	return dictate.Clean(raw, words), nil
}

// hubDictateSettings are the two paths the hub's whisper-server runs from, and the url of a shared one. Starting a
// program on the hub or sending audio elsewhere, so only from the hub's own machine.
var hubDictateSettings = []string{"dictate_server", "dictate_model", "dictate_url", "dictate_enable"}

// saveHubDictate stores dictate_server and dictate_model on the hub. False when the body names neither.
func (p *Proxy) saveHubDictate(w http.ResponseWriter, r *http.Request, keys map[string]any, stock Inventory) bool {
	named := false
	for _, k := range hubDictateSettings {
		if _, ok := keys[k]; ok {
			named = true
		}
	}
	if !named {
		return false
	}
	if !edge.LocalOperator(r) {
		writeErrBody(w, http.StatusForbidden, "speech to text is set from the machine the hub runs on")
		return true
	}
	st, ok := stock.(interface {
		SetHubSetting(name, value string) error
	})
	if !ok {
		writeErrBody(w, http.StatusServiceUnavailable, "this hub has no store to keep the setting in")
		return true
	}
	if on, ok := keys["dictate_enable"].(bool); ok {
		if err := p.enableDictation(on, keys["confirm_download"] == true, st); err != nil {
			writeErrBody(w, http.StatusBadRequest, err.Error())
			return true
		}
	}
	if v, ok := keys["dictate_url"].(string); ok && strings.TrimSpace(v) != "" {
		confirm, _ := keys["confirm_plain_http"].(bool)
		if err := dictate.CheckURL(strings.TrimSpace(v), confirm); err != nil {
			writeErrBody(w, http.StatusBadRequest, err.Error())
			return true
		}
		keys["dictate_url"] = strings.TrimSpace(v)
	}
	for _, k := range hubDictateSettings {
		if v, ok := keys[k].(string); ok {
			if err := st.SetHubSetting(k, v); err != nil {
				writeErrBody(w, http.StatusInternalServerError, err.Error())
				return true
			}
		}
	}
	writeJSONBody(w, http.StatusOK, p.hubDictateView(stock))
	return true
}

// oneRoomSettings is /v1/settings on a hub with a single room, where the request goes straight to that room and the
// hub's speech to text section would never reach the board. Only the dictate fields are the hub's here, the rest is
// the room's answer as it was.
func (p *Proxy) oneRoomSettings(w http.ResponseWriter, r *http.Request, room string) bool {
	stock := p.inventory()
	if r.URL.Path != "/v1/settings" || stock == nil {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		body := p.askRoomBody(r, room)
		if body == nil {
			return false
		}
		for k, v := range p.hubDictateView(stock) {
			body[k] = v
		}
		writeJSONBody(w, http.StatusOK, body)
		return true
	case http.MethodPost, http.MethodPut:
		payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(payload), r.Body))
		var keys map[string]any
		if err != nil || json.Unmarshal(payload, &keys) != nil {
			return false
		}
		return p.saveHubDictate(w, r, keys, stock)
	}
	return false
}

func (p *Proxy) hubDictateView(stock Inventory) map[string]any {
	out := map[string]any{"hub_dictate_server": "", "hub_dictate_model": "", "hub_dictate_url": ""}
	if inst := p.dictateInstaller(); inst != nil {
		out["hub_dictate_install"] = map[string]any{"plan": inst.Plan(), "status": inst.Status()}
	}
	if st, ok := stock.(interface {
		HubSetting(name string) (string, error)
	}); ok {
		out["hub_dictate_server"], _ = st.HubSetting("dictate_server")
		out["hub_dictate_model"], _ = st.HubSetting("dictate_model")
		out["hub_dictate_url"], _ = st.HubSetting("dictate_url")
	}
	url, _ := out["hub_dictate_url"].(string)
	server, _ := out["hub_dictate_server"].(string)
	model, _ := out["hub_dictate_model"].(string)
	switch {
	case dictate.Base(url) != "":
		out["hub_dictate_where"] = hostOf(url)
	case server != "" && model != "":
		out["hub_dictate_where"] = "this hub"
	default:
		out["hub_dictate_where"] = ""
	}
	return out
}
