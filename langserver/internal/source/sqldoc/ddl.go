package sqldoc

import (
	"fmt"
	"strings"
)

type Column struct {
	Name, Type, Description, Collation     string
	NameStart, NameEnd, TypeStart, TypeEnd int
	CollationDeclared, Quoted, Required    bool
	Children                               []Column
}

type Table struct {
	Name                                        string
	NameStart, NameEnd, Start, End, Open, Close int
	Columns                                     []Column
	DefaultCollation                            string
}

func (d *Document) tables() []Table {
	var tables []Table
	for i, t := range d.Tokens {
		if t.Kind != "word" || t.Value != "CREATE" {
			continue
		}
		j := d.next(i)
		if j < len(d.Tokens) && d.Tokens[j].Value == "OR" {
			j = d.next(d.next(j))
		}
		if j < len(d.Tokens) && (d.Tokens[j].Value == "TEMP" || d.Tokens[j].Value == "TEMPORARY") {
			j = d.next(j)
		}
		if j >= len(d.Tokens) || d.Tokens[j].Value != "TABLE" {
			continue
		}
		j = d.next(j)
		if j < len(d.Tokens) && d.Tokens[j].Value == "FUNCTION" {
			continue
		}
		if j < len(d.Tokens) && d.Tokens[j].Value == "IF" {
			j = d.next(d.next(d.next(j)))
		}
		name, end := d.path(j)
		if name == "" {
			continue
		}
		table := Table{Name: name, NameStart: d.Tokens[j].Start, NameEnd: d.Tokens[end].End, Start: t.Start, End: len(d.Text), Open: -1, Close: -1}
		for k := end + 1; k < len(d.Tokens); k++ {
			if d.Tokens[k].Kind == ";" {
				table.End = d.Tokens[k].Start
				break
			}
		}
		open := d.next(end)
		if open < len(d.Tokens) && d.Tokens[open].Kind == "(" && d.Tokens[open].Match >= 0 {
			close := d.Tokens[open].Match
			table.Open, table.Close = open, close
			table.Columns = d.columns(open+1, close)
			for k := close + 1; k < len(d.Tokens) && d.Tokens[k].Start < table.End; k++ {
				if d.Tokens[k].Value == "DEFAULT" {
					c := d.next(k)
					v := d.next(c)
					if c < len(d.Tokens) && d.Tokens[c].Value == "COLLATE" && v < len(d.Tokens) && d.Tokens[v].Kind == "literal" {
						table.DefaultCollation = decode(d.Tokens[v])
					}
				}
			}
		}
		tables = append(tables, table)
	}
	return tables
}

func (d *Document) columns(first, last int) []Column {
	var columns []Column
	start, angles := first, 0
	for i := first; i < last; i++ {
		t := d.Tokens[i]
		if t.Kind == "<" {
			angles++
		}
		if t.Kind == ">" {
			angles--
		}
		if t.Kind == "(" || t.Kind == "[" {
			if t.Match >= i && t.Match < last {
				i = t.Match
				continue
			}
		}
		if t.Kind == "," && angles == 0 {
			if c, ok := d.column(start, i); ok {
				columns = append(columns, c)
			}
			start = i + 1
		}
	}
	if c, ok := d.column(start, last); ok {
		columns = append(columns, c)
	}
	return columns
}

func (d *Document) typeEnd(first, last int) int {
	if first >= last || d.Tokens[first].Kind != "word" {
		return first
	}
	end := first + 1
	j := d.next(first)
	if j < last && d.Tokens[j].Kind == "(" && d.Tokens[j].Match >= j && d.Tokens[j].Match < last {
		end = d.Tokens[j].Match + 1
	}
	if j < last && d.Tokens[j].Kind == "<" {
		depth := 1
		for k := j + 1; k < last; k++ {
			if d.Tokens[k].Kind == "<" {
				depth++
			}
			if d.Tokens[k].Kind == ">" {
				depth--
				if depth == 0 {
					end = k + 1
					break
				}
			}
		}
	}
	return end
}

