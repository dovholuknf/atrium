package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// A terminal palette somebody brought with them, as opposed to one atrium
// shipped.
//
// THE TWO THEME SYSTEMS ARE STILL TWO. A skin is the colour of the board and
// there is one board, so it is a setting. A terminal theme belongs to one card
// and two terminals side by side may reasonably wear different ones, so the
// CHOICE lives on the card, in `task.theme`. What lives here is the other half:
// the palette that name refers to.
//
// WHY THE PALETTE IS ON THE DAEMON, when a grouping expression is refused
// storage three files away for looking like the same idea.
//
// The argument in `internal/api/settings.go` is not about where operator input
// is comfortable. It is about what the input IS. A grouping expression is
// compiled with `new Function` and runs with the whole page in scope, so
// storing one means one machine typing code that another machine runs. A
// colour is not code and cannot become code, for the reason below.
//
// And a terminal theme has a requirement a grouping expression does not: the
// name is ALREADY stored here, on the card, and has been since `0021_task_theme`.
// Keeping the palette in `localStorage` would mean a card whose theme resolves
// in the browser it was chosen in and nowhere else. That is the failure
// `KnownSkin` exists to refuse for skins: a setting that saved successfully and
// did nothing. So the definition follows the name, to the daemon, and a theme
// brought on a laptop is there on the phone.
//
// A COLOUR CANNOT BECOME CODE, and that is enforced here rather than at the
// HTTP boundary. `SaveTermTheme` normalises and refuses, so every route into
// storage goes through the same validator: the editor, the Windows Terminal
// import, and a configuration restored from a checkout. Anything that is not
// six hex digits behind a `#` never reaches the database, and what is written
// is the validator's own output rather than the bytes somebody sent.

// TermTheme is one named palette.
type TermTheme struct {
	Name    string  `json:"name"`
	Palette Palette `json:"palette"`
}

// Palette is what xterm.js is handed, in xterm.js's own field names.
//
// Stored in the consumer's vocabulary rather than Windows Terminal's, because
// the board hands this object straight to `term.options.theme`. The conversion
// from a Windows Terminal scheme happens once, on the way in, where the one
// trap in it can be tested: Windows Terminal calls magenta `purple`.
//
// The five above the ansi block are optional. Windows Terminal has no
// `selectionForeground` at all, and the board already reads every one of these
// with a fallback, so an absent value means "let the board decide" rather than
// black.
type Palette struct {
	Background          string `json:"background"`
	Foreground          string `json:"foreground"`
	Cursor              string `json:"cursor,omitempty"`
	SelectionBackground string `json:"selectionBackground,omitempty"`
	SelectionForeground string `json:"selectionForeground,omitempty"`

	Black   string `json:"black"`
	Red     string `json:"red"`
	Green   string `json:"green"`
	Yellow  string `json:"yellow"`
	Blue    string `json:"blue"`
	Magenta string `json:"magenta"`
	Cyan    string `json:"cyan"`
	White   string `json:"white"`

	BrightBlack   string `json:"brightBlack"`
	BrightRed     string `json:"brightRed"`
	BrightGreen   string `json:"brightGreen"`
	BrightYellow  string `json:"brightYellow"`
	BrightBlue    string `json:"brightBlue"`
	BrightMagenta string `json:"brightMagenta"`
	BrightCyan    string `json:"brightCyan"`
	BrightWhite   string `json:"brightWhite"`
}

// slot is one colour in a palette, its name as the operator typed it, and
// whether a theme without it is a theme.
type slot struct {
	name     string
	at       *string
	required bool
}

// slots is every colour, named, in the order the editor draws them.
//
// FIELD BY FIELD RATHER THAN BY REFLECTION, which is the same posture the
// configuration export takes and for the same reason. A field added to
// `Palette` next year is not validated until somebody writes a line here, and
// an unvalidated colour is the one thing this file exists to prevent. Missing
// from this list means missing from the editor too, which is how the omission
// gets noticed on the first screenshot rather than in a year.
func (p *Palette) slots() []slot {
	return []slot{
		{"background", &p.Background, true},
		{"foreground", &p.Foreground, true},
		{"cursor", &p.Cursor, false},
		{"selectionBackground", &p.SelectionBackground, false},
		{"selectionForeground", &p.SelectionForeground, false},

		{"black", &p.Black, true},
		{"red", &p.Red, true},
		{"green", &p.Green, true},
		{"yellow", &p.Yellow, true},
		{"blue", &p.Blue, true},
		{"magenta", &p.Magenta, true},
		{"cyan", &p.Cyan, true},
		{"white", &p.White, true},

		{"brightBlack", &p.BrightBlack, true},
		{"brightRed", &p.BrightRed, true},
		{"brightGreen", &p.BrightGreen, true},
		{"brightYellow", &p.BrightYellow, true},
		{"brightBlue", &p.BrightBlue, true},
		{"brightMagenta", &p.BrightMagenta, true},
		{"brightCyan", &p.BrightCyan, true},
		{"brightWhite", &p.BrightWhite, true},
	}
}

// PaletteSlots is the same list for anything that needs to state it rather
// than validate against it, which is the API describing itself to the editor.
func PaletteSlots() []struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
} {
	var p Palette
	out := make([]struct {
		Name     string `json:"name"`
		Required bool   `json:"required"`
	}, 0, 21)
	for _, s := range p.slots() {
		out = append(out, struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		}{s.name, s.required})
	}
	return out
}

