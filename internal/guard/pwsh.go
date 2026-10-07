package guard

import (
	"strings"
	"unicode"
)

// A tokenizer for the PowerShell tool's commands. Not a PowerShell parser: it
// knows quoting ('...', "...", here-strings, the backtick), the statement
// operators (newline ; && || | and a trailing &), redirections, comments, and
// the three ways a command nests inside another ((...), $(...), @(...) and
// {...}). That is enough to say which words are commands and which are
// arguments, which is all a rule asks.

type pwTok struct {
	src []rune
	i   int
	s   *Script
	r   dirResolver
}

func parsePwsh(s *Script, r dirResolver, src, dir string) {
	p := &pwTok{src: []rune(src), s: s, r: r}
	p.list(&tracker{r: r, dir: dir}, false, 0)
}

func (p *pwTok) eof() bool { return p.i >= len(p.src) }

func (p *pwTok) peek(off int) rune {
	if p.i+off < len(p.src) && p.i+off >= 0 {
		return p.src[p.i+off]
	}
	return 0
}

// list reads statements until `end` (0 for the end of input) and consumes it.
func (p *pwTok) list(t *tracker, nested bool, end rune) {
	cmd := &Cmd{Dir: t.dir, Nested: nested, Shell: "pwsh"}
	finish := func(sep string) {
		if len(cmd.Words) > 0 {
			p.s.Cmds = append(p.s.Cmds, cmd)
			if !nested {
				p.s.Chain = append(p.s.Chain, Link{Cmd: cmd, Sep: sep})
			}
			t.after(cmd, sep)
		}
		cmd = &Cmd{Dir: t.dir, Nested: nested, Shell: "pwsh"}
	}
	for {
		p.space()
		if p.eof() {
			finish("\n")
			return
		}
		ch := p.peek(0)
		switch {
		case end != 0 && ch == end:
			p.i++
			finish("\n")
			return
		case ch == ')' || ch == '}':
			// A closer with nothing open. Dropped, so the rest is still read.
			p.i++
		case ch == '\n':
			p.i++
			finish("\n")
		case ch == '#':
			p.lineComment()
		case ch == '<' && p.peek(1) == '#':
			p.blockComment()
		case ch == ';':
			p.i++
			finish(";")
		case ch == '&' && p.peek(1) == '&':
			p.i += 2
			finish("&&")
		case ch == '|' && p.peek(1) == '|':
			p.i += 2
			finish("||")
		case ch == '|':
			p.i++
			finish("|")
		case ch == '&':
			p.i++
			if len(cmd.Words) > 0 {
				finish("&")
			}
			// At the start of a statement it is the call operator.
		default:
			if op, ok := p.redirOp(); ok {
				rd := Redir{Op: op}
				if !strings.Contains(op, "&") {
					p.space()
					if !p.eof() && !p.isBreak(p.peek(0)) {
						rd.Target = p.word(t.dir)
					}
				}
				cmd.Redirs = append(cmd.Redirs, rd)
				p.s.Redirs = append(p.s.Redirs, rd)
				continue
			}
			cmd.Words = append(cmd.Words, p.word(t.dir))
		}
	}
}

