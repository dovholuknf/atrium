package api

import "strings"

// What the board is wearing.
//
// NOT A TERMINAL THEME, and the two are easy to confuse because both are
// called themes in ordinary speech. A terminal theme is sixteen ANSI colours
// handed to xterm.js, it belongs to one terminal, and there are fifty two of
// them. A skin is the colour of the board around the terminals.
//
// The difference that decides where each is stored: two terminals side by side
// may reasonably wear different themes, so a terminal theme is per screen. A
// skin cannot differ, because there is one board. So it is a daemon setting
// like the rest, and it follows to a second browser and to a phone.

const SettingBoardSkin = "board_skin"

// DefaultSkin is the palette in `:root`, which is the one the board came with.
//
// Named rather than empty so the picker has something to select. An empty
// setting means the same thing and is what a board that has never been asked
// reports, which is why `SkinOrDefault` treats them as one.
const DefaultSkin = "harbour"

// Skins is every skin the board can wear, in the order the picker shows them.
//
// DUPLICATED FROM THE STYLESHEET, and `scripts/check-skins.sh` fails if the two
// disagree. Parsing CSS out of the embedded board at startup to produce a list
// of ten strings is more machinery than the thing it produces; the check is
// what makes the duplication safe.
//
// Order is deliberate: the default first, then the two neutrals, then warm and
// cool alternating, so scrolling the picker does not read as a hue wheel.
var Skins = []string{
	DefaultSkin,
	// The darks, deepest family first.
	"graphite",
	"glacier",
	"abyss",
	"oxide",
	"moss",
	"plum",
	"ember",
	"vapor",
	"sandstone",
	"noir",
	// Lighter darks. Same idea, less depth, for a room with a window.
	"slate",
	"cobalt",
	"dusk",
	"fern",
	"clay",
	"mint",
	// LIGHT. These carry their own `--lift` and `--hairline`, which is what
	// made them possible: those are white at low alpha everywhere above, and
	// invisible on a pale background.
	"frost",
	"daylight",
	"paper",
	"linen",
}

// KnownSkin says whether a name is one atrium ships.
//
// This is checked on the way IN rather than on the way out, and the reason is
// that an unknown skin fails SILENTLY: the board writes the name onto the body
// element, no rule matches, and every variable falls through to the default.
// The result is a setting that saved successfully and did nothing, which is
// the worst answer available. Refusing at the boundary means the operator gets
// told.
func KnownSkin(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true // means the default, and clearing it is a request
	}
	for _, s := range Skins {
		if s == name {
			return true
		}
	}
	return false
}

// SkinOrDefault reads the stored skin, answering the default for both an
// unset setting and a stored name that is no longer shipped.
//
// The second case is the one worth handling: a skin removed from a later build
// would otherwise leave the board on a name nothing matches, which looks like
// the setting being ignored rather than the skin being gone.
func (s *Server) SkinOrDefault() string {
	v, err := s.st.Setting(SettingBoardSkin)
	if err != nil {
		return DefaultSkin
	}
	v = strings.TrimSpace(v)
	if v == "" || !KnownSkin(v) {
		return DefaultSkin
	}
	return v
}
