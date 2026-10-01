package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// Hub documents, the HTTP half. The design is docs/rnd/hub-documents-design.md and the
// contract @ui builds to is docs/fabric/hub-documents-api.md.
//
//	GET  /_hub/docs?q=&deleted=1&card=   the list and the store's usage
//	POST /_hub/docs                      upload a NEW document, multipart
//	GET  /_hub/docs/settings             the switch, the caps, usage, the largest
//	PUT  /_hub/docs/settings             operator only
//	GET  /_hub/docs/<slug>               metadata and versions
//	GET  /_hub/docs/<slug>/raw?v=<n>     the bytes, always an attachment
//	POST /_hub/docs/<slug>/versions      a new version
//	POST /_hub/docs/<slug>/title         {"title": ...}
//	POST /_hub/docs/<slug>/delete        a tombstone
//	POST /_hub/docs/<slug>/restore       undo it
//	POST /_hub/docs/<slug>/purge?v=<n>   operator only
//
// THESE ARE THE HUB'S OWN AND ARE NOT PROXIED TO A ROOM, like notifyapi.go.
//
// ── who may do what ─────────────────────────────────────
//
// Reading and writing are the board's, which means anybody past the share's password:
// upload, a new version, a rename, a delete (a tombstone) and a restore. PURGE, THE CAPS,
// THE PUBLISHING SWITCH, AN OVERRIDE OF A SECRET RULE AND THE RESTORE OF A DOCUMENT THAT
// WAS PURGED are `edge.LocalOperator` only, with `edge.ProxyNote` in the refusal, because
// each is a decision about what the hub keeps and the share is not the machine.
//
// ── the origin is the hub's to say ──────────────────────
//
// A version records `local` when `edge.LocalOperator` accepted the request and `share`
// for everything else. It is never read from the form: a page cannot claim `local`, and
// only `local` may later be called the operator at the machine. A `card` origin comes
// only from atrium_publish, in-process, and has no HTTP spelling at all.
//
// ── every write checks the origin of the page ───────────
//
// The board's listener already wraps the hub in http.CrossOriginProtection, and every
// write route here asks again before it does anything else. Two layers, because a route
// added to a handler that was reached some other way, or a test that builds the proxy
// bare, must not be the hole. A request with a foreign `Origin` or `Sec-Fetch-Site:
// cross-site` is 403, on upload, version, title, delete, restore, purge and settings.

// SetDocs wires the document store. Optional: a hub without one answers /_hub/docs 404.
func (p *Proxy) SetDocs(st *hubstore.Store) {
	p.mu.Lock()
	p.docs = st
	p.mu.Unlock()
}

func (p *Proxy) docStore() *hubstore.Store {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.docs
}

// docFormOverhead is what a multipart body may carry beyond the file itself.
const docFormOverhead = 1 << 20

var docsCrossOrigin = http.NewCrossOriginProtection()

// docOrigin is what a request is, decided from the request alone.
func docOrigin(r *http.Request) string {
	if edge.LocalOperator(r) {
		return "local"
	}
	return "share"
}

func docJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func docSay(w http.ResponseWriter, code int, msg string) {
	docJSON(w, code, map[string]string{"error": msg})
}

// docStatus maps a refusal to the status the contract gives it.
var docStatus = map[hubstore.DocErrKind]int{
	hubstore.DocBad:       http.StatusBadRequest,
	hubstore.DocForbidden: http.StatusForbidden,
	hubstore.DocNone:      http.StatusNotFound,
	hubstore.DocGone:      http.StatusGone,
	hubstore.DocTooBig:    http.StatusRequestEntityTooLarge,
	hubstore.DocSecret:    http.StatusUnprocessableEntity,
	hubstore.DocRate:      http.StatusTooManyRequests,
	hubstore.DocOff:       http.StatusServiceUnavailable,
	hubstore.DocFull:      http.StatusInsufficientStorage,
}

func docFail(w http.ResponseWriter, err error) {
	de, ok := hubstore.AsDocError(err)
	if !ok {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			docSay(w, http.StatusRequestEntityTooLarge, "that upload is over the size cap")
			return
		}
		log.Printf("[hub] documents: %v", err)
		docSay(w, http.StatusInternalServerError, "the hub could not do that: "+err.Error())
		return
	}
	body := map[string]string{"error": de.Msg}
	if de.Rule != "" {
		body["rule"] = de.Rule
	}
	docJSON(w, docStatus[de.Kind], body)
}

