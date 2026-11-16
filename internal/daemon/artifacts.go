package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true}

// artifactKind sorts a changed file: an image by extension, a build by living under build.claude/, else a file.
func artifactKind(ref string) string {
	if imageExts[strings.ToLower(filepath.Ext(ref))] {
		return store.ArtifactImage
	}
	if strings.HasPrefix(ref, "build.claude/") {
		return store.ArtifactBuild
	}
	return store.ArtifactFile
}

// recordFileArtifact adds the file a Write, Edit or NotebookEdit changed to the card's list. Best effort: a hook
// never fails a session, so a failure here is one log line.
func (d *Daemon) recordFileArtifact(taskID string, in ActivityEvent) {
	if in.File == "" {
		return
	}
	t, err := d.st.Get(taskID)
	if err != nil {
		return
	}
	ref := filepath.ToSlash(in.File)
	root := strings.TrimRight(filepath.ToSlash(t.Worktree), "/")
	if root != "" && strings.HasPrefix(strings.ToLower(ref), strings.ToLower(root)+"/") {
		ref = ref[len(root)+1:]
	}
	a := store.Artifact{TaskID: taskID, Kind: artifactKind(ref), Ref: ref, Source: "hook"}
	if fi, err := os.Stat(in.File); err == nil {
		a.Bytes = fi.Size()
	}
	extra, _ := json.Marshal(map[string]any{"change": in.FileChange, "add": in.FileAdd, "del": in.FileDel})
	a.Extra = string(extra)
	if err := d.st.RecordArtifact(a); err != nil {
		log.Printf("[atrium] could not record an artifact for %s: %v", taskID, err)
	}
}
