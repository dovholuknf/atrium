package rules

import (
	"strconv"
	"strings"
)

// PRKey is the canonical key of a pull request across rooms, host/org/repo/number in lower case. The hub's claim
// table is keyed on the same string.
func PRKey(host, org, repo string, number int) string {
	return strings.ToLower(host + "/" + org + "/" + repo + "/" + strconv.Itoa(number))
}
