package sqldoc

import "strings"

type Definition struct{ Start, End int }

func (d *Document) LocalDefinition(pos int) *Definition {
	i := d.at(pos, true)
	if i < 0 || d.blocked(pos, true) {
		return nil
	}
	// Partition/clustering references resolve only against top-level columns.
	for _, t := range d.Tables {
		if t.Close < 0 || pos < d.Tokens[t.Close].End || pos > t.End {
			continue
		}
		clause := ""
		for j := t.Close + 1; j <= i; j++ {
			switch d.Tokens[j].Value {
			case "PARTITION", "CLUSTER":
				clause = d.Tokens[j].Value
			case "OPTIONS", "AS":
				clause = ""
			}
		}
		next := d.next(i)
		if clause == "" || next < len(d.Tokens) && d.Tokens[next].Kind == "(" {
			continue
		}
		for _, c := range t.Columns {
			if strings.EqualFold(c.Name, d.name(i)) {
				return &Definition{c.NameStart, c.NameEnd}
			}
		}
	}
	ref := d.TableReference(pos)
	if ref != "" {
		if cte := d.cte(ref, pos); cte != nil {
			return cte
		}
	}
	// Aliases are scoped to their enclosing query parentheses.
	previous := d.previous(i)
	if previous >= 0 && d.Tokens[previous].Kind == "." {
		return nil
	}
	target := d.name(i)
	if target == "" {
		return nil
	}
	start, stop := d.statementBounds(pos)
	var aliasDefinition *Definition
	bestDepth := -1
	for j, t := range d.Tokens {
		if t.Value != "FROM" && t.Value != "JOIN" {
			continue
		}
		if t.Start < start || t.Start >= stop || !d.relation(j) {
			continue
		}
		first := d.next(j)
		_, end := d.path(first)
		if end == first && d.name(first) == "" {
			continue
		}
		alias := d.next(end)
		if alias < len(d.Tokens) && d.Tokens[alias].Value == "AS" {
			alias = d.next(alias)
		}
		if alias >= len(d.Tokens) || !strings.EqualFold(d.name(alias), target) || d.Tokens[alias].Kind != "word" && d.Tokens[alias].Kind != "quoted" {
			continue
		}
		if _, reserved := keywords[d.Tokens[alias].Value]; reserved && d.Tokens[alias].Kind == "word" {
			continue
		}
		parent := t.Parent
		depth := 0
		visible := false
		for scope := d.Tokens[i].Parent; ; scope = d.Tokens[scope].Parent {
			if scope == parent {
				visible = true
				break
			}
			if scope < 0 {
				break
			}
		}
		if !visible {
			continue
		}
		for scope := parent; scope >= 0; scope = d.Tokens[scope].Parent {
			depth++
		}
		if ref != "" {
			continue
		}
		if depth > bestDepth {
			aliasDefinition = &Definition{d.Tokens[alias].Start, d.Tokens[alias].End}
			bestDepth = depth
		}
	}
	return aliasDefinition
}

func (d *Document) TableReference(pos int) string {
	for i, t := range d.Tokens {
		if t.Kind != "word" {
			continue
		}
		switch t.Value {
		case "FROM", "JOIN", "LIKE", "CLONE", "COPY", "REFERENCES", "UPDATE", "INTO":
		default:
			continue
		}
		if !d.relation(i) {
			continue
		}
		start := d.next(i)
		name, end := d.path(start)
		if name == "" {
			continue
		}
		next := d.next(end)
		if next < len(d.Tokens) && d.Tokens[next].Kind == "(" && t.Value != "REFERENCES" {
			continue
		}
		if d.Tokens[start].Start <= pos && pos <= d.Tokens[end].End {
			return name
		}
	}
	return ""
}

func (d *Document) cte(name string, pos int) *Definition {
	var result *Definition
	bestStart := -1
	for i, t := range d.Tokens {
		if t.Kind != "word" || t.Value != "WITH" || t.Start > pos || t.Start < bestStart {
			continue
		}
		stop := len(d.Text)
		if t.Parent >= 0 && d.Tokens[t.Parent].Match >= 0 {
			stop = d.Tokens[d.Tokens[t.Parent].Match].Start
		}
		for j := i + 1; j < len(d.Tokens) && d.Tokens[j].Start < stop; j++ {
			if d.Tokens[j].Kind == ";" {
				stop = d.Tokens[j].Start
				break
			}
		}
		if pos > stop {
			continue
		}
		j := d.next(i)
		recursive := false
		if j < len(d.Tokens) && d.Tokens[j].Value == "RECURSIVE" {
			recursive = true
			j = d.next(j)
		}
		for j < len(d.Tokens) {
			alias := j
			if d.name(alias) == "" {
				break
			}
			as := d.next(alias)
			if as < len(d.Tokens) && d.Tokens[as].Kind == "(" && d.Tokens[as].Match >= 0 {
				as = d.next(d.Tokens[as].Match)
			}
			if as >= len(d.Tokens) || d.Tokens[as].Value != "AS" {
				break
			}
			open := d.next(as)
			if open >= len(d.Tokens) || d.Tokens[open].Kind != "(" || d.Tokens[open].Match < 0 {
				break
			}
			close := d.Tokens[open].Match
			visible := recursive || pos > d.Tokens[close].End
			if visible && strings.EqualFold(d.name(alias), name) {
				result = &Definition{d.Tokens[alias].Start, d.Tokens[alias].End}
				bestStart = t.Start
			}
			comma := d.next(close)
			if comma >= len(d.Tokens) || d.Tokens[comma].Kind != "," {
				break
			}
			j = d.next(comma)
		}
	}
	return result
}

func (d *Document) statementBounds(pos int) (int, int) {
	start, end := 0, len(d.Text)
	for _, t := range d.Tokens {
		if t.Kind != ";" {
			continue
		}
		if t.End <= pos {
			start = t.End
		} else {
			end = t.Start
			break
		}
	}
	return start, end
}

// A relation-bearing FROM/JOIN is in a query scope, rather than the FROM
// syntax of EXTRACT/TRIM or a LIKE expression. This remains usable before the
// unfinished statement can be parsed by the warehouse analyzer.
func (d *Document) relation(i int) bool {
	t := d.Tokens[i]
	start, _ := d.statementBounds(t.Start)
	if t.Value == "FROM" || t.Value == "JOIN" {
		first := -1
		for j := i - 1; j >= 0 && d.Tokens[j].Start >= start; j-- {
			if d.Tokens[j].Parent != t.Parent || d.Tokens[j].Kind == "comment" {
				continue
			}
			first = j
			if d.Tokens[j].Value == "SELECT" || d.Tokens[j].Value == "FROM" {
				return true
			}
		}
		return t.Value == "FROM" && first < 0
	}
	head := d.statementStart(i)
	if head >= len(d.Tokens) {
		return false
	}
	switch t.Value {
	case "REFERENCES":
		return d.Tokens[head].Value == "CREATE" || d.Tokens[head].Value == "ALTER"
	case "LIKE", "CLONE", "COPY":
		if d.Tokens[head].Value != "CREATE" || t.Parent != d.Tokens[head].Parent {
			return false
		}
		for j := head; j < i; j++ {
			if d.Tokens[j].Kind == "word" && d.Tokens[j].Value == "AS" && d.Tokens[j].Parent == t.Parent {
				return false
			}
		}
		return true
	case "INTO":
		return d.Tokens[head].Value == "INSERT" || d.Tokens[head].Value == "MERGE"
	case "UPDATE":
		return i == head
	}
	return false
}
