package guard

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// The Bash tool's commands, read by mvdan.cc/sh. A script it cannot parse is
// read by the PowerShell tokenizer instead, which splits words and operators
// the same way for anything simple, so a typo is still checked rather than
// waved through.

type bashConv struct {
	s   *Script
	r   dirResolver
	src string
}

func parseBash(s *Script, r dirResolver, src, dir string) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		parsePwsh(s, r, src, dir)
		s.Shell = "bash"
		for _, c := range s.Cmds {
			c.Shell = "bash"
		}
		return
	}
	c := &bashConv{s: s, r: r, src: src}
	t := &tracker{r: r, dir: dir}
	for _, st := range f.Stmts {
		c.top(st, t)
	}
}

// top flattens one top-level statement into the chain.
func (c *bashConv) top(st *syntax.Stmt, t *tracker) {
	c.countSemicolon(st)
	sep := "\n"
	if st.Background {
		sep = "&"
	} else if st.Semicolon.IsValid() {
		sep = ";"
	}
	c.chain(st, sep, t)
}

func (c *bashConv) countSemicolon(st *syntax.Stmt) {
	if st.Semicolon.IsValid() && !st.Background {
		if o := int(st.Semicolon.Offset()); o < len(c.src) && c.src[o] == ';' {
			c.s.Semicolons++
		}
	}
}

// chain adds a statement to the top level, splitting && || and pipes.
func (c *bashConv) chain(st *syntax.Stmt, sep string, t *tracker) {
	if b, ok := st.Cmd.(*syntax.BinaryCmd); ok && len(st.Redirs) == 0 {
		op := map[syntax.BinCmdOperator]string{
			syntax.AndStmt: "&&", syntax.OrStmt: "||", syntax.Pipe: "|", syntax.PipeAll: "|",
		}[b.Op]
		c.chain(b.X, op, t)
		c.chain(b.Y, sep, t)
		return
	}
	if call, ok := st.Cmd.(*syntax.CallExpr); ok {
		cmd := c.call(call, st.Redirs, t.dir, false)
		if cmd != nil {
			c.s.Chain = append(c.s.Chain, Link{Cmd: cmd, Sep: sep})
			t.after(cmd, sep)
			return
		}
	}
	// Anything compound: its commands are checked, nested, and where it
	// leaves the shell is not followed.
	before := t.dir
	inner := &tracker{r: t.r, dir: t.dir}
	c.nested(st, inner)
	c.s.Chain = append(c.s.Chain, Link{Sep: sep})
	if inner.dir != before {
		t.dir = ""
	}
}

// nested walks a compound statement, adding every command in it.
func (c *bashConv) nested(st *syntax.Stmt, t *tracker) {
	if st == nil {
		return
	}
	c.countSemicolon(st)
	if _, ok := st.Cmd.(*syntax.CallExpr); !ok {
		c.redirs(st.Redirs, t.dir)
	}
	switch x := st.Cmd.(type) {
	case *syntax.CallExpr:
		cmd := c.call(x, st.Redirs, t.dir, true)
		sep := "\n"
		if st.Background {
			sep = "&"
		}
		t.after(cmd, sep)
	case *syntax.BinaryCmd:
		c.nested(x.X, t)
		if x.Op != syntax.AndStmt {
			t.dir = ""
		}
		c.nested(x.Y, t)
	case *syntax.Subshell:
		sub := &tracker{r: t.r, dir: t.dir}
		c.list(x.Stmts, sub)
	case *syntax.Block:
		c.list(x.Stmts, t)
	case *syntax.IfClause:
		for ic := x; ic != nil; ic = ic.Else {
			c.list(ic.Cond, t)
			c.list(ic.Then, t)
		}
	case *syntax.WhileClause:
		c.list(x.Cond, t)
		c.list(x.Do, t)
	case *syntax.ForClause:
		c.words(x.Loop, t.dir)
		c.list(x.Do, t)
	case *syntax.CaseClause:
		c.words(x.Word, t.dir)
		for _, it := range x.Items {
			c.list(it.Stmts, t)
		}
	case *syntax.FuncDecl:
		c.nested(x.Body, &tracker{r: t.r, dir: t.dir})
	case *syntax.TimeClause:
		c.nested(x.Stmt, t)
	case *syntax.CoprocClause:
		c.nested(x.Stmt, &tracker{r: t.r, dir: t.dir})
	case *syntax.DeclClause:
		for _, a := range x.Args {
			c.words(a, t.dir)
		}
	default:
		if x != nil {
			c.words(x, t.dir)
		}
	}
}

