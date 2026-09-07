package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// A COLOUR CANNOT BECOME CODE.
//
// This file is the reason a terminal palette may be stored on the daemon while
// a grouping expression may not, and the argument only holds while the
// validator is total. Every test here names a way somebody makes it partial:
// by allowing a second colour grammar, by trusting the caller's JSON, or by
// adding a field and forgetting to check it.

func themeStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// good is a palette that passes, so a test about one field is about that field.
func good() Palette {
	var p Palette
	for _, s := range p.slots() {
		*s.at = "#123456"
	}
	return p
}

func TestOnlySixHexDigitsIsAColour(t *testing.T) {
	// Each of these is a colour some browser would render, and every one of
	// them is a second grammar somebody would then have to be sure of. The
	// last two are the ones that matter: they are what an injection looks like
	// when it arrives dressed as a colour.
	for _, bad := range []string{
		"red",
		"#abc",
		"rgb(1,2,3)",
		"hsl(120 50% 50%)",
		"#12345",
		"#1234567",
		"#12345g",
		"transparent",
		"var(--teal)",
		"#123456;background:url(http://elsewhere/x)",
		"url(javascript:alert(1))",
		"#123456 !important",
	} {
		th := TermTheme{Name: "brought", Palette: good()}
		th.Palette.Red = bad
		if err := th.Normalise(); err == nil {
			t.Errorf("%q was accepted as a colour. read the header in termtheme.go: the "+
				"whole reason this may be stored on the daemon is that the validator is total", bad)
		}
	}
}

// The list in `slots` is the validator. A field that is not in it is a colour
// nobody checks, and it would be stored and handed to the browser.
func TestEveryColourInAPaletteIsChecked(t *testing.T) {
	var p Palette
	for _, s := range p.slots() {
		th := TermTheme{Name: "brought", Palette: good()}
		for _, w := range th.Palette.slots() {
			if w.name == s.name {
				*w.at = "not-a-colour"
			}
		}
		if err := th.Normalise(); err == nil {
			t.Errorf("%s took a value that is not a colour, so it is missing from slots()", s.name)
		}
	}
}

func TestAThemeNeedsTheSixteenAnsiColours(t *testing.T) {
	var p Palette
	for _, s := range p.slots() {
		if !s.required {
			continue
		}
		th := TermTheme{Name: "brought", Palette: good()}
		for _, w := range th.Palette.slots() {
			if w.name == s.name {
				*w.at = ""
			}
		}
		if err := th.Normalise(); err == nil {
			t.Errorf("a theme with no %s was accepted, and a terminal has to draw something", s.name)
		}
	}
}

// Windows Terminal has no `selectionForeground`, so requiring it would refuse
// every scheme the format this feature exists for produces.
func TestTheThreeOptionalColoursMayBeAbsent(t *testing.T) {
	th := TermTheme{Name: "brought", Palette: good()}
	th.Palette.Cursor = ""
	th.Palette.SelectionBackground = ""
	th.Palette.SelectionForeground = ""
	if err := th.Normalise(); err != nil {
		t.Fatalf("a theme with no cursor or selection was refused: %v", err)
	}
}

// A name that every JavaScript object already answers to.
//
// `themeFor` looks a theme up by name and tests the answer for truthiness, so
// before the board's table was given a null prototype, a card set to
// `constructor` handed xterm a function. The board is fixed and this is the
// second defence: see `reservedNames`.
func TestANameEveryObjectAlreadyAnswersToIsRefused(t *testing.T) {
	for _, name := range []string{"constructor", "prototype", "toString", "valueOf", "__proto__"} {
		th := TermTheme{Name: name, Palette: good()}
		if err := th.Normalise(); err == nil {
			t.Errorf("%q was accepted as a theme name. a card wearing it looks up a function", name)
		}
	}
}

func TestAThemeNameIsLowercaseLettersDigitsAndHyphens(t *testing.T) {
	for _, name := range []string{
		"", "-leading", "trailing-", "has space", "has/slash", "has<tag>", "has\"quote",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // thirty five, past the limit
	} {
		th := TermTheme{Name: name, Palette: good()}
		if err := th.Normalise(); err == nil {
			t.Errorf("%q was accepted as a theme name", name)
		}
	}
}

// THIRTY TWO IS NOT ARBITRARY. The configuration export refuses itself when
// anything in it looks like a credential, and one of those shapes is forty
// alphanumeric characters in a row. A name that could reach forty would be a
// legal theme that stops the whole export with an error about a key.
func TestAThemeNameCannotReachTheExportSecretThreshold(t *testing.T) {
	th := TermTheme{Name: strings.Repeat("a", 40), Palette: good()}
	if err := th.Normalise(); err == nil {
		t.Fatal("a forty character name was accepted. it would trip the export's secret scanner")
	}
}

// What is stored is the validator's output. A caller cannot smuggle a field
// through by putting it in the JSON, because the JSON is never what is written.
func TestWhatComesBackIsWhatWasValidated(t *testing.T) {
	st := themeStore(t)
	in := TermTheme{Name: "Brought-IT", Palette: good()}
	in.Palette.Red = "#AABBCC"
	if _, err := st.SaveTermTheme(in); err != nil {
		t.Fatal(err)
	}
	// The name was lowercased on the way in, so it is found by the lowercase
	// one and by nothing else.
	got, err := st.TermThemeByName("brought-it")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the theme that was just saved is not there under its normalised name")
	}
	if got.Palette.Red != "#aabbcc" {
		t.Errorf("red came back as %q, wanted the normalised #aabbcc", got.Palette.Red)
	}
}

func TestABadThemeIsNotStoredAtAll(t *testing.T) {
	st := themeStore(t)
	bad := TermTheme{Name: "brought", Palette: good()}
	bad.Palette.BrightCyan = "chartreuse"
	if _, err := st.SaveTermTheme(bad); err == nil {
		t.Fatal("a palette with a word in it was saved")
	}
	got, err := st.TermThemeByName("brought")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("a refused theme was written anyway, so the refusal is after the write")
	}
}

// Saving the same name twice is an edit, which is what the editor does every
// time somebody changes a colour and presses save.
func TestSavingTheSameNameEditsIt(t *testing.T) {
	st := themeStore(t)
	first := TermTheme{Name: "brought", Palette: good()}
	if _, err := st.SaveTermTheme(first); err != nil {
		t.Fatal(err)
	}
	second := TermTheme{Name: "brought", Palette: good()}
	second.Palette.Green = "#00ff00"
	if _, err := st.SaveTermTheme(second); err != nil {
		t.Fatal(err)
	}
	all, err := st.TermThemes()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("saving over a name made %d themes, wanted 1", len(all))
	}
	if all[0].Palette.Green != "#00ff00" {
		t.Errorf("the edit did not stick: green is %q", all[0].Palette.Green)
	}
}

func TestDeletingAThemeLeavesTheOthers(t *testing.T) {
	st := themeStore(t)
	for _, n := range []string{"one", "two"} {
		if _, err := st.SaveTermTheme(TermTheme{Name: n, Palette: good()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteTermTheme("one"); err != nil {
		t.Fatal(err)
	}
	all, err := st.TermThemes()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Name != "two" {
		t.Fatalf("after deleting one, the themes are %v", all)
	}
}
