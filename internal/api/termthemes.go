package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// Bringing a terminal theme, and editing one.
//
// NOT THE BOARD'S SKINS. `skins.go` has the difference and it decides this
// file: a skin is board-wide and is one setting, a terminal theme belongs to
// one card and the card already stores which one it wears. What was missing is
// the palette behind the name, which until now was a table in the board's own
// page and could only be added to by editing the source.
//
// The import format is Windows Terminal's, because that is where the shipped
// themes came from and it is the file people already have. Its schemes live in
// `settings.json` beside everything else, so the importer takes that whole file
// as happily as it takes one scheme.
//
// WHAT MAKES THIS SAFE is one line in `store/termtheme.go`: a colour is six hex
// digits behind a hash and there is no second grammar. Read the header there
// before adding a field here. The reason a colour may be stored on the daemon
// while a grouping expression may not is not that colours feel harmless, it is
// that a total validator exists for one and not for the other.

// themeImportLimit bounds what will be read as a Windows Terminal settings
// file.
//
// A settings.json with a hundred schemes and every profile somebody has ever
// made is tens of kilobytes. A megabyte is far past generous, and reading an
// arbitrary upload to find out it was not a settings file is how a request
// becomes a memory problem.
const themeImportLimit = 1 << 20

// getThemes answers the brought themes and the shape of one.
//
// The slots go out with the list so the editor draws its boxes from the
// daemon's idea of a palette rather than from a second list in the page. That
// is the same rule the skin picker follows, and for the same reason: a page
// offering a field the daemon does not validate, or missing one it requires,
// fails at the moment somebody presses save.
//
// THE FIFTY TWO SHIPPED THEMES ARE NOT HERE. They live in the page, they are
// the same in every browser, and copying them into the daemon would be a second
// list to keep in step for no gain. The board merges the two, and a brought
// theme with a shipped theme's name wins, which is how somebody gets THEIR
// dracula. Deleting it puts the shipped one back.
func (s *Server) getThemes(w http.ResponseWriter, r *http.Request) {
	themes, err := s.st.TermThemes()
	if err != nil {
		s.fail(w, err)
		return
	}
	if themes == nil {
		themes = []store.TermTheme{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"themes": themes,
		"slots":  store.PaletteSlots(),
	})
}

