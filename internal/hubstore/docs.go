package hubstore

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Hub documents: a place for what agents and the operator write. The design is
// docs/rnd/hub-documents-design.md and the API it serves is internal/link/docs_api.go.
//
// ── what lives where ────────────────────────────────────
//
// ROWS in SQLite (`doc`, `doc_version`, migration 0007) and BYTES in files named
// for their SHA-256 under `<hub data dir>/docs/`. Bytes are written to a temp file
// in that folder and renamed, never inside SQLite, so a ten-minute snapshot stays
// small and a half-written blob is never a version. Two versions with the same
// bytes are one file.
//
// ── one chokepoint ──────────────────────────────────────
//
// DocAdd is the only way a version is made, and it applies every rule: the
// switch, the size by kind, the secret content check, the per-card rate and the
// store's total. The API and the publish tool differ only in who they say wrote it.
//
// A VERSION IS APPENDED, never overwritten. Two writers cannot lose each other's
// work, and the newest wins on `/d/<slug>`.

// Default caps and the hub setting they are kept in.
const (
	SettingDocs = "docs_settings"

	DefaultDocTextCap     int64 = 5 << 20
	DefaultDocOtherCap    int64 = 20 << 20
	DefaultDocTotalCap    int64 = 2 << 30
	DefaultDocPerCardHour       = 30

	docSlugMax  = 60
	docTitleMax = 200
	docNameMax  = 255
)

// DocCaps are the limits the operator can change. The JSON names are the API's.
type DocCaps struct {
	Text        int64 `json:"text"`
	Other       int64 `json:"other"`
	Total       int64 `json:"total"`
	PerCardHour int   `json:"per_card_hour"`
}

// DocSettings is the publishing switch and the caps.
type DocSettings struct {
	Enabled bool    `json:"enabled"`
	Caps    DocCaps `json:"caps"`
}

func defaultDocSettings() DocSettings {
	return DocSettings{Enabled: true, Caps: DocCaps{
		Text: DefaultDocTextCap, Other: DefaultDocOtherCap, Total: DefaultDocTotalCap,
		PerCardHour: DefaultDocPerCardHour}}
}

// DocSettings reads the switch and the caps. A value that will not parse, or a
// cap that is not positive, reads as the default, so a bad write cannot turn the
// store into one that refuses everything or accepts without limit.
func (s *Store) DocSettings() (DocSettings, error) {
	out := defaultDocSettings()
	v, err := s.HubSetting(SettingDocs)
	if err != nil || strings.TrimSpace(v) == "" {
		return out, err
	}
	var got struct {
		Enabled *bool    `json:"enabled"`
		Caps    *DocCaps `json:"caps"`
	}
	if json.Unmarshal([]byte(v), &got) != nil {
		return out, nil
	}
	if got.Enabled != nil {
		out.Enabled = *got.Enabled
	}
	if got.Caps != nil {
		if got.Caps.Text > 0 {
			out.Caps.Text = got.Caps.Text
		}
		if got.Caps.Other > 0 {
			out.Caps.Other = got.Caps.Other
		}
		if got.Caps.Total > 0 {
			out.Caps.Total = got.Caps.Total
		}
		if got.Caps.PerCardHour > 0 {
			out.Caps.PerCardHour = got.Caps.PerCardHour
		}
	}
	return out, nil
}

// DocSettingsPatch is any subset of the settings. A nil field is left alone.
type DocSettingsPatch struct {
	Enabled *bool `json:"enabled"`
	Caps    *struct {
		Text        *int64 `json:"text"`
		Other       *int64 `json:"other"`
		Total       *int64 `json:"total"`
		PerCardHour *int   `json:"per_card_hour"`
	} `json:"caps"`
}

