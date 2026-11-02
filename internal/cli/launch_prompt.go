package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// namedPrompt resolves `--prompt @<name>` to the text of the room's named prompt (GET /v1/actions), matched by id or
// by label. Any other prompt is returned as given, so one that opens with an @file reference and goes on is still text.
func namedPrompt(boardURL, prompt string) (string, error) {
	name, ok := strings.CutPrefix(strings.TrimSpace(prompt), "@")
	if !ok || name == "" || strings.ContainsAny(name, " \t\r\n") {
		return prompt, nil
	}
	url := boardAddress(boardURL) + "/v1/actions"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("reading the named prompts: %s", resp.Status)
	}
	var got struct {
		Actions []struct {
			ID, Label, Prompt string
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&got); err != nil {
		return "", fmt.Errorf("reading the named prompts: %w", err)
	}
	var names []string
	for _, a := range got.Actions {
		if a.ID == name || strings.EqualFold(a.Label, name) {
			return a.Prompt, nil
		}
		names = append(names, a.ID)
	}
	return "", fmt.Errorf("--prompt @%s names no prompt. the named prompts are: %s", name, strings.Join(names, ", "))
}
