package link

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// Web Push, the notifier's second sink. See docs/rnd/web-push-design.md and docs/backlog/ui/u-new-web-push-build.md.
//
// ── what is shared with the command sink ────────────────
//
// Everything about WHEN: the trigger, the dedupe, the seeding, the desktop-tab quiet and the drop-oldest queue are the
// notifier's, so the phone follows the board's one set of rules and not a second set. This file is only HOW a notice
// reaches a browser.
//
// ── the credential rule ─────────────────────────────────
//
// The VAPID private key is atrium's own, made on first enable and kept in hub_setting. IT IS NEVER LOGGED, RETURNED
// OR WRITTEN ANYWHERE ELSE. A subscription's endpoint, p256dh and auth are the browser's, handed over for exactly this
// use. The endpoint is also a capability (whoever holds it can push to that browser), so it is kept out of logs and
// out of error text, and the list the gear reads never carries the keys.
//
// ── what goes out ───────────────────────────────────────
//
// FOUR PLAIN FIELDS, encrypted to the one browser: title (the card's name cut to 60), body (one of four phrases), tag
// (the card id) and path (where a tap lands). Never the command a permission wants to run, a question's text or a
// recap. No action buttons: an approval from a lock screen would be given without seeing the command.

// Push settings, in the hub_setting table.
const (
	settingPushEnabled = "push.enabled"
	settingPushPrivate = "push.vapid_private"
	settingPushSub     = "push.vapid_sub"
)

// pushDefaultSub is the VAPID contact when the operator set none. A URL and never an email address: Apple wants one,
// and the push service sees it.
const pushDefaultSub = "https://github.com/dovholuknf/atrium"

const (
	pushMaxFailures = 3
	pushTTL         = "3600"
	pushTitleMax    = 60
	pushBurstMax    = 5
	pushBurstWindow = 2 * time.Minute
	pushResponseMax = 4 << 10
	pushSendTimeout = 10 * time.Second
	pushSummaryTag  = "atrium-summary"
	pushLabelMax    = 80
	pushOriginMax   = 200
)

// PushStore is what push needs from the hub's database. *hubstore.Store is one.
type PushStore interface {
	Setting(name string) (string, error)
	SetSetting(name, value string) error
	PushSubs() ([]hubstore.PushSub, error)
	PushAdd(p hubstore.PushSub) (id string, added bool, err error)
	PushRemoveID(id string) error
	PushRemoveEndpoint(endpoint string) error
	PushOutcome(id string, ok bool, reason string, limit int) (failures int, err error)
}

// pushBody is the phrase for a reason.
func pushBody(reason string) string {
	switch reason {
	case ReasonPermission:
		return "wants permission"
	case ReasonQuestion:
		return "asked you something"
	case ReasonInput:
		return "is waiting for you"
	case ReasonFinished:
		return "finished its turn"
	case "test":
		return "this is a test alert"
	}
	return "wants you"
}

// pushPayload is the whole message.
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	Path  string `json:"path"`
}

// clipRunes cuts s to at most n runes.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// payloadFor builds a notice's message. The path is the form the board's card.js opens by id, with no alias lookup.
func payloadFor(n Notice) pushPayload {
	title := strings.TrimSpace(clipRunes(n.Name, pushTitleMax))
	if title == "" {
		title = "atrium"
	}
	return pushPayload{Title: title, Body: pushBody(n.Reason), Tag: n.Card, Path: "/m/#term=" + url.QueryEscape(n.Card)}
}

// pushTopic is the RFC 8030 Topic for a card: at most 32 characters of the base64url alphabet, so the card id is
// hashed down to a stable one. The push service replaces an undelivered push with the same Topic.
func pushTopic(card string) string {
	sum := sha256.Sum256([]byte(card))
	return b64.EncodeToString(sum[:])[:32]
}