// SetDocSettings applies a patch and answers what is now in force. A cap that is
// not positive is refused rather than stored.
func (s *Store) SetDocSettings(p DocSettingsPatch) (DocSettings, error) {
	cur, err := s.DocSettings()
	if err != nil {
		return cur, err
	}
	if p.Enabled != nil {
		cur.Enabled = *p.Enabled
	}
	if c := p.Caps; c != nil {
		for name, v := range map[string]*int64{"text": c.Text, "other": c.Other, "total": c.Total} {
			if v != nil && *v <= 0 {
				return cur, &DocError{Kind: DocBad, Msg: "the " + name + " cap has to be more than zero"}
			}
		}
		if c.PerCardHour != nil && *c.PerCardHour <= 0 {
			return cur, &DocError{Kind: DocBad, Msg: "the per-card hourly cap has to be more than zero"}
		}
		if c.Text != nil {
			cur.Caps.Text = *c.Text
		}
		if c.Other != nil {
			cur.Caps.Other = *c.Other
		}
		if c.Total != nil {
			cur.Caps.Total = *c.Total
		}
		if c.PerCardHour != nil {
			cur.Caps.PerCardHour = *c.PerCardHour
		}
	}
	raw, _ := json.Marshal(cur)
	return cur, s.SetHubSetting(SettingDocs, string(raw))
}

// ── errors ──────────────────────────────────────────────

// DocErrKind says which refusal, and the API maps each to a status.
type DocErrKind int

const (
	DocBad       DocErrKind = iota + 1 // 400: unreadable, empty, a title with no text
	DocForbidden                       // 403: not the operator for this
	DocNone                            // 404: no such document or version
	DocGone                            // 410: tombstoned, purged or missing bytes
	DocTooBig                          // 413: over the cap for its kind
	DocSecret                          // 422: a secret rule, named in Rule
	DocRate                            // 429: a card past its hourly publishes
	DocOff                             // 503: publishing is turned off
	DocFull                            // 507: the store is at its total cap
)

// DocError is a refusal that is an answer, not a failure. The store does not halt
// on one.
type DocError struct {
	Kind DocErrKind
	Rule string
	Msg  string
}

func (e *DocError) Error() string { return e.Msg }

func docRefuse(kind DocErrKind, msg string) error {
	return refuse(&DocError{Kind: kind, Msg: msg})
}

// AsDocError unwraps one.
func AsDocError(err error) (*DocError, bool) {
	var de *DocError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// ── views ───────────────────────────────────────────────

// DocVersion is one version as the API shows it.
type DocVersion struct {
	N       int    `json:"n"`
	At      string `json:"at"`
	By      string `json:"by"`
	Origin  string `json:"origin"`
	Card    string `json:"card,omitempty"`
	Size    int64  `json:"size"`
	Kind    string `json:"kind"`
	Mime    string `json:"mime"`
	Name    string `json:"name"`
	SHA     string `json:"sha"`
	Missing bool   `json:"missing"`
	Purged  bool   `json:"purged"`
	// Override is not in the board's contract. It is kept so the history can say
	// a version went past a secret rule, and omitted when false.
	Override bool `json:"override,omitempty"`
}

// DocDeleted is a tombstone, or nil.
type DocDeleted struct {
	At string `json:"at"`
	By string `json:"by"`
}

// DocSummary is a row of the list.
type DocSummary struct {
	Slug     string      `json:"slug"`
	Title    string      `json:"title"`
	Created  string      `json:"created"`
	Updated  string      `json:"updated"`
	Versions int         `json:"versions"`
	Latest   DocVersion  `json:"latest"`
	Deleted  *DocDeleted `json:"deleted"`
}

// DocDetail is one document with every version, ascending.
type DocDetail struct {
	Slug     string       `json:"slug"`
	Title    string       `json:"title"`
	Created  string       `json:"created"`
	Deleted  *DocDeleted  `json:"deleted"`
	Versions []DocVersion `json:"versions"`
}

// DocUsage is what the store holds.
type DocUsage struct {
	Bytes    int64 `json:"bytes"`
	Docs     int   `json:"docs"`
	Versions int   `json:"versions"`
}

// DocLargest is a row of the settings' list of what takes the room.
type DocLargest struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
}

// ── slugs, names and kinds ──────────────────────────────

// reservedSlugs are names a route already answers, so a document cannot hide one.
var reservedSlugs = map[string]bool{"settings": true}

