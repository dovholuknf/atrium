package docs

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The hub file drop, the board's half. The tools are files_mcp.go, the store is internal/hubstore/files.go and the
// design is docs/rnd/hub-file-drop.md.
//
//	GET  /_hub/files                      the list and the drop's usage
//	POST /_hub/files                      upload one file, multipart: file, note, ttl_days
//	GET  /_hub/files/settings             the caps, and whether the caller is the operator
//	PUT  /_hub/files/settings             operator only
//	GET  /_hub/files/<name>/raw           the bytes, always an attachment
//	POST /_hub/files/<name>/delete        remove it
//
// THESE ARE THE HUB'S OWN AND ARE NOT PROXIED TO A ROOM, like docs_api.go, and the rules are the same ones. Anybody
// past the board's password may list, download, upload and delete. The caps are the operator's, from the machine the
// hub runs on. The origin of an upload is the hub's to say, `local` or `share`, and never read from the form. Every
// write asks the cross-origin question first.

var filesCrossOrigin = http.NewCrossOriginProtection()

// ServeFiles answers every /_hub/files route.
func (p *Service) ServeFiles(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.DocStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "files"), "/")
	parts := strings.Split(rest, "/")
	if rest == "" {
		parts = nil
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	// THE CROSS-ORIGIN CHECK COMES FIRST, before the route is worked out, as ServeDocs does.
	if write {
		if err := filesCrossOrigin.Check(r); err != nil {
			docSay(w, http.StatusForbidden, "a page on another origin cannot write files here")
			return
		}
	}
	notFound := func() { docSay(w, http.StatusNotFound, "there is no such files route") }
	badMethod := func(want string) { docSay(w, http.StatusMethodNotAllowed, "that has to be a "+want) }

	switch {
	case len(parts) == 0:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.filesList(w, st)
		case http.MethodPost:
			p.filesUpload(w, r, st)
		default:
			badMethod("GET or a POST")
		}
	case len(parts) == 1 && parts[0] == "settings":
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.filesSettings(w, r, st)
		case http.MethodPut:
			p.filesSetSettings(w, r, st)
		default:
			badMethod("GET or a PUT")
		}
	case len(parts) == 2 && parts[1] == "raw":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			badMethod("GET")
			return
		}
		p.filesRaw(w, r, st, parts[0])
	case len(parts) == 2 && parts[1] == "delete":
		if r.Method != http.MethodPost {
			badMethod("POST")
			return
		}
		by := docBy(r)
		if err := st.FileDelete(parts[0]); err != nil {
			docFail(w, err)
			return
		}
		p.RecordAudit("", "file-deleted", parts[0]+" by "+by)
		docJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		notFound()
	}
}

func (p *Service) filesList(w http.ResponseWriter, st *hubstore.Store) {
	files, err := st.FileList("")
	if err != nil {
		docFail(w, err)
		return
	}
	set, err := st.FileSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	used, err := st.FileUsage()
	if err != nil {
		docFail(w, err)
		return
	}
	docJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"usage": map[string]int64{"bytes": used, "cap": set.Caps.Total},
	})
}

func (p *Service) filesSettings(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	set, err := st.FileSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	used, err := st.FileUsage()
	if err != nil {
		docFail(w, err)
		return
	}
	docJSON(w, http.StatusOK, map[string]any{
		"operator": edge.LocalOperator(r), "caps": set.Caps,
		"usage": map[string]int64{"bytes": used, "cap": set.Caps.Total},
	})
}

func (p *Service) filesSetSettings(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	if !docOperatorOnly(w, r, "the file drop's caps are changed") {
		return
	}
	var patch hubstore.FileSettings
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&patch); err != nil {
		docSay(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	set, err := st.SetFileSettings(patch.Caps)
	if err != nil {
		docFail(w, err)
		return
	}
	p.RecordAudit("", "files-settings", fmt.Sprintf("file=%d total=%d per_card_hour=%d ttl_days=%d max_ttl_days=%d",
		set.Caps.File, set.Caps.Total, set.Caps.PerCardHour, set.Caps.TTLDays, set.Caps.MaxTTLDays))
	p.filesSettings(w, r, st)
}

// filesRaw serves the bytes, and ALWAYS as an attachment of octet-stream with nosniff, so a hostile HTML file cannot
// run in the board's origin, which holds every card.
func (p *Service) filesRaw(w http.ResponseWriter, r *http.Request, st *hubstore.Store, name string) {
	f, row, err := st.FileOpen(name)
	if err != nil {
		docFail(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", AttachmentHeader(row.Name))
	h.Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// filesUpload takes one multipart form: `file` (required), `note` and `ttl_days`.
func (p *Service) filesUpload(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	set, err := st.FileSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		docSay(w, http.StatusBadRequest, "send a multipart form with a file field")
		return
	}
	// BOUNDED AT THE FILE CAP PLUS THE FORM, and cut off with 413 past it.
	r.Body = http.MaxBytesReader(w, r.Body, set.Caps.File+docFormOverhead)
	mr := multipart.NewReader(r.Body, params["boundary"])

	var (
		data     []byte
		fileName string
		haveFile bool
		note     string
		ttl      int
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			docFail(w, readErr(err))
			return
		}
		switch part.FormName() {
		case "file":
			if haveFile {
				docSay(w, http.StatusBadRequest, "send one file")
				return
			}
			if data, err = io.ReadAll(part); err != nil {
				docFail(w, readErr(err))
				return
			}
			fileName, haveFile = part.FileName(), true
		case "note", "ttl_days":
			b, err := io.ReadAll(io.LimitReader(part, 1<<14))
			if err != nil {
				docFail(w, readErr(err))
				return
			}
			if part.FormName() == "note" {
				note = string(b)
				break
			}
			if v := strings.TrimSpace(string(b)); v != "" {
				if ttl, err = strconv.Atoi(v); err != nil {
					docSay(w, http.StatusBadRequest, "ttl_days is a number of days")
					return
				}
			}
		}
	}
	if !haveFile {
		docSay(w, http.StatusBadRequest, "there is no file field in that form")
		return
	}
	by := docBy(r)
	row, err := st.FileAdd(hubstore.FileInput{
		Name: fileName, Note: note, Data: data, TTLDays: ttl, Origin: docOrigin(r), By: by,
	})
	if err != nil {
		docFail(w, err)
		return
	}
	p.RecordAudit("", "file-uploaded", row.Name+" by "+by)
	docJSON(w, http.StatusCreated, map[string]any{"name": row.Name, "size": row.Size, "expires": row.Expires})
}
