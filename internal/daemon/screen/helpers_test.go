package screen

import (
	"fmt"
	"strings"
)

func render(in string, cols int) string {
	return string(RenderHistory([]byte(in), cols))
}

func lines(out string) []string {
	out = strings.TrimSuffix(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func plain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func conptyLine(n, cols int) string {
	text := fmt.Sprintf(" %d -    line %d of the diff", n, n)
	gap := cols - 7 - len(text)
	return fmt.Sprintf("\x1b[48;2;61;1;0m%s\x1b[%dX\x1b[m\x1b[%dC%s\r\n",
		text, gap, gap, strings.Repeat(" ", 7))
}