// Push is the switch, the key and the subscriptions, and the sink the notifier runs.
type Push struct {
	st PushStore
	n  *Notifier

	// client, allow and now are variables for the tests. A test never reaches a real push service: it swaps the
	// client for one that dials a fake and the allowlist for one that lets the fake's address through.
	client *http.Client
	allow  func(endpoint string) error
	now    func() time.Time

	// announce says a new device subscribed. The proxy wires it to the desktop growler and the audit feed.
	announce func(id, label, origin string)

	mu      sync.Mutex
	enabled bool
	key     *ecdsa.PrivateKey
	burst   map[string]*pushBurst
}

// pushBurst is one subscription's recent sends, for the cap.
type pushBurst struct {
	times   []time.Time
	summary bool
	count   int
	last    time.Time
}

// NewPush reads the switch and adds the push sink to the notifier.
func NewPush(st PushStore, n *Notifier) *Push {
	p := &Push{st: st, n: n, allow: pushEndpointOK, now: time.Now, burst: map[string]*pushBurst{}}
	p.client = &http.Client{
		Timeout: pushSendTimeout,
		// A push service answers and never redirects. Following one would be a way to send somewhere not allowed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if v, _ := st.Setting(settingPushEnabled); v == "on" {
		p.enabled = true
	}
	if n != nil {
		n.AddSink("push", pushSink{p}, p.On)
	}
	return p
}

// On reports whether push is switched on.
func (p *Push) On() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled
}

// signer returns the hub's key, making it on first use.
func (p *Push) signer() (*ecdsa.PrivateKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.key != nil {
		return p.key, nil
	}
	stored, err := p.st.Setting(settingPushPrivate)
	if err != nil {
		return nil, err
	}
	if stored == "" {
		if stored, err = newVAPIDKey(); err != nil {
			return nil, err
		}
		if err := p.st.SetSetting(settingPushPrivate, stored); err != nil {
			return nil, err
		}
	}
	k, err := parseVAPIDKey(stored)
	if err != nil {
		return nil, err
	}
	p.key = k
	return k, nil
}

// PublicKey is what the browser is given to subscribe with. Empty while push is off.
func (p *Push) PublicKey() (string, bool) {
	if !p.On() {
		return "", false
	}
	k, err := p.signer()
	if err != nil {
		return "", false
	}
	pub, err := vapidPublic(k)
	return pub, err == nil
}

// Contact is the VAPID sub.
func (p *Push) Contact() string {
	if v, _ := p.st.Setting(settingPushSub); v != "" {
		return v
	}
	return pushDefaultSub
}

// SetContact sets the VAPID sub, which has to be an https URL or a mailto: address.
func (p *Push) SetContact(sub string) error {
	sub = strings.TrimSpace(sub)
	if sub != "" {
		u, err := url.Parse(sub)
		if err != nil || !(u.Scheme == "https" && u.Host != "" || u.Scheme == "mailto" && u.Opaque != "") {
			return errors.New("the contact has to be an https URL or a mailto: address")
		}
	}
	return p.st.SetSetting(settingPushSub, sub)
}

// SetEnabled switches push on or off. Turning it on makes the key if there is none and seeds the trigger, so what is
// already waiting does not flood the phone. Turning it off keeps the key and the subscriptions.
func (p *Push) SetEnabled(on bool) error {
	if on == p.On() {
		return nil
	}
	if on {
		if _, err := p.signer(); err != nil {
			return err
		}
		if p.n != nil {
			if err := p.n.Arm(); err != nil {
				return err
			}
		}
	}
	v := "off"
	if on {
		v = "on"
	}
	if err := p.st.SetSetting(settingPushEnabled, v); err != nil {
		return err
	}
	p.mu.Lock()
	p.enabled = on
	p.mu.Unlock()
	return nil
}

// Rotate makes a new key. Every subscription was made against the old public key and a push service refuses it under
// another, so they are all removed and each phone subscribes again. Returns how many.
func (p *Push) Rotate() (int, error) {
	stored, err := newVAPIDKey()
	if err != nil {
		return 0, err
	}
	k, err := parseVAPIDKey(stored)
	if err != nil {
		return 0, err
	}
	subs, err := p.st.PushSubs()
	if err != nil {
		return 0, err
	}
	if err := p.st.SetSetting(settingPushPrivate, stored); err != nil {
		return 0, err
	}
	p.mu.Lock()
	p.key = k
	p.mu.Unlock()
	for _, s := range subs {
		_ = p.st.PushRemoveID(s.ID)
	}
	return len(subs), nil
}

