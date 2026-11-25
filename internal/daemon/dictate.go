package daemon

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/dictate"
)

// dictateConf is the whisper-server and model from the room's settings, read on every call so a change takes.
func (d *Daemon) dictateConf() (string, string) {
	bin, _ := d.st.Setting(api.SettingDictateServer)
	model, _ := d.st.Setting(api.SettingDictateModel)
	return strings.TrimSpace(bin), strings.TrimSpace(model)
}

// ErrHubDictateUnavailable marks a hub that cannot transcribe right now: down, older than the kind, or with speech to
// text off. The room then tries its own whisper before it says why not.
var ErrHubDictateUnavailable = errors.New("the hub has no speech to text")

// hubDictator is a Relay that can send a clip up the room's link to the hub's whisper, and ask whether it would take
// one. Both fail with ErrHubDictateUnavailable when the hub cannot. The link adapter in internal/cli implements it.
type hubDictator interface {
	Dictate(ctx context.Context, wav []byte, words []string) (string, error)
	DictateReady(ctx context.Context) error
}

// maxHubWords is how many card names go up with a clip, the most the hub primes whisper with.
const maxHubWords = 300

func (d *Daemon) dictateLocal() bool {
	bin, model := d.dictateConf()
	return bin != "" && model != ""
}

// dictateOff is why dictation cannot work on this room, or nil. A linked room is ready when the hub has speech to text
// on, or failing that when it has its own whisper, and otherwise says why the hub cannot.
func (d *Daemon) dictateOff() error {
	if hd, ok := d.Relay().(hubDictator); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := hd.DictateReady(ctx)
		if err == nil || d.dictateLocal() {
			return nil
		}
		return err
	}
	if !d.dictateLocal() {
		return dictate.ErrOff
	}
	return nil
}

// dictateText is the room's half of push-to-talk. A linked room sends the clip up when the hub has speech to text on,
// otherwise uses its own whisper-server when set, and only then says why it cannot. Either way the post-pass follows.
func (d *Daemon) dictateText(ctx context.Context, wav []byte, words []string) (string, error) {
	if hd, ok := d.Relay().(hubDictator); ok {
		if len(words) > maxHubWords {
			words = words[:maxHubWords]
		}
		text, err := hd.Dictate(ctx, wav, words)
		if !errors.Is(err, ErrHubDictateUnavailable) || !d.dictateLocal() {
			return text, err
		}
	}
	raw, err := d.dict.Transcribe(ctx, wav, dictate.Prompt(words))
	if err != nil {
		return "", err
	}
	return dictate.Clean(raw, words), nil
}

// dictateWarm loads the model at start, so the first clip does not wait for it.
func (d *Daemon) dictateWarm(ctx context.Context) {
	if bin, model := d.dictateConf(); bin == "" || model == "" {
		return
	}
	t0 := time.Now()
	if err := d.dict.Warm(ctx); err != nil {
		log.Printf("[atrium] dictation: %v", err)
		return
	}
	log.Printf("[atrium] dictation: whisper-server is warm in %d ms", time.Since(t0).Milliseconds())
}
