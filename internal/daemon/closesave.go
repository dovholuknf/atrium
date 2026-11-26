package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/base/docshare"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/store"
)

// A CLOSE'S LOOSE FILES: files in a card's home that no git holds, a REPORT.md the card left beside its work. A close
// saves them to the hub as one document and then removes the home. Only unique content is saved: BRIEF.md, atrium's own
// folders and build output are regenerable and never are. A file the hub cannot take as text, or one named like a secret,
// keeps the home with the reason, because a hub document is readable by everyone past the board's password.

const (
	closeSavePath     = "/_hub/publish"
	closeSaveFiles    = 20
	closeSaveFileMax  = 1 << 20
	closeSaveTotalMax = 4 << 20
	closeSaveWait     = 60 * time.Second
)

var regenerableDirs = map[string]bool{
	"node_modules": true, ".cache": true, "__pycache__": true, "build": true, "build.claude": true, "dist": true,
	"target": true, ".venv": true, "venv": true, home.HomeMeta: true, home.HomeTmp: true,
}

// homeLooseFiles lists the files of a home worth saving, or why the home must stay: a git checkout in it is the
// close's to answer for, and a file that is not saveable text is not ours to delete.
func homeLooseFiles(dir string) (files []capFile, why string) {
	var total int64
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || why != "" {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case e.Name() == ".git":
			why = "it holds a git checkout at " + filepath.ToSlash(filepath.Dir(rel))
			return fs.SkipAll
		case e.IsDir():
			if regenerableDirs[e.Name()] {
				return fs.SkipDir
			}
			return nil
		case rel == "BRIEF.md" || rel == home.HomeWork+"/BRIEF.md":
			return nil
		case !e.Type().IsRegular():
			why = "it holds " + rel + ", which is not a plain file"
			return fs.SkipAll
		}
		info, err := e.Info()
		switch {
		case err != nil:
			why = "it holds " + rel + ", which could not be read"
		case docshare.SecretFileName(rel):
			why = "it holds " + rel + ", which is named like a secret and is not saved"
		case info.Size() > closeSaveFileMax:
			why = fmt.Sprintf("it holds %s, over the %d bytes a saved file may be", rel, closeSaveFileMax)
		case len(files) >= closeSaveFiles:
			why = fmt.Sprintf("it holds more than %d files", closeSaveFiles)
		}
		if total += info.Size(); why == "" && total > closeSaveTotalMax {
			why = fmt.Sprintf("its files are over %d bytes together", closeSaveTotalMax)
		}
		if why != "" {
			return fs.SkipAll
		}
		files = append(files, capFile{rel: rel, abs: p, size: info.Size()})
		return nil
	})
	if why != "" {
		return nil, why
	}
	return files, ""
}

// saveHomeFiles publishes the files as one hub document under the card and answers why it could not, "" when it did.
func (d *Daemon) saveHomeFiles(ctx context.Context, t *store.Task, files []capFile) string {
	var body strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f.abs)
		if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return "it holds " + f.rel + ", which is not text the hub document can take"
		}
		if len(files) > 1 {
			fmt.Fprintf(&body, "===== %s =====\n", f.rel)
		}
		body.Write(data)
		if len(files) > 1 {
			body.WriteString("\n")
		}
	}
	ha, _ := d.Relay().(hublink.HubAsker)
	if ha == nil {
		return "it holds files and this room has no link to a hub to save them to"
	}
	raw, _ := json.Marshal(map[string]string{"title": "Files saved when " + t.DisplayTitle() + " was closed", "content": body.String()})
	ctx, cancel := context.WithTimeout(ctx, closeSaveWait)
	defer cancel()
	res, err := ha.Hub(ctx, t.WireName, http.MethodPost, closeSavePath, raw)
	switch {
	case err != nil:
		return "its files did not save to the hub: " + err.Error()
	case !res.OK:
		return "its files did not save to the hub: " + res.Error
	}
	var got struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(res.Data, &got)
	log.Printf("[atrium] close saved %d file(s) of %s to the hub as %s", len(files), t.DisplayTitle(), got.URL)
	_ = d.st.AppendEvent(t.ID, store.EventNotified, map[string]any{"by": "close-saved", "files": len(files), "url": got.URL})
	return ""
}