// Subscribe stores a subscription and reports whether it is a new device. An endpoint off the allowlist is refused
// here and again at every send.
func (p *Push) Subscribe(s hubstore.PushSub) (id string, added bool, err error) {
	if err := p.allow(s.Endpoint); err != nil {
		return "", false, err
	}
	pub, err := b64.DecodeString(strings.TrimRight(s.P256dh, "="))
	if err != nil || len(pub) != 65 {
		return "", false, errors.New("the p256dh key is not a 65 byte P-256 point")
	}
	if a, err := b64.DecodeString(strings.TrimRight(s.Auth, "=")); err != nil || len(a) != 16 {
		return "", false, errors.New("the auth secret is not 16 bytes")
	}
	s.P256dh, s.Auth = b64.EncodeToString(pub), strings.TrimRight(s.Auth, "=")
	s.Label = clipRunes(strings.TrimSpace(s.Label), pushLabelMax)
	s.Origin = clipRunes(strings.TrimSpace(s.Origin), pushOriginMax)
	id, added, err = p.st.PushAdd(s)
	if err != nil {
		return "", false, err
	}
	if added && p.announce != nil {
		p.announce(id, s.Label, s.Origin)
	}
	return id, added, nil
}

// Test sends one test push to the subscription with that id, now, and reports how it went. It is the push the hub
// sends after a subscribe, and the operator's test.
func (p *Push) Test(ctx context.Context, id string) Result {
	subs, err := p.st.PushSubs()
	if err != nil {
		return Result{ExitCode: -1, Err: err.Error()}
	}
	for _, s := range subs {
		if s.ID == id {
			return p.deliver(ctx, s, pushPayload{Title: "atrium test", Body: pushBody("test"), Tag: "atrium-test",
				Path: "/m"}, "test", "normal")
		}
	}
	return Result{ExitCode: -1, Err: ErrNoSub.Error()}
}

// TestAll sends the test push to every subscription.
func (p *Push) TestAll(ctx context.Context) []Result {
	subs, _ := p.st.PushSubs()
	out := make([]Result, len(subs))
	var wg sync.WaitGroup
	for i, s := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = p.deliver(ctx, s, pushPayload{Title: "atrium test", Body: pushBody("test"), Tag: "atrium-test",
				Path: "/m"}, "test", "normal")
		}()
	}
	wg.Wait()
	return out
}

// ErrNoSub is a subscription nobody holds.
var ErrNoSub = hubstore.ErrNoPushSub

// pushSink is the Sink the notifier runs. A value type so the notifier holds no pointer to the whole of Push.
type pushSink struct{ p *Push }

// Send gives the notice to every subscription that is on, at once and each bounded, so one slow push service delays
// none of the others. The result is ok when no subscription was due one or at least one took it.
func (s pushSink) Send(ctx context.Context, n Notice) Result {
	began := time.Now()
	subs, err := s.p.st.PushSubs()
	if err != nil {
		return Result{ExitCode: -1, Err: err.Error()}
	}
	urgency := "normal"
	if n.Reason == ReasonPermission {
		urgency = "high"
	}
	type one struct {
		res Result
		ran bool
	}
	got := make([]one, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		if sub.DisabledReason != "" {
			continue
		}
		pay, summary := s.p.capped(sub.ID, n)
		u := urgency
		if summary {
			u = "high"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = one{res: s.p.deliver(ctx, sub, pay, n.Card, u), ran: true}
		}()
	}
	wg.Wait()
	res := Result{OK: true, TookMS: time.Since(began).Milliseconds()}
	ok, ran := 0, 0
	for _, g := range got {
		if !g.ran {
			continue
		}
		ran++
		if g.res.OK {
			ok++
		} else if res.Err == "" {
			res.Err = g.res.Err
		}
	}
	if ran > 0 && ok == 0 {
		res.OK = false
		res.ExitCode = -1
	}
	return res
}