func (d *Document) column(first, last int) (Column, bool) {
	for first < last && d.Tokens[first].Kind == "comment" {
		first++
	}
	if first >= last || d.name(first) == "" {
		return Column{}, false
	}
	if d.Tokens[first].Kind == "word" {
		switch d.Tokens[first].Value {
		case "CONSTRAINT", "PRIMARY", "FOREIGN", "CHECK":
			return Column{}, false
		}
	}
	typ := d.next(first)
	end := d.typeEnd(typ, last)
	if end <= typ {
		return Column{}, false
	}
	c := Column{Name: d.name(first), NameStart: d.Tokens[first].Start, NameEnd: d.Tokens[first].End, Quoted: d.Tokens[first].Kind == "quoted", TypeStart: d.Tokens[typ].Start, TypeEnd: d.Tokens[end-1].End, Type: d.formatType(typ, end)}
	inner := d.next(typ)
	if d.Tokens[typ].Value == "STRUCT" && inner < end && d.Tokens[inner].Kind == "<" {
		c.Children = d.columns(inner+1, end-1)
	}
	if d.Tokens[typ].Value == "ARRAY" && inner < end && d.Tokens[inner].Kind == "<" {
		nested := d.next(inner)
		op := d.next(nested)
		if nested < end && d.Tokens[nested].Value == "STRUCT" && op < end && d.Tokens[op].Kind == "<" {
			c.Children = d.columns(op+1, end-2)
		}
	}
	for i := end; i < last; i++ {
		t := d.Tokens[i]
		j := d.next(i)
		if t.Value == "NOT" && j < last && d.Tokens[j].Value == "NULL" {
			c.Required = true
		}
		if t.Value == "COLLATE" && j < last && d.Tokens[j].Kind == "literal" {
			c.Collation = decode(d.Tokens[j])
			c.CollationDeclared = true
		}
		if t.Value == "OPTIONS" && j < last && d.Tokens[j].Kind == "(" && d.Tokens[j].Match > j {
			stop := d.Tokens[j].Match
			for k := j + 1; k < stop; k++ {
				if d.Tokens[k].Value != "DESCRIPTION" || d.Tokens[k].Parent != j {
					continue
				}
				eq := d.next(k)
				value := d.next(eq)
				if eq >= stop || d.Tokens[eq].Kind != "=" {
					continue
				}
				var parts []string
				for value < stop && d.Tokens[value].Kind == "literal" && d.Tokens[value].Closed {
					parts = append(parts, decode(d.Tokens[value]))
					value = d.next(value)
				}
				if len(parts) > 0 && (value == stop || d.Tokens[value].Kind == ",") {
					c.Description = strings.Join(parts, "")
				}
			}
			i = stop
		}
	}
	return c, true
}

func (d *Document) formatType(first, last int) string {
	var out strings.Builder
	prev := ""
	for i := first; i < last; i++ {
		t := d.Tokens[i]
		if t.Kind == "comment" {
			continue
		}
		if t.Value == "OPTIONS" {
			j := d.next(i)
			if j < last && d.Tokens[j].Kind == "(" && d.Tokens[j].Match >= j {
				i = d.Tokens[j].Match
				continue
			}
		}
		if out.Len() > 0 && t.Kind != ">" && t.Kind != "<" && t.Kind != "," && t.Kind != ")" && t.Kind != "(" && prev != "<" && prev != "(" {
			out.WriteByte(' ')
		}
		out.WriteString(d.Text[t.Start:t.End])
		prev = t.Kind
	}
	return out.String()
}

func (c Column) Documentation() string {
	name := c.Name
	if c.Quoted {
		name = "`" + strings.ReplaceAll(name, "`", "\\`") + "`"
	}
	required := ""
	if c.Required {
		required = " NOT NULL"
	}
	doc := fmt.Sprintf("```sql\n%s %s%s\n```", name, c.Type, required)
	if c.Description != "" {
		doc += "\n\n" + c.Description
	}
	if help := TypeExplanation(c.Type); help != "" {
		doc += "\n\n" + help
	}
	return doc
}

func (c Column) clusterable(defaultCollation string) bool {
	info, ok := Type(c.Type)
	if !ok {
		return false
	}
	switch info.Canonical {
	case "BIGNUMERIC", "BOOL", "DATE", "DATETIME", "GEOGRAPHY", "INT64", "NUMERIC", "RANGE", "STRING", "TIMESTAMP":
		if info.Canonical == "STRING" {
			collation := c.Collation
			if !c.CollationDeclared {
				collation = defaultCollation
			}
			return collation == ""
		}
		return true
	}
	return false
}

