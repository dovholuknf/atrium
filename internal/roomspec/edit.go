package roomspec

import (
	"regexp"
	"strings"
)

// The editors are pure: lines in, lines out. A file is edited in place, so its other lines stay, a repeated key is cut to one,
// and the caller writes nothing when the lines are equal. They are the same edits the provision script made.

// bom is the UTF-8 byte order mark, which Windows editors put at the start of a file.
const bom = "\xef\xbb\xbf"

// Text is a config file as it was written: its lines, the line ending it used, and whether it began with a BOM. An edit changes
// the lines and writes the file back the way it came, so the diff of an edit is the line that changed.
type Text struct {
	Lines []string
	NL    string
	BOM   bool
}

// ParseText reads a file. defNL is the line ending of a file with none yet (the OS's). utf16 is true for a file that is UTF-16,
// which PowerShell 5.1's Out-File makes: it is neither read nor written as UTF-8, because that would corrupt it.
func ParseText(data []byte, defNL string) (t Text, utf16 bool) {
	if len(data) >= 2 && ((data[0] == 0xff && data[1] == 0xfe) || (data[0] == 0xfe && data[1] == 0xff)) {
		return Text{NL: defNL}, true
	}
	t = Text{NL: defNL, BOM: strings.HasPrefix(string(data), bom), Lines: SplitLines(data)}
	if i := strings.Index(string(data), "\n"); i >= 0 {
		t.NL = "\n"
		if i > 0 && data[i-1] == '\r' {
			t.NL = "\r\n"
		}
	}
	return t, false
}

// Bytes is the file to write.
func (t Text) Bytes() []byte {
	b := JoinLines(t.Lines, t.NL)
	if t.BOM && b != nil {
		b = append([]byte(bom), b...)
	}
	return b
}

// SplitLines reads a file's text as lines: a BOM, a CRLF and a final newline do not count.
func SplitLines(data []byte) []string {
	s := strings.TrimPrefix(string(data), bom)
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	return strings.Split(s, "\n")
}

// JoinLines writes lines with the OS's newline, ending in one. No BOM: the files are UTF-8 as they were.
func JoinLines(lines []string, nl string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, nl) + nl)
}

// EditKV sets key=val: the first line for the key is replaced, a later one is dropped, and a missing one is appended.
func EditKV(old []string, key, val string) []string {
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	line := key + "=" + val
	var out []string
	placed := false
	for _, l := range old {
		if re.MatchString(l) {
			if !placed {
				out = append(out, line)
				placed = true
			}
			continue
		}
		out = append(out, l)
	}
	if !placed {
		out = append(out, line)
	}
	return out
}

// EditINI sets key = val inside [sec]: another section is left as it is, a repeat of the key in the section is dropped, and a
// missing section is appended with it.
func EditINI(old []string, sec, key, val string) []string {
	anySec := regexp.MustCompile(`^\s*\[.*\]`)
	thisSec := regexp.MustCompile(`^\s*\[` + regexp.QuoteMeta(sec) + `\]\s*([;#].*)?$`)
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*[=:]`)
	line := key + " = " + val
	var out []string
	in, seen := false, false
	for _, l := range old {
		if anySec.MatchString(l) {
			in = thisSec.MatchString(l)
			out = append(out, l)
			if in && !seen {
				out = append(out, line)
				seen = true
			}
			continue
		}
		if in && keyRe.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	if !seen {
		out = append(out, "["+sec+"]", line)
	}
	return out
}

// ShellQuote is a value as one sh word: single quotes, and a quote inside it closed, escaped and reopened, so no value can end
// the word.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// EditProfile owns the one marked line for key: every marked line for it goes (another key's stays), and `export KEY='val'  # marker` is appended.
func EditProfile(old []string, key, val string) []string {
	var out []string
	for _, l := range old {
		if !(strings.HasSuffix(l, ProfileMarker) && strings.HasPrefix(l, "export "+key+"=")) {
			out = append(out, l)
		}
	}
	return append(out, "export "+key+"="+ShellQuote(val)+"  "+ProfileMarker)
}

// EqualLines is whether two files say the same, so an unchanged one is not written.
func EqualLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