// putTheme writes one theme, named by the path.
//
// The name comes from the URL and not from the body, so editing `cocoa` and
// creating `cocoa` are the same request and cannot disagree about which theme
// they meant.
func (s *Server) putTheme(w http.ResponseWriter, r *http.Request) {
	var p store.Palette
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, themeImportLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := json.Unmarshal(body, &p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.st.SaveTermTheme(store.TermTheme{Name: r.PathValue("name"), Palette: p})
	if err != nil {
		// A 400 rather than a 500, always. Everything `SaveTermTheme` refuses
		// is something the operator typed and can fix, and the message names
		// which box.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("themes", map[string]string{"saved": saved.Name})
	writeJSON(w, http.StatusOK, saved)
}

// deleteTheme forgets one.
//
// Cards wearing it keep the name and fall back to their project's colour, which
// is what `themeFor` already does for a name it does not know. See
// `DeleteTermTheme`.
func (s *Server) deleteTheme(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.st.DeleteTermTheme(name); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("themes", map[string]string{"removed": name})
	writeJSON(w, http.StatusOK, map[string]string{"removed": name})
}

// importThemes reads Windows Terminal schemes.
//
// `?force=1` overwrites a theme that is already here. Without it an existing
// name is kept and reported, which is the same rule the configuration import
// follows: the dangerous direction never overwrites silently.
func (s *Server) importThemes(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, themeImportLimit))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	found, unnamed, err := schemesFrom(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	force := r.URL.Query().Get("force") == "1"

	imported := []string{}
	kept := []map[string]string{}
	for _, at := range unnamed {
		kept = append(kept, map[string]string{
			"name": at,
			"why":  "this scheme has no name atrium can use, and the name is how you pick it",
		})
	}
	for _, t := range found {
		if !force {
			have, err := s.st.TermThemeByName(t.Name)
			if err != nil {
				s.fail(w, err)
				return
			}
			if have != nil {
				kept = append(kept, map[string]string{
					"name": t.Name,
					"why":  "already here. import again with overwrite to replace it",
				})
				continue
			}
		}
		if _, err := s.st.SaveTermTheme(t); err != nil {
			// ONE BAD SCHEME DOES NOT LOSE THE OTHER FIFTY. A settings.json is
			// somebody's whole collection and one scheme in it with a colour
			// atrium will not take is not a reason to import none of them. It
			// is reported by name beside the ones that landed.
			kept = append(kept, map[string]string{"name": t.Name, "why": err.Error()})
			continue
		}
		imported = append(imported, t.Name)
	}
	if len(imported) > 0 {
		s.Broadcast("themes", map[string]int{"imported": len(imported)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": imported,
		"kept":     kept,
	})
}

// wtScheme is a Windows Terminal colour scheme, in Windows Terminal's own
// field names.
//
// THE TRAP IS `purple`. Windows Terminal calls the fifth ansi colour purple and
// xterm calls it magenta, and a scheme converted by hand puts it in the wrong
// slot without ever looking wrong: purple and magenta are the same colour, so
// the mistake shows up as every magenta in every session being the terminal's
// default instead. Named here once, mapped once, and tested.
//
// `cursorColor` is the other rename. Everything else matches.
type wtScheme struct {
	Name string `json:"name"`

	Background          string `json:"background"`
	Foreground          string `json:"foreground"`
	CursorColor         string `json:"cursorColor"`
	SelectionBackground string `json:"selectionBackground"`

	Black  string `json:"black"`
	Red    string `json:"red"`
	Green  string `json:"green"`
	Yellow string `json:"yellow"`
	Blue   string `json:"blue"`
	Purple string `json:"purple"`
	Cyan   string `json:"cyan"`
	White  string `json:"white"`

	BrightBlack  string `json:"brightBlack"`
	BrightRed    string `json:"brightRed"`
	BrightGreen  string `json:"brightGreen"`
	BrightYellow string `json:"brightYellow"`
	BrightBlue   string `json:"brightBlue"`
	BrightPurple string `json:"brightPurple"`
	BrightCyan   string `json:"brightCyan"`
	BrightWhite  string `json:"brightWhite"`
}

// theme converts one scheme, without validating it. `SaveTermTheme` does that,
// and doing it in two places is how the two answers drift apart.
func (w wtScheme) theme() store.TermTheme {
	return store.TermTheme{
		Name: SlugTheme(w.Name),
		Palette: store.Palette{
			Background:          w.Background,
			Foreground:          w.Foreground,
			Cursor:              w.CursorColor,
			SelectionBackground: w.SelectionBackground,
			// No `selectionForeground`. Windows Terminal has no such field, and
			// inventing one here would be atrium deciding what somebody's theme
			// looks like. The board falls back on its own.

			Black:   w.Black,
			Red:     w.Red,
			Green:   w.Green,
			Yellow:  w.Yellow,
			Blue:    w.Blue,
			Magenta: w.Purple,
			Cyan:    w.Cyan,
			White:   w.White,

			BrightBlack:   w.BrightBlack,
			BrightRed:     w.BrightRed,
			BrightGreen:   w.BrightGreen,
			BrightYellow:  w.BrightYellow,
			BrightBlue:    w.BrightBlue,
			BrightMagenta: w.BrightPurple,
			BrightCyan:    w.BrightCyan,
			BrightWhite:   w.BrightWhite,
		},
	}
}

// notNameable is everything a theme name may not contain, collapsed to hyphens.
var notNameable = regexp.MustCompile(`[^a-z0-9]+`)

// SlugTheme turns a scheme's display name into a theme name.
//
// `Campbell Powershell` becomes `campbell-powershell`. Windows Terminal names
// are free text with capitals, spaces, `+` and `()` in them, and the theme
// names atrium already uses are lowercase and hyphenated. Converting rather
// than refusing, because a person who has used `One Half Dark` for six years
// should not have to rename it to bring it.
func SlugTheme(name string) string {
	s := notNameable.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	// Truncated here rather than refused by the validator, because the length
	// limit is atrium's and the name is somebody else's. See `themeName`.
	if len(s) > 32 {
		s = strings.Trim(s[:32], "-")
	}
	return s
}

// schemesFrom finds the colour schemes in whatever was uploaded.
//
// FOUR SHAPES, because there are four things somebody plausibly pastes and
// telling them they pasted the wrong one is a worse answer than reading it:
//
//	a whole settings.json          an object with a `schemes` array in it
//	the schemes array on its own   a bare array
//	one scheme                     an object with colours in it
//	a fragment with a name         the same, and the name is used
//
// The array cases come first, because a settings.json IS an object and would
// otherwise be read as one very strange scheme with no colours in it.
// The second return is the schemes that were skipped for having no usable
// name, by position, which is the only handle such a scheme has.
func schemesFrom(raw []byte) ([]store.TermTheme, []string, error) {
	raw = stripJSONC(raw)

	var doc struct {
		Schemes []wtScheme `json:"schemes"`
	}
	if err := json.Unmarshal(raw, &doc); err == nil && len(doc.Schemes) > 0 {
		return named(doc.Schemes)
	}
	var list []wtScheme
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return named(list)
	}
	var one wtScheme
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, nil, fmt.Errorf("this is not json atrium can read as a colour scheme. paste a "+
			"windows terminal settings.json, its `schemes` array, or one scheme out of it: %w", err)
	}
	return named([]wtScheme{one})
}