func columnAt(columns []Column, pos int) *Column {
	for _, c := range columns {
		if c.NameStart <= pos && pos < c.NameEnd {
			return &c
		}
		if c.TypeStart <= pos && pos < c.TypeEnd {
			if child := columnAt(c.Children, pos); child != nil {
				return child
			}
			return &c
		}
	}
	return nil
}

func (d *Document) columnHover(pos int) string {
	i := d.at(pos, false)
	if i < 0 || d.blocked(pos, true) {
		return ""
	}
	for _, table := range d.Tables {
		if c := columnAt(table.Columns, pos); c != nil {
			return c.Documentation()
		}
		if pos < table.Start || pos > table.End || table.Close < 0 || pos < d.Tokens[table.Close].End {
			continue
		}
		// Only partition/cluster expressions reference these declarations.
		clause := ""
		for j := table.Close + 1; j <= i; j++ {
			switch d.Tokens[j].Value {
			case "PARTITION", "CLUSTER":
				clause = d.Tokens[j].Value
			case "OPTIONS", "AS":
				clause = ""
			}
		}
		if clause == "" {
			continue
		}
		next := d.next(i)
		if next < len(d.Tokens) && d.Tokens[next].Kind == "(" {
			continue
		}
		for _, c := range table.Columns {
			if strings.EqualFold(c.Name, d.name(i)) {
				return c.Documentation()
			}
		}
	}
	return ""
}

type Item struct {
	Name, Type, Description, Documentation, NewText, Kind string
	Start, End                                            int
}

func (d *Document) clusterCompletion(pos int) ([]Item, bool) {
	for _, table := range d.Tables {
		if table.Close < 0 || pos < table.Start || pos > table.End {
			continue
		}
		cluster := -1
		for i := table.Close + 1; i < len(d.Tokens) && d.Tokens[i].Start < pos; i++ {
			if d.Tokens[i].Value == "CLUSTER" {
				j := d.next(i)
				if j < len(d.Tokens) && d.Tokens[j].Value == "BY" && d.Tokens[j].End <= pos {
					cluster = j
				}
			}
		}
		if cluster < 0 {
			continue
		}
		if d.blocked(pos, true) {
			return nil, true
		}
		used := map[string]int{}
		segment := 0
		editing := -1
		for i := cluster + 1; i < len(d.Tokens) && d.Tokens[i].Start <= table.End; i++ {
			t := d.Tokens[i]
			if t.Value == "OPTIONS" && d.next(i) < len(d.Tokens) && d.Tokens[d.next(i)].Kind == "(" || t.Value == "AS" || t.Kind == ";" {
				if pos > t.Start || pos == t.Start && t.Kind != ";" {
					return nil, false
				}
				break
			}
			if t.Kind == "comment" {
				continue
			}
			if t.Kind == "word" || t.Kind == "quoted" {
				used[strings.ToLower(d.name(i))]++
			}
			if t.Start < pos {
				if t.Kind == "," {
					segment = 0
				} else {
					segment++
				}
			}
			if t.Start <= pos && (pos < t.End || pos == t.End && (t.Kind == "word" || !t.Closed)) {
				editing = i
			}
		}
		if segment > 1 {
			return nil, true
		}
		if editing < 0 && segment > 0 {
			return nil, true
		}
		start, end, quoted := pos, pos, false
		if editing >= 0 {
			t := d.Tokens[editing]
			if t.Kind != "word" && t.Kind != "quoted" {
				return nil, true
			}
			start, end, quoted = t.Start, t.End, t.Kind == "quoted"
			used[strings.ToLower(d.name(editing))]--
		}
		slots := 0
		for _, n := range used {
			slots += n
		}
		if slots >= 4 {
			return nil, true
		}
		var items []Item
		for _, c := range table.Columns {
			if !c.clusterable(table.DefaultCollation) || used[strings.ToLower(c.Name)] > 0 {
				continue
			}
			name := c.Name
			if quoted || c.Quoted || strings.IndexFunc(name, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
			}) >= 0 {
				name = "`" + strings.ReplaceAll(name, "`", "\\`") + "`"
			}
			if pos == d.Tokens[cluster].End {
				name = " " + name
			}
			description := c.Description
			if description == "" {
				info, _ := Type(c.Type)
				description = info.Summary
			}
			items = append(items, Item{Name: c.Name, Type: c.Type, Description: description, Documentation: c.Documentation(), NewText: name, Kind: "field", Start: start, End: end})
		}
		return items, true
	}
	return nil, false
}
