package cli

import (
	"strings"
	"testing"
)

func TestPickFlowRepo(t *testing.T) {
	one := []string{"github/o/a"}
	two := []string{"github/o/a", "github/o/b"}
	cases := []struct {
		name, set string
		held      []string
		want      string
		why       string
	}{
		{"set wins", " github/o/b ", two, "github/o/b", ""},
		{"one repo stands in", "", one, "github/o/a", ""},
		{"none", "", nil, "", "holds no repository"},
		{"several", "", two, "", "2 repositories"},
	}
	for _, c := range cases {
		got, why := pickFlowRepo(c.set, c.held)
		if got != c.want || (c.why == "") != (why == "") || !strings.Contains(why, c.why) {
			t.Errorf("%s: got %q, %q", c.name, got, why)
		}
		if why != "" && !strings.Contains(why, "flow_repo is unset") {
			t.Errorf("%s: reason %q does not say flow_repo is unset", c.name, why)
		}
	}
}