func (p *pwTok) space() {
	for !p.eof() {
		ch := p.peek(0)
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r':
			p.i++
		case ch == '`' && (p.peek(1) == '\n' || p.peek(1) == '\r'):
			p.i += 2
			if p.peek(0) == '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

func (p *pwTok) lineComment() {
	for !p.eof() && p.peek(0) != '\n' {
		p.i++
	}
}

func (p *pwTok) blockComment() {
	p.i += 2
	for !p.eof() && !(p.peek(0) == '#' && p.peek(1) == '>') {
		p.i++
	}
	p.i += 2
}

// redirOp reads a redirection operator at the cursor: > >> 2> 2>> *> 2>&1 <.
func (p *pwTok) redirOp() (string, bool) {
	j := p.i
	if j < len(p.src) && (unicode.IsDigit(p.src[j]) || p.src[j] == '*') {
		j++
	}
	if j >= len(p.src) || (p.src[j] != '>' && p.src[j] != '<') {
		return "", false
	}
	if p.src[j] == '<' && j != p.i {
		return "", false
	}
	j++
	if j < len(p.src) && p.src[j] == '>' {
		j++
	}
	if j+1 < len(p.src) && p.src[j] == '&' && unicode.IsDigit(p.src[j+1]) {
		j += 2
	}
	op := string(p.src[p.i:j])
	p.i = j
	return op, true
}

func (p *pwTok) isBreak(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ';' || ch == '|' ||
		ch == '&' || ch == ')' || ch == '}'
}

// word reads one argument.
func (p *pwTok) word(dir string) Word {
	var b strings.Builder
	dyn := false
	start := p.i
	for !p.eof() {
		ch := p.peek(0)
		if p.isBreak(ch) {
			break
		}
		// A redirection glued to the end of a word: `foo>out`.
		if (ch == '>' || ch == '<') && p.i > start {
			break
		}
		switch {
		case ch == '\'':
			p.i++
			for !p.eof() {
				if p.peek(0) == '\'' {
					if p.peek(1) == '\'' {
						b.WriteRune('\'')
						p.i += 2
						continue
					}
					p.i++
					break
				}
				b.WriteRune(p.peek(0))
				p.i++
			}
		case ch == '"':
			p.i++
			if p.dquote(&b, dir, '"') {
				dyn = true
			}
		case ch == '@' && (p.peek(1) == '\'' || p.peek(1) == '"') && p.i == start:
			if p.hereString(&b, dir) {
				dyn = true
			}
		case ch == '`':
			p.i++
			if !p.eof() {
				b.WriteRune(p.peek(0))
				p.i++
			}
		case (ch == '$' || ch == '@') && p.peek(1) == '(':
			p.i += 2
			dyn = true
			b.WriteString("$(...)")
			p.list(&tracker{r: p.r, dir: dir}, true, ')')
		case ch == '(':
			p.i++
			dyn = true
			b.WriteString("(...)")
			p.list(&tracker{r: p.r, dir: dir}, true, ')')
		case ch == '{':
			p.i++
			dyn = true
			b.WriteString("{...}")
			p.list(&tracker{r: p.r, dir: dir}, true, '}')
		case ch == '$' && p.isVarStart(p.peek(1)):
			dyn = true
			b.WriteRune('$')
			p.i++
			p.varName(&b)
		default:
			b.WriteRune(ch)
			p.i++
		}
	}
	return Word{Lit: b.String(), Dyn: dyn}
}

func (p *pwTok) isVarStart(ch rune) bool {
	return ch == '{' || ch == '_' || ch == '?' || ch == '$' || ch == '^' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
}

func (p *pwTok) varName(b *strings.Builder) {
	if p.peek(0) == '{' {
		for !p.eof() && p.peek(0) != '}' {
			b.WriteRune(p.peek(0))
			p.i++
		}
		p.i++
		return
	}
	for !p.eof() {
		ch := p.peek(0)
		if ch == '_' || ch == ':' || ch == '?' || ch == '$' || ch == '^' || unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			b.WriteRune(ch)
			p.i++
			continue
		}
		return
	}
}

// dquote reads the rest of a double-quoted string. True when it expands
// something.
func (p *pwTok) dquote(b *strings.Builder, dir string, close rune) bool {
	dyn := false
	for !p.eof() {
		ch := p.peek(0)
		switch {
		case ch == close && close == '"' && p.peek(1) == '"':
			b.WriteRune('"')
			p.i += 2
		case ch == close:
			p.i++
			return dyn
		case ch == '`':
			p.i++
			if !p.eof() {
				b.WriteRune(p.peek(0))
				p.i++
			}
		case ch == '$' && p.peek(1) == '(':
			p.i += 2
			dyn = true
			b.WriteString("$(...)")
			p.list(&tracker{r: p.r, dir: dir}, true, ')')
		case ch == '$' && p.isVarStart(p.peek(1)):
			dyn = true
			b.WriteRune('$')
			p.i++
			p.varName(b)
		default:
			b.WriteRune(ch)
			p.i++
		}
	}
	return dyn
}

// hereString reads @'...'@ or @"..."@, whose closer must start a line.
func (p *pwTok) hereString(b *strings.Builder, dir string) bool {
	q := p.peek(1)
	p.i += 2
	for !p.eof() && p.peek(0) != '\n' {
		p.i++
	}
	p.i++
	var body strings.Builder
	for !p.eof() {
		if p.peek(0) == '\n' && p.peek(1) == q && p.peek(2) == '@' {
			p.i += 3
			break
		}
		if p.peek(0) == '\r' && p.peek(1) == '\n' && p.peek(2) == q && p.peek(3) == '@' {
			p.i += 4
			break
		}
		body.WriteRune(p.peek(0))
		p.i++
	}
	text := body.String()
	if q == '\'' {
		b.WriteString(text)
		return false
	}
	// An expanding here-string: its $(...) run.
	sub := &pwTok{src: []rune(text), s: p.s, r: p.r}
	return sub.dquote(b, dir, 0)
}