func (c *bashConv) list(sts []*syntax.Stmt, t *tracker) {
	for _, st := range sts {
		c.nested(st, t)
	}
}

// call converts a simple command. Nil when it is only assignments.
func (c *bashConv) call(x *syntax.CallExpr, rds []*syntax.Redirect, dir string, nested bool) *Cmd {
	cmd := &Cmd{Dir: dir, Nested: nested, Shell: "bash"}
	for _, a := range x.Assigns {
		v := ""
		if a.Value != nil {
			v = c.word(a.Value, dir).Lit
		}
		if a.Name != nil {
			cmd.Assigns = append(cmd.Assigns, a.Name.Value+"="+v)
		}
		if a.Array != nil {
			c.words(a.Array, dir)
		}
	}
	for _, w := range x.Args {
		cmd.Words = append(cmd.Words, c.word(w, dir))
	}
	cmd.Redirs = c.redirs(rds, dir)
	if len(cmd.Words) == 0 {
		return nil
	}
	c.s.Cmds = append(c.s.Cmds, cmd)
	return cmd
}

func (c *bashConv) redirs(rds []*syntax.Redirect, dir string) []Redir {
	var out []Redir
	for _, rd := range rds {
		r := Redir{Op: rd.Op.String()}
		if rd.N != nil {
			r.Op = rd.N.Value + r.Op
		}
		if rd.Word != nil {
			r.Target = c.word(rd.Word, dir)
		}
		if rd.Hdoc != nil {
			r.Body = c.word(rd.Hdoc, dir).Lit
		}
		out = append(out, r)
		c.s.Redirs = append(c.s.Redirs, r)
	}
	return out
}

// words adds the commands inside any substitution under n.
func (c *bashConv) words(n syntax.Node, dir string) {
	syntax.Walk(n, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.CmdSubst:
			c.list(x.Stmts, &tracker{r: c.r, dir: dir})
			return false
		case *syntax.ProcSubst:
			c.list(x.Stmts, &tracker{r: c.r, dir: dir})
			return false
		}
		return true
	})
}

// word is a word's value as bash would pass it, and the commands any
// substitution in it runs.
func (c *bashConv) word(w *syntax.Word, dir string) Word {
	var b strings.Builder
	dyn := false
	var part func(p syntax.WordPart, quoted bool)
	part = func(p syntax.WordPart, quoted bool) {
		switch x := p.(type) {
		case *syntax.Lit:
			if quoted {
				b.WriteString(unescapeDouble(x.Value))
			} else {
				b.WriteString(unescapeBare(x.Value))
			}
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				part(q, true)
			}
		case *syntax.CmdSubst:
			dyn = true
			b.WriteString("$(...)")
			c.list(x.Stmts, &tracker{r: c.r, dir: dir})
		case *syntax.ProcSubst:
			dyn = true
			c.list(x.Stmts, &tracker{r: c.r, dir: dir})
		default:
			dyn = true
			b.WriteString("$")
			c.words(x, dir)
		}
	}
	for _, p := range w.Parts {
		part(p, false)
	}
	return Word{Lit: b.String(), Dyn: dyn}
}

// unescapeBare drops the backslash bash removes outside quotes: `a\ b` is
// `a b`, and `C:\Users` is `C:Users`.
func unescapeBare(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			if s[i] == '\n' {
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescapeDouble drops a backslash only where bash does inside double quotes.
func unescapeDouble(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("$`\"\\\n", s[i+1]) >= 0 {
			i++
			if s[i] == '\n' {
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