// named converts the schemes and separates out the ones with no usable name.
//
// A scheme with no name is skipped rather than given one, because the name is
// how it is chosen afterwards and `theme-3` is not a thing anybody would pick
// on purpose.
func named(in []wtScheme) ([]store.TermTheme, []string, error) {
	var out []store.TermTheme
	var bad []string
	for i, w := range in {
		t := w.theme()
		if t.Name == "" {
			bad = append(bad, fmt.Sprintf("scheme #%d", i+1))
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("no colour scheme in that has a name atrium can use. a scheme " +
			"needs a `name` with a letter or a digit in it")
	}
	return out, bad, nil
}

// meaningful is the index of the next character that is neither whitespace nor
// part of a comment, at or after `from`.
func meaningful(raw []byte, from int) int {
	i := from
	for i < len(raw) {
		switch {
		case raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\r' || raw[i] == '\n':
			i++
		case raw[i] == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
		case raw[i] == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i += 2
		default:
			return i
		}
	}
	return len(raw)
}

// stripJSONC removes what Windows Terminal puts in its settings file and
// `encoding/json` will not read.
//
// A REAL settings.json HAS COMMENTS IN IT. Windows Terminal writes them itself,
// starting with the one at the top telling you to add settings below it, and
// people annotate the rest. Refusing the file that the format's own editor
// produces would make the import work only on a file somebody had cleaned up by
// hand first, which is most of the work this is supposed to save.
//
// A scanner rather than a regular expression, because the thing that goes
// wrong is `"commandline": "wsl.exe -d Ubuntu // notes"`, where the comment
// marker is inside a string. So this tracks whether it is in a string and
// whether the last character was an escape, which is the whole of the state a
// JSON string needs.
//
// Trailing commas are handled by the same pass, for the same reason: an editor
// that lets you comment out the last scheme leaves one behind.
func stripJSONC(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			// The newline itself is kept, so a comment cannot join the line
			// above to the line below.
			if i < len(raw) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i++
		case c == ',':
			// A comma is dropped when the next thing that means anything is a
			// closing bracket. Looked ahead rather than backtracked, because
			// what has already been written out may end in whitespace a comment
			// left behind.
			//
			// The lookahead steps over comments as well as space, and it has to:
			// the way a trailing comma appears in the first place is somebody
			// commenting out the last scheme in the list, which leaves the
			// comment sitting between the comma and the bracket.
			if j := meaningful(raw, i+1); j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