// hexColour is the whole of what a colour may be.
//
// Six digits behind a `#`, and nothing else. No `rgb()`, no `hsl()`, no named
// colour, no three digit short form. Every one of those is a colour a browser
// would accept, and each one is a second grammar to be sure of, in a value
// that ends up in `style.setProperty` and in `term.options.theme`. The board
// has fifty two palettes written in this form already, Windows Terminal writes
// this form, and a rule with no exceptions is a rule that can be read in one
// line.
var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// themeName is what a theme may be called.
//
// Lowercase, digits and hyphens, starting and ending on something that is not
// a hyphen. The same shape every shipped theme already has, so a brought theme
// and a shipped one are told apart by nothing except which list they are in.
//
// THIRTY TWO RATHER THAN FORTY, and the reason is in `daemon/export.go`. The
// configuration export refuses itself if anything in it has the shape of a
// credential, and one of those shapes is forty or more alphanumeric characters
// in a row. A theme called `aaaa...` forty long is a legal name that would stop
// the whole export with an error about a key, on a machine nobody is watching.
// Thirty two is plenty for a name and cannot reach the threshold.
var themeName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

// reservedNames are the names a JavaScript object answers to whether or not
// anybody put them there.
//
// THE SECOND DEFENCE, and the first one is in the board: the merged theme table
// is built with a null prototype and read with an own-property check, so
// `TERM_THEMES.constructor` is no longer a function pretending to be a palette.
//
// This list is here anyway, because the two defences fail differently. The
// board's fix is one line in one function and the next function to look a theme
// up by name will be written by somebody who never read it. Refusing the name
// at the point of storage means there is nothing on the board to look up.
var reservedNames = map[string]bool{
	"constructor":    true,
	"prototype":      true,
	"proto":          true,
	"tostring":       true,
	"valueof":        true,
	"hasownproperty": true,
}

// Normalise checks a theme and rewrites it into the one form that gets stored.
//
// Called by `SaveTermTheme` rather than by its callers, so there is no route
// into the table that skips it.
func (t *TermTheme) Normalise() error {
	t.Name = strings.ToLower(strings.TrimSpace(t.Name))
	if t.Name == "" {
		return fmt.Errorf("a theme needs a name")
	}
	if !themeName.MatchString(t.Name) {
		return fmt.Errorf("%q is not a theme name. lowercase letters, digits and hyphens, "+
			"up to thirty two of them, the way every theme atrium ships is named", t.Name)
	}
	if reservedNames[t.Name] {
		return fmt.Errorf("%q cannot be a theme name. it is a name every javascript object "+
			"already answers to, so a card wearing it would look up a function instead of "+
			"a palette", t.Name)
	}
	for _, s := range t.Palette.slots() {
		v := strings.ToLower(strings.TrimSpace(*s.at))
		if v == "" {
			if s.required {
				return fmt.Errorf("%s has no %s, and a terminal needs one", t.Name, s.name)
			}
			*s.at = ""
			continue
		}
		if !hexColour.MatchString(v) {
			// The value is echoed because it is a colour the operator typed and
			// there is nothing to leak in it, and because "that is not a
			// colour" without saying which one sends somebody back to twenty
			// one boxes to find it.
			return fmt.Errorf("%s in %s is %q, and a colour here is six hex digits behind a "+
				"hash, like #1a2b3c", s.name, t.Name, v)
		}
		*s.at = v
	}
	return nil
}

// TermThemes is every brought theme, by name.
func (s *Store) TermThemes() ([]TermTheme, error) {
	var out []TermTheme
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT name, palette FROM term_theme ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = nil
		for rows.Next() {
			var name, raw string
			if err := rows.Scan(&name, &raw); err != nil {
				return err
			}
			var p Palette
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				// A row that will not parse is skipped rather than failing the
				// list. One unreadable palette must not be a board with no
				// themes on it, and the only way a row gets into this state is
				// somebody editing the database by hand.
				continue
			}
			out = append(out, TermTheme{Name: name, Palette: p})
		}
		return rows.Err()
	})
	return out, err
}

// TermThemeByName reads one, answering nil when there is no such theme.
func (s *Store) TermThemeByName(name string) (*TermTheme, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	var out *TermTheme
	err := s.guard(func() error {
		var raw string
		err := s.db.QueryRow(`SELECT palette FROM term_theme WHERE name = ?`, name).Scan(&raw)
		if err == sql.ErrNoRows {
			out = nil
			return nil
		}
		if err != nil {
			return err
		}
		var p Palette
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		out = &TermTheme{Name: name, Palette: p}
		return nil
	})
	return out, err
}

// SaveTermTheme writes one, having checked every colour in it.
//
// What is stored is the marshalled `Palette`, which is the validator's own
// output. Not the caller's bytes: a field nobody has heard of cannot ride along
// in the JSON and come back out on the next read.
func (s *Store) SaveTermTheme(t TermTheme) (*TermTheme, error) {
	if err := t.Normalise(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(t.Palette)
	if err != nil {
		return nil, err
	}
	err = s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO term_theme (name, palette, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET palette = excluded.palette,
			                                 updated_at = excluded.updated_at`,
			t.Name, string(raw), ts(now()), ts(now()))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteTermTheme forgets one.
//
// Cards wearing it are left alone rather than rewritten. `themeFor` already
// answers the project's own colour for a name it does not know, so a deleted
// theme costs those cards their colour and nothing else. Rewriting every card
// that mentioned it would be a delete that edits work, and undoing it means
// bringing the theme back and choosing it again on each one.
//
// Deleting a brought theme that shadows a shipped one puts the shipped one
// back, which is the only reset this needs.
func (s *Store) DeleteTermTheme(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM term_theme WHERE name = ?`, name)
		return err
	})
}