// DocSlugFor makes a slug from a title: lower case, every other run of
// characters one `-`, at most 60. Empty when nothing is left, and the caller
// falls back to `doc-<sha8>`.
func DocSlugFor(title string) string {
	var b strings.Builder
	dash := true // so a leading separator writes nothing
	for _, r := range strings.ToLower(title) {
		if r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return clipSlug(strings.Trim(b.String(), "-"), docSlugMax)
}

func clipSlug(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	return strings.Trim(s, "-")
}

// ValidDocSlug is the shape a slug in a URL has.
func ValidDocSlug(s string) bool {
	if s == "" || len(s) > docSlugMax {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// cleanTitle drops control characters and folds runs of space. A title is text a
// model wrote, and a newline in one is never wanted.
func cleanTitle(t string) string {
	var b strings.Builder
	space := false
	for _, r := range t {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := b.String()
	if utf8.RuneCountInString(out) > docTitleMax {
		out = string([]rune(out)[:docTitleMax])
	}
	return strings.TrimSpace(out)
}

// cleanName is the file name as given, minus any directory and control character.
func cleanName(n string) string {
	if i := strings.LastIndexAny(n, `/\`); i >= 0 {
		n = n[i+1:]
	}
	var b strings.Builder
	for _, r := range n {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len(out) > docNameMax {
		out = out[:docNameMax]
		for !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return out
}

// DocTitleFromName is the default title: the file name without its extension.
func DocTitleFromName(name string) string {
	name = cleanName(name)
	return cleanTitle(strings.TrimSuffix(name, filepath.Ext(name)))
}

// DocKind works out what the bytes are, from the name and the bytes. mime is
// what the hub decided, and for anything shown as text it is text/plain,
// whatever the extension says: HTML and SVG are stored as text and never
// rendered, and a label that said text/html would be an invitation.
func DocKind(name string, data []byte) (kind, mime string) {
	ext := strings.ToLower(filepath.Ext(name))
	sniff := ""
	if len(data) > 0 {
		head := data
		if len(head) > 512 {
			head = head[:512]
		}
		sniff = strings.SplitN(http.DetectContentType(head), ";", 2)[0]
	}
	switch sniff {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return "image", sniff
	}
	textual := isText(data)
	switch ext {
	case ".diff", ".patch":
		if textual {
			return "diff", "text/x-diff"
		}
	case ".md", ".markdown":
		if textual {
			return "markdown", "text/markdown"
		}
	}
	if !textual {
		return "other", "application/octet-stream"
	}
	if ext == "" && name == "" {
		// Nothing said what it is, and it is text: what an agent writes is markdown.
		return "markdown", "text/markdown"
	}
	return "text", "text/plain"
}

// isText is valid UTF-8 with no NUL in the part a person would read first.
func isText(b []byte) bool {
	head := b
	if len(head) > 8192 {
		head = head[:8192]
		// A cut can land in the middle of a character. Back up to a boundary.
		for len(head) > 0 && !utf8.Valid(head) && len(b)-len(head) < 4 {
			head = b[:len(head)-1]
		}
	}
	return utf8.Valid(head) && !strings.ContainsRune(string(head), 0)
}

// ── blobs ───────────────────────────────────────────────

func (s *Store) blobPath(sha string) string { return filepath.Join(s.docsDir, sha) }

// writeBlob puts the bytes where their hash says, through a temp file and a
// rename. One already there with the right size is left alone: same name, same
// bytes.
func (s *Store) writeBlob(sha string, data []byte) error {
	dst := s.blobPath(sha)
	if st, err := os.Stat(dst); err == nil && st.Size() == int64(len(data)) {
		return nil
	}
	if err := os.MkdirAll(s.docsDir, 0o700); err != nil {
		return err
	}
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(s.docsDir, ".tmp-"+hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func hexSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ── writing ─────────────────────────────────────────────

// DocInput is one version to add.
type DocInput struct {
	// Slug names an existing document to add a version to. Empty makes a new one.
	Slug string
	// Title is for a new document only. Empty takes it from Name.
	Title string
	Name  string
	Data  []byte

	// Origin, By and Card are decided by the hub from the request and never read
	// from a form. Origin is local, share or card, and Card is room~id.
	Origin string
	By     string
	Card   string

	// Override lets this one past a secret rule. The caller has checked who may.
	Override bool
	// Rate counts the write against its card's hourly publishes. Publish only.
	Rate bool
}

// DocResult is where a write landed.
type DocResult struct {
	Slug    string
	Version int
}

// DocAdd makes a new document or a new version, applying every rule.
func (s *Store) DocAdd(in DocInput) (DocResult, error) {
	var res DocResult
	set, err := s.DocSettings()
	if err != nil {
		return res, err
	}
	if !set.Enabled {
		return res, &DocError{Kind: DocOff, Msg: "publishing is turned off by the operator"}
	}
	switch in.Origin {
	case "local", "share", "card":
	default:
		return res, &DocError{Kind: DocBad, Msg: "a document needs an origin"}
	}
	if len(in.Data) == 0 {
		return res, &DocError{Kind: DocBad, Msg: "that file is empty"}
	}
	name := cleanName(in.Name)
	kind, mime := DocKind(name, in.Data)
	limit := set.Caps.Other
	if kind == "markdown" || kind == "text" || kind == "diff" {
		limit = set.Caps.Text
	}
	if int64(len(in.Data)) > limit {
		what := "text"
		if limit == set.Caps.Other && kind != "markdown" && kind != "text" && kind != "diff" {
			what = "a file of this kind"
		}
		return res, &DocError{Kind: DocTooBig, Msg: fmt.Sprintf(
			"that is %d bytes, over the %d byte cap for %s", len(in.Data), limit, what)}
	}
	if rule := SecretContent(in.Data); rule != "" && !in.Override {
		return res, &DocError{Kind: DocSecret, Rule: rule, Msg: "that looks like it holds a secret (" +
			rule + "). a document is readable by everyone past the board's password, so it was not stored. " +
			"this check is a speed bump and not a guarantee"}
	}
	sha := hexSHA(in.Data)
	title := cleanTitle(in.Title)
	if in.Slug == "" {
		if title == "" {
			title = DocTitleFromName(name)
		}
		if title == "" {
			if in.Title != "" {
				return res, &DocError{Kind: DocBad, Msg: "that title leaves no text"}
			}
			title = "doc-" + sha[:8]
		}
	}

	err = s.tx(func(t *sql.Tx) error {
		res = DocResult{}
		slug := in.Slug
		if slug != "" {
			var n int
			if err := t.QueryRow(`SELECT COUNT(*) FROM doc WHERE slug = ?`, slug).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return docRefuse(DocNone, "there is no document called "+slug)
			}
		}
		// THE TOTAL, counting a blob once however many versions hold it, and not
		// counting these bytes when they are already there.
		var used int64
		if err := t.QueryRow(usageSQL).Scan(&used); err != nil {
			return err
		}
		var have int
		if err := t.QueryRow(`SELECT COUNT(*) FROM doc_version WHERE sha = ? AND purged = 0`, sha).Scan(&have); err != nil {
			return err
		}
		if have == 0 && used+int64(len(in.Data)) > set.Caps.Total {
			return docRefuse(DocFull, fmt.Sprintf(
				"the document store is full: %d of %d bytes used. delete or purge documents in the gear to make room",
				used, set.Caps.Total))
		}
		if in.Rate && in.Card != "" {
			var recent int
			since := ts(now().Add(-time.Hour))
			if err := t.QueryRow(`SELECT COUNT(*) FROM doc_version WHERE card = ? AND at > ?`,
				in.Card, since).Scan(&recent); err != nil {
				return err
			}
			if recent >= set.Caps.PerCardHour {
				return docRefuse(DocRate, fmt.Sprintf(
					"this card has published %d documents in the last hour, which is the cap. try again later", recent))
			}
		}
		at := ts(now())
		if slug == "" {
			base := DocSlugFor(title)
			if base == "" {
				base = "doc-" + sha[:8]
			}
			var err error
			if slug, err = freeSlug(t, base); err != nil {
				return err
			}
			if _, err := t.Exec(`INSERT INTO doc (slug, title, created_at) VALUES (?, ?, ?)`,
				slug, title, at); err != nil {
				return err
			}
		}
		var n int
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) + 1 FROM doc_version WHERE doc = ?`, slug).Scan(&n); err != nil {
			return err
		}
		ov := 0
		if in.Override {
			ov = 1
		}
		// The blob first inside the transaction, so a failure to write it rolls the
		// row back. A blob left behind by a later failure is harmless: it is named for
		// its bytes and the next write of them reuses it.
		if err := s.writeBlob(sha, in.Data); err != nil {
			return fmt.Errorf("could not store the bytes: %w", err)
		}
		if _, err := t.Exec(`INSERT INTO doc_version
			(doc, n, at, by, origin, card, size, kind, mime, name, sha, purged, override)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
			slug, n, at, in.By, in.Origin, in.Card, len(in.Data), kind, mime, name, sha, ov); err != nil {
			return err
		}
		res = DocResult{Slug: slug, Version: n}
		return nil
	})
	return res, err
}

// usageSQL is the bytes held: each blob once, and not the purged.
const usageSQL = `SELECT COALESCE(SUM(sz), 0) FROM (
	SELECT MAX(size) AS sz FROM doc_version WHERE purged = 0 GROUP BY sha)`

// freeSlug is base, or base-2, base-3 and so on, whichever no document has. A
// tombstoned document keeps its slug, because its URL still answers.
func freeSlug(t *sql.Tx, base string) (string, error) {
	for i := 1; ; i++ {
		cand := base
		if i > 1 {
			suffix := fmt.Sprintf("-%d", i)
			cand = clipSlug(base, docSlugMax-len(suffix)) + suffix
		}
		if reservedSlugs[cand] {
			continue
		}
		var n int
		if err := t.QueryRow(`SELECT COUNT(*) FROM doc WHERE slug = ?`, cand).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return cand, nil
		}
	}
}

// DocRetitle renames. The slug never changes. Last write wins.
func (s *Store) DocRetitle(slug, title string) error {
	title = cleanTitle(title)
	if title == "" {
		return &DocError{Kind: DocBad, Msg: "that title leaves no text"}
	}
	return s.docExec(slug, `UPDATE doc SET title = ? WHERE slug = ?`, title, slug)
}

// DocDelete tombstones. Deleting twice is not an error.
func (s *Store) DocDelete(slug, by string) error {
	return s.docExec(slug, `UPDATE doc SET deleted_at = ?, deleted_by = ? WHERE slug = ?`, ts(now()), by, slug)
}

// DocRestore undoes a tombstone. A document with a purged version is restored
// by the operator only, and operator says whether this caller is.
func (s *Store) DocRestore(slug string, operator bool) error {
	return s.tx(func(t *sql.Tx) error {
		if err := docThere(t, slug); err != nil {
			return err
		}
		if !operator {
			var purged int
			if err := t.QueryRow(`SELECT COUNT(*) FROM doc_version WHERE doc = ? AND purged = 1`, slug).Scan(&purged); err != nil {
				return err
			}
			if purged > 0 {
				return docRefuse(DocForbidden, "this document has purged versions, so only the operator restores it, "+
					"from the machine the hub runs on")
			}
		}
		_, err := t.Exec(`UPDATE doc SET deleted_at = '', deleted_by = '' WHERE slug = ?`, slug)
		return err
	})
}

func docThere(t *sql.Tx, slug string) error {
	var n int
	if err := t.QueryRow(`SELECT COUNT(*) FROM doc WHERE slug = ?`, slug).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return docRefuse(DocNone, "there is no document called "+slug)
	}
	return nil
}

func (s *Store) docExec(slug, q string, args ...any) error {
	return s.tx(func(t *sql.Tx) error {
		if err := docThere(t, slug); err != nil {
			return err
		}
		_, err := t.Exec(q, args...)
		return err
	})
}

// DocPurge deletes the bytes of version n, or of every version when n is zero.
// The rows stay and say purged. A blob is one file for every version with the
// same bytes, so those versions are purged with it: they have no bytes left to
// show.
func (s *Store) DocPurge(slug string, n int) (int, error) {
	var shas []string
	err := s.tx(func(t *sql.Tx) error {
		shas = nil
		if err := docThere(t, slug); err != nil {
			return err
		}
		q, args := `SELECT DISTINCT sha FROM doc_version WHERE doc = ?`, []any{slug}
		if n > 0 {
			q += ` AND n = ?`
			args = append(args, n)
		}
		rows, err := t.Query(q, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sha string
			if err := rows.Scan(&sha); err != nil {
				rows.Close()
				return err
			}
			shas = append(shas, sha)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(shas) == 0 {
			return docRefuse(DocNone, fmt.Sprintf("there is no version %d of %s", n, slug))
		}
		for _, sha := range shas {
			if _, err := t.Exec(`UPDATE doc_version SET purged = 1 WHERE sha = ?`, sha); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, sha := range shas {
		if err := os.Remove(s.blobPath(sha)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return len(shas), fmt.Errorf("purged the rows but could not remove %s: %w", sha, err)
		}
	}
	return len(shas), nil
}

// ── reading ─────────────────────────────────────────────

const versionCols = `v.n, v.at, v.by, v.origin, v.card, v.size, v.kind, v.mime, v.name, v.sha, v.purged, v.override`

func (s *Store) scanVersion(r scanner, extra ...any) (DocVersion, error) {
	var v DocVersion
	var purged, override int
	dst := append([]any{&v.N, &v.At, &v.By, &v.Origin, &v.Card, &v.Size, &v.Kind, &v.Mime, &v.Name,
		&v.SHA, &purged, &override}, extra...)
	if err := r.Scan(dst...); err != nil {
		return v, err
	}
	v.Purged, v.Override = purged == 1, override == 1
	if !v.Purged {
		if _, err := os.Stat(s.blobPath(v.SHA)); err != nil {
			v.Missing = true
		}
	}
	return v, nil
}

func deletedOf(at, by string) *DocDeleted {
	if at == "" {
		return nil
	}
	return &DocDeleted{At: at, By: by}
}

// DocList is the documents, newest update first. Tombstones are left out, unless
// deleted is true, and then ONLY they are listed. card keeps documents that card
// wrote a version of, and q keeps titles holding the text, case-insensitively.
func (s *Store) DocList(q string, deleted bool, card string) ([]DocSummary, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []DocSummary
	err := s.guard(func() error {
		out = []DocSummary{}
		query := `SELECT ` + versionCols + `,
			d.slug, d.title, d.created_at, d.deleted_at, d.deleted_by,
			(SELECT COUNT(*) FROM doc_version c WHERE c.doc = d.slug)
			FROM doc d JOIN doc_version v ON v.doc = d.slug
			  AND v.n = (SELECT MAX(n) FROM doc_version m WHERE m.doc = d.slug)
			WHERE d.deleted_at ` + map[bool]string{false: `= ''`, true: `<> ''`}[deleted]
		args := []any{}
		if card != "" {
			query += ` AND EXISTS (SELECT 1 FROM doc_version w WHERE w.doc = d.slug AND w.card = ?)`
			args = append(args, card)
		}
		query += ` ORDER BY v.at DESC, d.slug`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DocSummary
			var delAt, delBy string
			v, err := s.scanVersion(rows, &d.Slug, &d.Title, &d.Created, &delAt, &delBy, &d.Versions)
			if err != nil {
				return err
			}
			if q != "" && !strings.Contains(strings.ToLower(d.Title), q) {
				continue
			}
			d.Latest, d.Updated, d.Deleted = v, v.At, deletedOf(delAt, delBy)
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// DocGet is one document with every version. A tombstoned document answers, with
// Deleted set.
func (s *Store) DocGet(slug string) (DocDetail, error) {
	var d DocDetail
	err := s.guard(func() error {
		var delAt, delBy string
		err := s.db.QueryRow(`SELECT slug, title, created_at, deleted_at, deleted_by FROM doc WHERE slug = ?`,
			slug).Scan(&d.Slug, &d.Title, &d.Created, &delAt, &delBy)
		if err == sql.ErrNoRows {
			return docRefuse(DocNone, "there is no document called "+slug)
		}
		if err != nil {
			return err
		}
		d.Deleted = deletedOf(delAt, delBy)
		d.Versions = []DocVersion{}
		rows, err := s.db.Query(`SELECT `+versionCols+` FROM doc_version v WHERE v.doc = ? ORDER BY v.n`, slug)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := s.scanVersion(rows)
			if err != nil {
				return err
			}
			d.Versions = append(d.Versions, v)
		}
		return rows.Err()
	})
	return d, err
}

// DocOpen opens the bytes of version n, or of the newest when n is zero. The
// caller closes the file.
//
// 404 for an unknown document or version. 410 for a tombstoned document, and for
// a purged or missing version, which says the bytes are missing.
func (s *Store) DocOpen(slug string, n int) (*os.File, DocVersion, error) {
	d, err := s.DocGet(slug)
	if err != nil {
		return nil, DocVersion{}, err
	}
	if d.Deleted != nil {
		return nil, DocVersion{}, &DocError{Kind: DocGone, Msg: "this document was deleted on " + d.Deleted.At}
	}
	if len(d.Versions) == 0 {
		return nil, DocVersion{}, &DocError{Kind: DocNone, Msg: "that document has no versions"}
	}
	v := d.Versions[len(d.Versions)-1]
	if n > 0 {
		found := false
		for _, c := range d.Versions {
			if c.N == n {
				v, found = c, true
			}
		}
		if !found {
			return nil, DocVersion{}, &DocError{Kind: DocNone, Msg: fmt.Sprintf("there is no version %d of %s", n, slug)}
		}
	}
	gone := &DocError{Kind: DocGone, Msg: "the bytes are missing"}
	if v.Purged || v.Missing {
		return nil, v, gone
	}
	f, err := os.Open(s.blobPath(v.SHA))
	if err != nil {
		v.Missing = true
		return nil, v, gone
	}
	return f, v, nil
}

// DocUsage is what the store holds and what takes the most room.
func (s *Store) DocUsage() (DocUsage, []DocLargest, error) {
	var u DocUsage
	large := []DocLargest{}
	err := s.guard(func() error {
		if err := s.db.QueryRow(usageSQL).Scan(&u.Bytes); err != nil {
			return err
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM doc`).Scan(&u.Docs); err != nil {
			return err
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM doc_version`).Scan(&u.Versions); err != nil {
			return err
		}
		rows, err := s.db.Query(`SELECT d.slug, d.title, COALESCE(SUM(v.size), 0) AS b
			FROM doc d JOIN doc_version v ON v.doc = d.slug AND v.purged = 0
			GROUP BY d.slug ORDER BY b DESC, d.slug LIMIT 10`)
		if err != nil {
			return err
		}
		defer rows.Close()
		large = large[:0]
		for rows.Next() {
			var l DocLargest
			if err := rows.Scan(&l.Slug, &l.Title, &l.Bytes); err != nil {
				return err
			}
			large = append(large, l)
		}
		return rows.Err()
	})
	return u, large, err
}

// ── the daily copy of the blobs ─────────────────────────

// docsCopyEvery is how often the blob folder is copied next to the snapshots.
const docsCopyEvery = 24 * time.Hour

// docsStamp is the file whose age says when the last copy ran.
const docsStamp = "docs.copied"

// CopyDocs mirrors the blob folder into `<dir>/docs`, once a day however often it
// is called. Blobs are named for their bytes, so a copy is the files not there
// yet. A blob whose rows are ALL purged is removed from the copy as well, because
// a purge that left the bytes in a backup would be a purge in name only. A blob
// merely missing from the live folder is NOT removed from the copy: that is the
// disaster the copy is for.
func (s *Store) CopyDocs(dir string) (copied int, err error) {
	stamp := filepath.Join(dir, docsStamp)
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < docsCopyEvery {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o700); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.docsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	var firstErr error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		dst := filepath.Join(dir, "docs", name)
		src := s.blobPath(name)
		si, err := os.Stat(src)
		if err != nil {
			continue
		}
		if di, err := os.Stat(dst); err == nil && di.Size() == si.Size() {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		copied++
	}
	// What was purged goes from the copy too.
	var purged []string
	if err := s.guard(func() error {
		purged = nil
		rows, err := s.db.Query(`SELECT sha FROM doc_version GROUP BY sha HAVING MIN(purged) = 1`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sha string
			if err := rows.Scan(&sha); err != nil {
				return err
			}
			purged = append(purged, sha)
		}
		return rows.Err()
	}); err != nil {
		return copied, err
	}
	sort.Strings(purged)
	for _, sha := range purged {
		_ = os.Remove(filepath.Join(dir, "docs", sha))
	}
	if firstErr != nil {
		// No stamp, so the next pass tries again.
		return copied, firstErr
	}
	return copied, os.WriteFile(stamp, []byte(ts(now())), 0o600)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
