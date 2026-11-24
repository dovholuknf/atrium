package cardurl

import "testing"

func TestPage(t *testing.T) {
	for path, want := range map[string]struct {
		page   string
		isCard bool
	}{
		"/alias/rnd":                   {"index.html", true},
		"/alias/RND":                   {"index.html", true},
		"/room/claude-room-e":          {"index.html", true},
		"/room/claude-room-e/rnd":      {"index.html", true},
		"/m/alias/rnd":                 {"m/index.html", true},
		"/m/room/claude-room-e/rnd":    {"m/index.html", true},
		"/sg3/terminal/release":        {"index.html", true},
		"/m/sg3/terminal/release":      {"m/index.html", true},
		"/room/terminal/x":             {"index.html", true},
		"/v1/terminal/x":               {"", false},
		"/sg3/terminal/":               {"", false},
		"/alias":                       {"", true},
		"/alias/":                      {"", true},
		"/alias/rnd/more":              {"", true},
		"/room/":                       {"", true},
		"/room/claude-room-e/":         {"", true},
		"/room/claude-room-e/rnd/more": {"", true},
		"/m/room/claude-room-e":        {"", true},
		"/":                            {"", false},
		"/index.html":                  {"", false},
		"/m/":                          {"", false},
		"/v1/tasks/rnd":                {"", false},
		"/v1/room/stats":               {"", false},
		"/js/solo.js":                  {"", false},
		"/aliases":                     {"", false},
		"/roomy/x":                     {"", false},
	} {
		page, isCard := Page(path)
		if page != want.page || isCard != want.isCard {
			t.Errorf("%s: %q %v, want %q %v", path, page, isCard, want.page, want.isCard)
		}
	}
}
