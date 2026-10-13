// Package sqldoc provides source-local SQL intelligence without warehouse
// analysis. Its scanner retains source ranges while editing incomplete DDL.
package sqldoc

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type Token struct {
	Kind, Value   string
	Start, End    int
	Closed, Raw   bool
	Parent, Match int
}

type Document struct {
	Text   string
	Tokens []Token
	Tables []Table
}

func Parse(text string) *Document {
	d := &Document{Text: text, Tokens: scan(text)}
	d.Tables = d.tables()
	return d
}

func word(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 128
}

func scan(src string) []Token {
	var out []Token
	var stack []int
	for i := 0; i < len(src); {
		start := i
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		t := Token{Start: start, Parent: -1, Match: -1, Closed: true}
		if len(stack) > 0 {
			t.Parent = stack[len(stack)-1]
		}
		prefix := 0
		if strings.ContainsRune("rRbB", rune(c)) {
			if i+1 < len(src) && (src[i+1] == '\'' || src[i+1] == '"') {
				prefix = 1
			} else if i+2 < len(src) && strings.ContainsRune("rRbB", rune(src[i+1])) && strings.ToLower(src[i:i+1]) != strings.ToLower(src[i+1:i+2]) && (src[i+2] == '\'' || src[i+2] == '"') {
				prefix = 2
			}
		}
		switch {
		case strings.HasPrefix(src[i:], "--") || strings.HasPrefix(src[i:], "//") || c == '#':
			t.Kind = "comment"
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			// The insertion position before the newline still belongs to the comment.
			t.Closed = false
		case strings.HasPrefix(src[i:], "/*"):
			t.Kind = "comment"
			i += 2
			for i < len(src) && !strings.HasPrefix(src[i:], "*/") {
				i++
			}
			t.Closed = i < len(src)
			if t.Closed {
				i += 2
			}
		case c == '\'' || c == '"' || c == '`' || prefix > 0:
			p := strings.ToLower(src[i : i+prefix])
			t.Raw = strings.Contains(p, "r")
			i += prefix
			c = src[i]
			quote := string(c)
			t.Kind = "literal"
			if c == '`' {
				t.Kind = "quoted"
			} else if strings.Contains(p, "b") {
				t.Kind = "bytes"
			}
			if c != '`' && strings.HasPrefix(src[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			i += len(quote)
			valueStart := i
			for i < len(src) {
				if src[i] == '\\' {
					i++
					if i < len(src) {
						i++
					}
				} else if strings.HasPrefix(src[i:], quote) {
					break
				} else {
					i++
				}
			}
			t.Value = src[valueStart:i]
			t.Closed = i < len(src)
			if t.Closed {
				i += len(quote)
			}
		case word(c):
			t.Kind = "word"
			i++
			for i < len(src) && word(src[i]) {
				i++
			}
			t.Value = strings.ToUpper(src[start:i])
		default:
			t.Kind = string(c)
			t.Value = t.Kind
			i++
		}
		t.End = i
		if t.Kind == ")" || t.Kind == "]" {
			if len(stack) > 0 {
				open := stack[len(stack)-1]
				if out[open].Kind == "(" && t.Kind == ")" || out[open].Kind == "[" && t.Kind == "]" {
					stack = stack[:len(stack)-1]
					out[open].Match = len(out)
					t.Match = open
					t.Parent = out[open].Parent
				}
			}
		}
		out = append(out, t)
		if t.Kind == "(" || t.Kind == "[" {
			stack = append(stack, len(out)-1)
		}
		if t.Kind == ";" {
			stack = nil
		}
	}
	return out
}

func (d *Document) next(i int) int {
	for i++; i < len(d.Tokens) && d.Tokens[i].Kind == "comment"; i++ {
	}
	return i
}

func (d *Document) previous(i int) int {
	for i--; i >= 0 && d.Tokens[i].Kind == "comment"; i-- {
	}
	return i
}

func (d *Document) at(pos int, boundary bool) int {
	left := -1
	for i, t := range d.Tokens {
		if t.Start > pos {
			break
		}
		if t.Start <= pos && pos < t.End {
			if boundary && left >= 0 && d.Tokens[left].Kind == "word" && t.Kind != "word" && t.Kind != "quoted" && t.Kind != "literal" && t.Kind != "bytes" && t.Kind != "comment" {
				return left
			}
			return i
		}
		if pos == t.End && (boundary || !t.Closed) {
			left = i
		}
	}
	return left
}

func (d *Document) blocked(pos int, identifiers bool) bool {
	i := d.at(pos, false)
	if i < 0 {
		return false
	}
	k := d.Tokens[i].Kind
	return k == "literal" || k == "bytes" || k == "comment" || !identifiers && k == "quoted"
}

func decode(t Token) string {
	if t.Raw {
		return t.Value
	}
	var out strings.Builder
	for i := 0; i < len(t.Value); {
		if t.Value[i] != '\\' || i+1 == len(t.Value) {
			out.WriteByte(t.Value[i])
			i++
			continue
		}
		c := t.Value[i+1]
		escapes := map[byte]byte{'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '?': '?', '\'': '\'', '"': '"', '`': '`'}
		if value, ok := escapes[c]; ok {
			out.WriteByte(value)
			i += 2
			continue
		}
		n, base, start := 0, 16, i+2
		switch c {
		case 'x', 'X':
			n = 2
		case 'u':
			n = 4
		case 'U':
			n = 8
		default:
			if c >= '0' && c <= '7' {
				n = 3
				base = 8
				start = i + 1
			}
		}
		if n > 0 && start+n <= len(t.Value) {
			v, err := strconv.ParseInt(t.Value[start:start+n], base, 32)
			if err == nil && utf8.ValidRune(rune(v)) && (base != 8 || v <= 255) {
				out.WriteRune(rune(v))
				i = start + n
				continue
			}
		}
		out.WriteByte(t.Value[i])
		i++
	}
	return out.String()
}

func (d *Document) name(i int) string {
	if i < 0 || i >= len(d.Tokens) {
		return ""
	}
	t := d.Tokens[i]
	if t.Kind == "quoted" {
		return decode(t)
	}
	if t.Kind == "word" {
		return d.Text[t.Start:t.End]
	}
	return ""
}

func (d *Document) path(i int) (string, int) {
	name := d.name(i)
	if name == "" {
		return "", i
	}
	end := i
	// GoogleSQL permits dashes in the first table-path component.
	for {
		dash := d.next(end)
		if dash >= len(d.Tokens) || d.Tokens[dash].Kind != "-" || d.Tokens[dash].Start != d.Tokens[end].End {
			break
		}
		part := d.next(dash)
		if part >= len(d.Tokens) || d.Tokens[part].Kind != "word" || d.Tokens[part].Start != d.Tokens[dash].End {
			break
		}
		name += "-" + d.name(part)
		end = part
	}
	for {
		dot := d.next(end)
		if dot >= len(d.Tokens) || d.Tokens[dot].Kind != "." {
			break
		}
		part := d.next(dot)
		if d.name(part) == "" {
			break
		}
		name += "." + d.name(part)
		end = part
	}
	return name, end
}

func (d *Document) statementStart(i int) int {
	start := 0
	for j := 0; j < i; j++ {
		if d.Tokens[j].Kind == ";" {
			start = j + 1
		}
	}
	for start < len(d.Tokens) && d.Tokens[start].Kind == "comment" {
		start++
	}
	// A declaration within a script owns its own options.
	for j := start; j < i; j++ {
		switch d.Tokens[j].Value {
		case "CREATE", "EXPORT", "LOAD":
			start = j
		case "ALTER":
			next := d.next(j)
			if next < len(d.Tokens) && (d.Tokens[next].Value == "TABLE" || d.Tokens[next].Value == "VIEW" || d.Tokens[next].Value == "SCHEMA" || d.Tokens[next].Value == "MATERIALIZED") {
				start = j
			}
		}
	}
	return start
}