// docOperatorOnly refuses a request that is not the machine's own user.
func docOperatorOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if edge.LocalOperator(r) {
		return true
	}
	docSay(w, http.StatusForbidden, what+" only from a browser or shell on the machine the hub runs on. "+
		"it is not available over the share"+edge.ProxyNote(r))
	return false
}

// serveDocs answers every /_hub/docs route.
func (p *Proxy) serveDocs(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.docStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "docs"), "/")
	parts := strings.Split(rest, "/")
	if rest == "" {
		parts = nil
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	// THE CROSS-ORIGIN CHECK COMES FIRST, before the route is even worked out, so a
	// write that is refused here cannot have been refused for some other reason, and one
	// that reaches a handler has passed it.
	if write {
		if err := docsCrossOrigin.Check(r); err != nil {
			docSay(w, http.StatusForbidden, "a page on another origin cannot write documents here")
			return
		}
	}
	notFound := func() { docSay(w, http.StatusNotFound, "there is no such documents route") }
	badMethod := func(want string) { docSay(w, http.StatusMethodNotAllowed, "that has to be a "+want) }

	switch {
	case len(parts) == 0:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.docsList(w, r, st)
		case http.MethodPost:
			p.docsUpload(w, r, st, "")
		default:
			badMethod("GET or a POST")
		}
	case parts[0] == "settings" && len(parts) == 1:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.docsSettings(w, r, st)
		case http.MethodPut:
			p.docsSetSettings(w, r, st)
		default:
			badMethod("GET or a PUT")
		}
	case !hubstore.ValidDocSlug(parts[0]) || len(parts) > 2:
		notFound()
	case len(parts) == 1:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			badMethod("GET")
			return
		}
		d, err := st.DocGet(parts[0])
		if err != nil {
			docFail(w, err)
			return
		}
		docJSON(w, http.StatusOK, d)
	default:
		slug := parts[0]
		switch parts[1] {
		case "raw":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				badMethod("GET")
				return
			}
			p.docsRaw(w, r, st, slug)
		case "versions", "title", "delete", "restore", "purge":
			if r.Method != http.MethodPost {
				badMethod("POST")
				return
			}
			switch parts[1] {
			case "versions":
				p.docsUpload(w, r, st, slug)
			case "title":
				p.docsRetitle(w, r, st, slug)
			case "delete":
				if err := st.DocDelete(slug, "operator"); err != nil {
					docFail(w, err)
					return
				}
				docJSON(w, http.StatusOK, map[string]any{"ok": true})
			case "restore":
				// Anybody may undo a tombstone, unless the document has a purged version,
				// and then the store says so and only the operator is let through.
				if err := st.DocRestore(slug, edge.LocalOperator(r)); err != nil {
					if de, ok := hubstore.AsDocError(err); ok && de.Kind == hubstore.DocForbidden {
						de.Msg += edge.ProxyNote(r)
					}
					docFail(w, err)
					return
				}
				p.RecordAudit("", "doc-restored", slug)
				docJSON(w, http.StatusOK, map[string]any{"ok": true})
			case "purge":
				p.docsPurge(w, r, st, slug)
			}
		default:
			notFound()
		}
	}
}

func (p *Proxy) docsList(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	docs, err := st.DocList(q.Get("q"), q.Get("deleted") == "1", strings.TrimSpace(q.Get("card")))
	if err != nil {
		docFail(w, err)
		return
	}
	set, err := st.DocSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	u, _, err := st.DocUsage()
	if err != nil {
		docFail(w, err)
		return
	}
	docJSON(w, http.StatusOK, map[string]any{
		"docs":  docs,
		"usage": map[string]int64{"bytes": u.Bytes, "cap": set.Caps.Total},
	})
}

func (p *Proxy) docsSettings(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	set, err := st.DocSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	u, large, err := st.DocUsage()
	if err != nil {
		docFail(w, err)
		return
	}
	docJSON(w, http.StatusOK, map[string]any{
		"operator": edge.LocalOperator(r), "enabled": set.Enabled, "caps": set.Caps,
		"usage": u, "largest": large,
	})
}

