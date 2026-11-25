package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/dictate"
)

// maxDictateClip is about four minutes of the 16 kHz mono WAV the board records.
const maxDictateClip = 8 << 20

// dictate turns a held push-to-talk clip into text for this card's input line. The board sends 16 kHz mono WAV and
// pastes what comes back. Audio goes only to this room's whisper-server, see internal/daemon/dictate.
func (s *Server) dictate(w http.ResponseWriter, r *http.Request) {
	if _, err := s.st.Get(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	if s.Dictate == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("dictation is not set up on this room"))
		return
	}
	t0 := time.Now()
	wav, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDictateClip))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("the clip is over %d MB", maxDictateClip>>20))
		return
	}
	if len(wav) < 44 || !bytes.HasPrefix(wav, []byte("RIFF")) || string(wav[8:12]) != "WAVE" {
		writeErr(w, http.StatusBadRequest, errors.New("send the clip as WAV"))
		return
	}
	t1 := time.Now()
	text, err := s.Dictate(r.Context(), wav, s.dictateWords())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	dictate.LogClip("room", len(wav), t1.Sub(t0), time.Since(t1), time.Since(t0), r.Header.Get(dictate.LastTrip),
		s.DictateFirst != nil && s.DictateFirst())
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}

// dictateReady answers whether a clip could be transcribed here, so the mic can refuse before anyone talks.
func (s *Server) dictateReady(w http.ResponseWriter, r *http.Request) {
	if s.Dictate == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false, "error": "dictation is not set up on this room"})
		return
	}
	if s.DictateOff != nil {
		if err := s.DictateOff(); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ready": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}

// dictateWords is the word list whisper is primed with: every card's handle and alias on this room, and its repo.
func (s *Server) dictateWords() []string {
	tasks, err := s.st.List()
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range tasks {
		out = append(out, t.WireName, t.Alias)
		if t.Repo != "" {
			out = append(out, path.Base(strings.ReplaceAll(t.Repo, "\\", "/")))
		}
	}
	return out
}

// SettingDictateServer is the path to whisper.cpp's whisper-server, and SettingDictateModel its ggml model.
const (
	SettingDictateServer = "dictate_server"
	SettingDictateModel  = "dictate_model"
)
