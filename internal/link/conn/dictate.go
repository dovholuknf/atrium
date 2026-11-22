package conn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The dictate kind: a room sending a push-to-talk clip up to its hub's whisper. Dialled by the room, one clip, one
// answer, closed, like the relay. The hub reaches into nothing.

const dictateKind = wire.DictateKind

// MaxDictateClip is the largest clip either end carries, the board's own limit.
const MaxDictateClip = 8 << 20

// dictateWait bounds one clip: the model may need to load first.
const dictateWait = 3 * time.Minute

// ErrDictateOff is a hub that answered and has no speech to text set up.
var ErrDictateOff = errors.New("speech to text is off on the hub")

// ErrDictateDown is a hub this room could not reach.
var ErrDictateDown = errors.New("the hub is not answering, so there is no speech to text")

// ErrDictateOld is a hub that predates the dictate kind.
var ErrDictateOld = errors.New("the hub is older than speech to text. update the hub")

// DictateRequest is the frame before the audio: the card names to prime whisper with, and the bytes that follow.
type DictateRequest struct {
	Words []string `json:"words,omitempty"`
	Size  int      `json:"size"`
}

// DictateAnswer is the hub's one reply.
type DictateAnswer struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text,omitempty"`
	Off   bool   `json:"off,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dictate sends one WAV clip to the hub and answers the text. The error is ErrDictateOff, ErrDictateDown,
// ErrDictateOld, or the hub's own sentence when whisper failed.
func (r *Room) Dictate(ctx context.Context, wav []byte, words []string) (string, error) {
	if len(wav) > MaxDictateClip {
		return "", fmt.Errorf("the clip is over %d MB", MaxDictateClip>>20)
	}
	c, br, err := r.dictateConn(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	defer r.controlDialing()()
	if err := c.SetDeadline(time.Now().Add(dictateWait)); err != nil {
		return "", fmt.Errorf("%w: %v", ErrDictateDown, err)
	}
	if err := writeJSON(c, DictateRequest{Words: words, Size: len(wav)}); err != nil {
		return "", fmt.Errorf("%w: %v", ErrDictateDown, err)
	}
	if _, err := c.Write(wav); err != nil {
		return "", fmt.Errorf("%w: %v", ErrDictateDown, err)
	}
	var ans DictateAnswer
	if err := readJSONLimit(br, &ans, announceMax); err != nil {
		return "", fmt.Errorf("%w: %v", ErrDictateDown, err)
	}
	switch {
	case ans.Off:
		return "", ErrDictateOff
	case !ans.OK:
		return "", errors.New(ans.Error)
	}
	return ans.Text, nil
}

// DictateReady asks the hub whether it would take a clip, and sends none: its welcome says. Nil means yes.
func (r *Room) DictateReady(ctx context.Context) error {
	c, _, err := r.dictateConn(ctx)
	if err != nil {
		return err
	}
	return c.Close()
}

// dictateConn is a dictate connection past a welcome that said yes, or why not.
func (r *Room) dictateConn(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	r.mu.Lock()
	up, session := r.up, r.session
	r.mu.Unlock()
	if !up {
		return nil, nil, fmt.Errorf("%w: this room is not attached to it", ErrDictateDown)
	}
	defer r.controlDialing()()
	c, br, w, err := r.kindConn(ctx, session, dictateKind)
	if err != nil {
		switch {
		case !w.OK && strings.Contains(w.Error, "a connection is control") && !strings.Contains(w.Error, dictateKind):
			return nil, nil, ErrDictateOld
		case !w.OK && w.Error == ErrDictateOff.Error():
			return nil, nil, ErrDictateOff
		case !w.OK && w.Error != "":
			return nil, nil, errors.New(w.Error)
		}
		return nil, nil, fmt.Errorf("%w: %v", ErrDictateDown, err)
	}
	return c, br, nil
}

// serveDictate reads one clip from an attached room and answers with the hub's transcription.
func (h *Hub) serveDictate(ctx context.Context, name string, conn net.Conn, br *bufio.Reader) {
	if h.Dictate == nil || (h.DictateOn != nil && !h.DictateOn()) {
		_ = writeJSON(conn, welcome{OK: false, Error: ErrDictateOff.Error()})
		return
	}
	if !h.Has(name) {
		_ = writeJSON(conn, welcome{OK: false, Error: "attach to this hub before sending it a clip"})
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	if err := conn.SetDeadline(time.Now().Add(dictateWait)); err != nil {
		return
	}
	var req DictateRequest
	if err := readJSONLimit(br, &req, 1<<16); err != nil || req.Size < 0 || req.Size > MaxDictateClip {
		_ = writeJSON(conn, DictateAnswer{Error: "the clip header is not one this hub takes"})
		return
	}
	wav := make([]byte, req.Size)
	if _, err := io.ReadFull(br, wav); err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, dictateWait)
	defer cancel()
	text, err := h.Dictate(cctx, name, wav, req.Words)
	switch {
	case errors.Is(err, ErrDictateOff):
		_ = writeJSON(conn, DictateAnswer{Off: true, Error: ErrDictateOff.Error()})
	case err != nil:
		_ = writeJSON(conn, DictateAnswer{Error: err.Error()})
	default:
		_ = writeJSON(conn, DictateAnswer{OK: true, Text: text})
	}
}