func (p *Proxy) docsSetSettings(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	if !docOperatorOnly(w, r, "the publishing switch and the caps are changed") {
		return
	}
	var patch hubstore.DocSettingsPatch
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&patch); err != nil {
		docSay(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	set, err := st.SetDocSettings(patch)
	if err != nil {
		docFail(w, err)
		return
	}
	p.RecordAudit("", "docs-settings", fmt.Sprintf("enabled=%v text=%d other=%d total=%d per_card_hour=%d",
		set.Enabled, set.Caps.Text, set.Caps.Other, set.Caps.Total, set.Caps.PerCardHour))
	p.docsSettings(w, r, st)
}

func (p *Proxy) docsRetitle(w http.ResponseWriter, r *http.Request, st *hubstore.Store, slug string) {
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body); err != nil {
		docSay(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if err := st.DocRetitle(slug, body.Title); err != nil {
		docFail(w, err)
		return
	}
	docJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Proxy) docsPurge(w http.ResponseWriter, r *http.Request, st *hubstore.Store, slug string) {
	// A purge through a proxy is refused here whatever the share's password: LocalOperator
	// fails on any forwarding header, so X-Forwarded-For set is 403.
	if !docOperatorOnly(w, r, "bytes are purged") {
		return
	}
	n := 0
	if v := r.URL.Query().Get("v"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			docSay(w, http.StatusBadRequest, "v has to be a version number")
			return
		}
	}
	count, err := st.DocPurge(slug, n)
	if err != nil {
		docFail(w, err)
		return
	}
	p.RecordAudit("", "doc-purged", fmt.Sprintf("%s v=%d", slug, n))
	docJSON(w, http.StatusOK, map[string]any{"ok": true, "purged": count})
}

// docsRaw serves the bytes, and ALWAYS as an attachment of octet-stream with nosniff. The
// client reads kind and mime from the metadata and renders from these bytes, never
// inline: a document is text a model or a visitor wrote, and served inline as HTML it
// would run on the board's own origin, which holds every card.
func (p *Proxy) docsRaw(w http.ResponseWriter, r *http.Request, st *hubstore.Store, slug string) {
	n := 0
	if v := r.URL.Query().Get("v"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			docSay(w, http.StatusBadRequest, "v has to be a version number")
			return
		}
	}
	f, v, err := st.DocOpen(slug, n)
	if err != nil {
		docFail(w, err)
		return
	}
	defer f.Close()
	title := slug
	if d, err := st.DocGet(slug); err == nil {
		title = d.Title
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", attachmentHeader(downloadName(title, v)))
	h.Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// downloadName is the title, with the extension of the file the author gave.
func downloadName(title string, v hubstore.DocVersion) string {
	ext := filepath.Ext(cleanHeaderText(v.Name))
	if ext == "" {
		switch v.Kind {
		case "markdown":
			ext = ".md"
		case "text":
			ext = ".txt"
		case "diff":
			ext = ".diff"
		}
	}
	if strings.EqualFold(filepath.Ext(title), ext) {
		ext = ""
	}
	return title + ext
}

// cleanHeaderText drops every control character, CR and LF among them, which is what
// keeps one header one header.
func cleanHeaderText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// attachmentHeader is a Content-Disposition by RFC 6266: an ASCII `filename=` with CR, LF,
// `"` and `\` taken out, and `filename*=UTF-8”` carrying the real name percent-encoded.
//
// A TITLE IS TEXT A MODEL WROTE. A raw CR or LF in it would end the header and start
// another, so both are removed from BOTH forms, and everything outside the unreserved
// set is percent-encoded in the second.
func attachmentHeader(name string) string {
	name = cleanHeaderText(name)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "document"
	}
	var ascii strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\':
			continue
		case r < 0x20 || r > 0x7e:
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}
	a := strings.TrimSpace(ascii.String())
	if a == "" {
		a = "document"
	}
	return `attachment; filename="` + a + `"; filename*=UTF-8''` + rfc5987(name)
}

