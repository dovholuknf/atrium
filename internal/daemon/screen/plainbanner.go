package screen

import "strings"

// ReadsSGRAsMouse is every runner but claude: prompt_toolkit (aider) takes `ESC [ 0 m` as a urxvt mouse report and dies
// unpacking it, and no other runner is known to need the colour.
func ReadsSGRAsMouse(runner string) bool {
	return !strings.EqualFold(strings.TrimSpace(runner), "claude")
}
