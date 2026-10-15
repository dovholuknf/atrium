package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHub = "http://127.0.0.1:7778"

// hubCall is one plain HTTP call to the hub. room, when set, rides X-Atrium-Room so the hub passes /v1/* to that
// room. A non-2xx answer is an error carrying the hub's own words, and is never retried.
func hubCall(hub, method, path, room string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(hub, "/")+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if room != "" {
		req.Header.Set("X-Atrium-Room", room)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return fmt.Errorf("hub refused %s %s: %d %s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// backlogDone marks a backlog item done: POST /_hub/backlog/<id> {"status","by"}.
func backlogDone(hub, id string) error {
	return hubCall(hub, http.MethodPost, "/_hub/backlog/"+url.PathEscape(id), "",
		map[string]string{"status": "done", "by": "devloop"}, nil)
}

// exitCard asks a room to exit a card's runner: POST /v1/tasks/<id>/exit with no body, which is the operator's ask.
func exitCard(hub, room, id string) error {
	return hubCall(hub, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/exit", room, nil, nil)
}

// splitCard reads "room~id", or a bare id with the room given separately.
func splitCard(card, room string) (string, string) {
	if r, id, ok := strings.Cut(card, "~"); ok {
		return r, id
	}
	return room, card
}