// rfc5987 percent-encodes all but attr-char.
func rfc5987(s string) string {
	var b strings.Builder
	var buf [4]byte
	for _, r := range s {
		if r == utf8.RuneError {
			continue
		}
		n := utf8.EncodeRune(buf[:], r)
		for _, c := range buf[:n] {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
				strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
				b.WriteByte(c)
			default:
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}

// docsUpload takes one multipart form: `file` (required), `title` (a new document only)
// and `override` (operator only). slug is empty for a new document.
func (p *Proxy) docsUpload(w http.ResponseWriter, r *http.Request, st *hubstore.Store, slug string) {
	set, err := st.DocSettings()
	if err != nil {
		docFail(w, err)
		return
	}
	if !set.Enabled {
		docFail(w, &hubstore.DocError{Kind: hubstore.DocOff, Msg: "publishing is turned off by the operator"})
		return
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		docSay(w, http.StatusBadRequest, "send a multipart form with a file field")
		return
	}
	limit := set.Caps.Text
	if set.Caps.Other > limit {
		limit = set.Caps.Other
	}
	// BOUNDED AT THE LARGER CAP PLUS THE FORM, and cut off with 413 past it.
	r.Body = http.MaxBytesReader(w, r.Body, limit+docFormOverhead)
	mr := multipart.NewReader(r.Body, params["boundary"])

	var (
		data        []byte
		fileName    string
		haveFile    bool
		title, over string
		hasSlug     bool
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
			// The part is bounded by the body's bound above.
			if data, err = io.ReadAll(part); err != nil {
				docFail(w, readErr(err))
				return
			}
			fileName, haveFile = part.FileName(), true
		case "title", "override", "slug":
			b, err := io.ReadAll(io.LimitReader(part, 1<<14))
			if err != nil {
				docFail(w, readErr(err))
				return
			}
			switch part.FormName() {
			case "title":
				title = string(b)
			case "override":
				over = strings.TrimSpace(string(b))
			case "slug":
				hasSlug = true
			}
		}
	}
	if hasSlug && slug == "" {
		docSay(w, http.StatusBadRequest, "a new version of a document goes to /_hub/docs/<slug>/versions, "+
			"not to this route with a slug field")
		return
	}
	if !haveFile {
		docSay(w, http.StatusBadRequest, "there is no file field in that form")
		return
	}
	override := false
	switch over {
	case "", "0":
	case "1":
		// 403 from a non-operator, not ignored: a page that sends it is asking for
		// something it will not get, and silence would let it think it had.
		if !docOperatorOnly(w, r, "a secret rule is overridden") {
			return
		}
		override = true
	default:
		docSay(w, http.StatusBadRequest, "override is 1 or nothing")
		return
	}
	in := hubstore.DocInput{
		Slug: slug, Name: fileName, Data: data, Override: override,
		Origin: docOrigin(r), By: "operator",
	}
	if slug == "" {
		in.Title = title
	}
	res, err := st.DocAdd(in)
	if err != nil {
		docFail(w, err)
		return
	}
	if override {
		p.RecordAudit("", "doc-override", fmt.Sprintf("%s v%d went past a secret rule", res.Slug, res.Version))
	}
	docJSON(w, http.StatusCreated, map[string]any{
		"slug": res.Slug, "version": res.Version,
		"url":         "/d/" + res.Slug,
		"version_url": fmt.Sprintf("/d/%s@%d", res.Slug, res.Version),
	})
}

// readErr tells a body that was cut off from one that was unreadable.
func readErr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return err
	}
	return &hubstore.DocError{Kind: hubstore.DocBad, Msg: "could not read that form: " + err.Error()}
}

// ParseDocPath reads /d/<slug> and /d/<slug>@<n>. The split is on the LAST `@`, and a slug
// never holds one. n is 0 for the newest. False for anything else.
func ParseDocPath(p string) (slug string, n int, ok bool) {
	rest, found := strings.CutPrefix(p, "/d/")
	if !found || rest == "" {
		return "", 0, false
	}
	slug = rest
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		slug = rest[:i]
		ver := rest[i+1:]
		if ver == "" || ver[0] == '0' {
			return "", 0, false
		}
		for _, c := range ver {
			if c < '0' || c > '9' {
				return "", 0, false
			}
		}
		v, err := strconv.Atoi(ver)
		if err != nil || v < 1 {
			return "", 0, false
		}
		n = v
	}
	if !hubstore.ValidDocSlug(slug) {
		return "", 0, false
	}
	return slug, n, true
}