// capped is the burst cap: more than pushBurstMax in pushBurstWindow to one subscription becomes one "N cards want
// you" summary under the tag atrium-summary, until a quiet window passes.
func (p *Push) capped(id string, n Notice) (pushPayload, bool) {
	at := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.burst[id]
	if b == nil {
		b = &pushBurst{}
		p.burst[id] = b
	}
	defer func() { b.last = at }()
	if b.summary && at.Sub(b.last) >= pushBurstWindow {
		*b = pushBurst{}
	}
	if b.summary {
		b.count++
		return summaryPayload(b.count), true
	}
	keep := b.times[:0]
	for _, t := range b.times {
		if at.Sub(t) < pushBurstWindow {
			keep = append(keep, t)
		}
	}
	b.times = append(keep, at)
	if len(b.times) > pushBurstMax {
		b.summary, b.count, b.times = true, len(b.times), nil
		return summaryPayload(b.count), true
	}
	return payloadFor(n), false
}

func summaryPayload(n int) pushPayload {
	return pushPayload{Title: fmt.Sprintf("%d cards want you", n), Body: "open the board", Tag: pushSummaryTag, Path: "/m"}
}

// deliver is one POST to one subscription. It decides what the outcome means: a 404 or 410 deletes the row, and three
// failures in a row switch it off with the reason. The endpoint is never in a log line or an error.
func (p *Push) deliver(ctx context.Context, s hubstore.PushSub, pay pushPayload, topicOf, urgency string) Result {
	began := time.Now()
	res := Result{ExitCode: -1}
	done := func() Result { res.TookMS = time.Since(began).Milliseconds(); return res }
	fail := func(why string) Result {
		res.Err = why
		res.Output = why
		n, err := p.st.PushOutcome(s.ID, false, why, pushMaxFailures)
		if err == nil && n == pushMaxFailures {
			log.Printf("[hub] push to %q is switched off after %d failures in a row: %s", s.Label, n, why)
		}
		return done()
	}
	// CHECKED AGAIN ON EVERY SEND, so a changed allowlist applies to rows already stored.
	if err := p.allow(s.Endpoint); err != nil {
		return fail(err.Error())
	}
	key, err := p.signer()
	if err != nil {
		return fail("the VAPID key could not be read")
	}
	pub, err := b64.DecodeString(s.P256dh)
	if err != nil {
		return fail("the stored p256dh is damaged")
	}
	authSecret, err := b64.DecodeString(s.Auth)
	if err != nil {
		return fail("the stored auth secret is damaged")
	}
	plain, _ := json.Marshal(pay)
	body, err := encryptPush(plain, pub, authSecret)
	if err != nil {
		return fail(err.Error())
	}
	auth, err := vapidHeader(key, s.Endpoint, p.Contact(), p.now())
	if err != nil {
		return fail("the VAPID header could not be made")
	}
	ctx, cancel := context.WithTimeout(ctx, pushSendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fail("the endpoint is not a URL")
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", pushTTL)
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Topic", pushTopic(topicOf))
	req.Header.Set("Authorization", auth)
	resp, err := p.client.Do(req)
	if err != nil {
		// url.Error's text is the whole URL, and the endpoint is a capability.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fail("the push service could not be reached: " + clipRunes(err.Error(), 120))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, pushResponseMax))
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// The browser dropped it, or the web app was deleted. That is how a gone phone goes away.
		_ = p.st.PushRemoveID(s.ID)
		p.dropped(s.ID)
		res.Err = fmt.Sprintf("the push service says it is gone (%d), so it was removed", resp.StatusCode)
		res.Output = res.Err
		return done()
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		_, _ = p.st.PushOutcome(s.ID, true, "", pushMaxFailures)
		res.OK, res.ExitCode = true, 0
		return done()
	}
	return fail(fmt.Sprintf("the push service answered %d", resp.StatusCode))
}

// dropped forgets a removed subscription's burst state.
func (p *Push) dropped(id string) {
	p.mu.Lock()
	delete(p.burst, id)
	p.mu.Unlock()
}
