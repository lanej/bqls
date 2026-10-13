package sqldoc

import (
	"strings"
)

type optionContext struct {
	Set                                  OptionSet
	Keyword, Open, Last, Selected, Depth int
	Segment                              []int
	Used                                 map[string]bool
	HasEquals                            bool
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (d *Document) optionScope(keyword, open int) string {
	start := d.statementStart(keyword)
	if start >= len(d.Tokens) {
		return ""
	}
	var words []string
	for i := start; i < keyword; i++ {
		if d.Tokens[i].Kind == "word" {
			words = append(words, d.Tokens[i].Value)
		}
	}
	if len(words) < 2 {
		return ""
	}
	head := words[0]
	j := 1
	if words[j] == "OR" {
		j += 2
	}
	if j < len(words) && (words[j] == "TEMP" || words[j] == "TEMPORARY") {
		j++
	}
	if j >= len(words) {
		return ""
	}
	object := words[j]
	// OPTIONS belongs to the DDL header, never an AS query or SQL body.
	for i := start; i < keyword; i++ {
		token := d.Tokens[i]
		if token.Kind == "word" && token.Parent == d.Tokens[start].Parent && (token.Value == "AS" || token.Value == "BEGIN") {
			return ""
		}
	}
	materialized := object == "MATERIALIZED" && j+1 < len(words) && words[j+1] == "VIEW"
	view := object == "VIEW" || materialized
	statement := " " + strings.Join(words, " ") + " "

	column := false
	for _, t := range d.Tables {
		if t.Open >= 0 && keyword > t.Open && keyword < t.Close {
			column = true
		}
	}
	if !column && view {
		for i := start; i < keyword; i++ {
			if d.Tokens[i].Kind == "(" && d.Tokens[i].Match > keyword && d.Tokens[i].Parent == -1 {
				column = true
			}
		}
	}
	if head == "LOAD" {
		if d.Tokens[keyword].Value == "FILES" {
			return "load_data_files"
		}
		if d.Tokens[keyword].Parent >= 0 {
			return "load_data_column"
		}
		return "load_data_table"
	}
	if head == "EXPORT" {
		backend := "files"
		last := len(d.Tokens)
		if d.Tokens[open].Match >= 0 {
			last = d.Tokens[open].Match
		}
		for i := open + 1; i < last; i++ {
			if d.Tokens[i].Value == "FORMAT" {
				eq := d.next(i)
				value := d.next(eq)
				if eq < last && d.Tokens[eq].Kind == "=" && value < last {
					switch strings.ToUpper(d.Tokens[value].Value) {
					case "BIGTABLE":
						backend = "bigtable"
					case "CLOUD_PUBSUB":
						backend = "pubsub"
					case "CLOUD_SPANNER":
						backend = "spanner"
					case "CLOUD_ALLOYDB":
						backend = "alloydb"
					}
				}
			}
		}
		return "export_" + backend
	}
	if head == "ALTER" {
		if strings.Contains(statement, " ALTER COLUMN ") {
			if view {
				return "alter_view_column"
			}
			return "alter_column"
		}
		if object == "SCHEMA" && strings.Contains(statement, " ADD REPLICA ") {
			return "add_schema_replica"
		}
		if materialized {
			return "alter_materialized_view"
		}
		if view {
			return "alter_view"
		}
		switch object {
		case "TABLE":
			return "alter_table"
		case "SCHEMA":
			return "alter_schema"
		}
	}
	if head != "CREATE" {
		return ""
	}
	if column {
		if view {
			return "create_view_column"
		}
		return "create_column"
	}
	if materialized {
		return "create_materialized_view"
	}
	if view {
		return "create_view"
	}
	switch object {
	case "SCHEMA":
		return "create_schema"
	case "SNAPSHOT":
		return "create_snapshot_table"
	case "EXTERNAL":
		return "create_external_table"
	case "MODEL":
		return "create_model"
	case "TABLE":
		if j+1 < len(words) && words[j+1] == "FUNCTION" {
			return "create_table_function"
		}
		if strings.Contains(statement, " CLONE ") {
			return "create_table_clone"
		}
		if strings.Contains(statement, " COPY ") {
			return "create_table_copy"
		}
		if strings.Contains(statement, " LIKE ") {
			return "create_table_like"
		}
		return "create_table"
	case "FUNCTION":
		if strings.Contains(statement, " REMOTE WITH CONNECTION ") {
			return "create_remote_function"
		}
		if strings.Contains(statement, " LANGUAGE JS ") || strings.Contains(statement, " LANGUAGE JAVASCRIPT ") {
			return "create_javascript_function"
		}
		if strings.Contains(statement, " LANGUAGE PYTHON ") {
			return "create_python_function"
		}
		return "create_sql_function"
	case "PROCEDURE":
		if strings.Contains(statement, " WITH CONNECTION ") {
			return "create_spark_procedure"
		}
		return "create_sql_procedure"
	}
	return ""
}

func (d *Document) optionContext(pos int) *optionContext {
	if d.blocked(pos, false) {
		return nil
	}
	var ctx *optionContext
	for i, t := range d.Tokens {
		if t.Kind != "word" || t.Value != "OPTIONS" && t.Value != "FILES" {
			continue
		}
		open := d.next(i)
		if open >= len(d.Tokens) || d.Tokens[open].Kind != "(" {
			continue
		}
		last := len(d.Text) + 1
		stop := d.Tokens[open].Match
		if stop >= 0 {
			last = d.Tokens[stop].Start
		} else {
			for j := open + 1; j < len(d.Tokens); j++ {
				if d.Tokens[j].Kind == ";" {
					last = d.Tokens[j].Start
					break
				}
			}
		}
		if pos < t.Start || pos > last {
			continue
		}
		scope := d.optionScope(i, open)
		set, ok := options.Contexts[scope]
		if !ok {
			continue
		}
		if scope == "create_model" {
			model := ""
			for j := open + 1; j < len(d.Tokens) && d.Tokens[j].Start < last; j++ {
				if d.Tokens[j].Value == "MODEL_TYPE" {
					eq := d.next(j)
					v := d.next(eq)
					if eq < len(d.Tokens) && d.Tokens[eq].Kind == "=" && v < len(d.Tokens) && d.Tokens[v].Kind == "literal" && d.Tokens[v].Closed {
						model = strings.ToUpper(d.Tokens[v].Value)
					}
				}
			}
			for _, family := range options.Families {
				if !contains(family.ModelTypes, model) {
					continue
				}
				set = family
				set.Options = nil
				for _, o := range family.Options {
					if len(o.ModelTypes) == 0 || contains(o.ModelTypes, model) {
						set.Options = append(set.Options, o)
					}
				}
				break
			}
		}
		ctx = &optionContext{Set: set, Keyword: i, Open: open, Last: last, Selected: d.at(pos, true), Used: map[string]bool{}}
		depth := 0
		segment := []int{}
		assignments := map[int]bool{}
		for j := open + 1; j < len(d.Tokens) && d.Tokens[j].Start < last; j++ {
			token := d.Tokens[j]
			if token.Kind == "comment" {
				continue
			}
			if depth == 0 && token.Kind == "," {
				segment = nil
			} else if depth == 0 {
				segment = append(segment, j)
				if token.Kind == "=" && len(segment) > 0 && d.Tokens[segment[0]].Kind == "word" {
					ctx.Used[strings.ToLower(d.Tokens[segment[0]].Value)] = true
					assignments[segment[0]] = true
				}
			}
			if token.Start <= pos {
				ctx.Segment = append([]int(nil), segment...)
				ctx.Depth = depth
			}
			if token.Kind == "(" || token.Kind == "[" {
				depth++
			}
			if token.Kind == ")" || token.Kind == "]" {
				depth--
			}
		}
		if len(ctx.Segment) > 0 {
			ctx.HasEquals = assignments[ctx.Segment[0]]
		}
	}
	return ctx
}

func (d *Document) optionsHover(pos int) string {
	c := d.optionContext(pos)
	if c == nil {
		return ""
	}
	if c.Selected == c.Keyword {
		s := "```sql\n" + d.Tokens[c.Keyword].Value + "(name = value, ...)\n```\n\n" + c.Set.Title + "\n"
		for _, o := range c.Set.Options {
			s += "\n- `" + o.Name + "` · `" + o.Type + "` — " + o.Summary
		}
		return s + "\n\n[GoogleSQL option reference](" + c.Set.URL + ")"
	}
	if len(c.Segment) > 0 && c.Selected == c.Segment[0] {
		for _, o := range c.Set.Options {
			if strings.EqualFold(o.Name, d.Tokens[c.Selected].Value) {
				return optionDoc(o)
			}
		}
	}
	return ""
}

func (d *Document) Complete(pos int) ([]Item, bool) {
	if d.blocked(pos, true) {
		return nil, true
	}
	if c := d.optionContext(pos); c != nil {
		if pos <= d.Tokens[c.Open].Start || c.Depth != 0 {
			return nil, true
		}
		key := ""
		if len(c.Segment) > 0 {
			t := d.Tokens[c.Segment[0]]
			if len(c.Segment) != 1 || t.Kind != "word" {
				return nil, true
			}
			key = strings.ToLower(t.Value)
		}
		start, end := pos, pos
		if i := d.at(pos, true); i >= 0 && d.Tokens[i].Kind == "word" {
			start, end = d.Tokens[i].Start, d.Tokens[i].End
		}
		var items []Item
		for _, o := range c.Set.Options {
			if c.Used[o.Name] && o.Name != key {
				continue
			}
			text := o.Name
			if !c.HasEquals {
				text += " = "
			}
			items = append(items, Item{Name: o.Name, Type: o.Type, Description: o.Summary, Documentation: optionDoc(o), NewText: text, Kind: "property", Start: start, End: end})
		}
		return items, true
	}
	return d.clusterCompletion(pos)
}

func (d *Document) Hover(pos int) string {
	if d.blocked(pos, true) {
		return ""
	}
	if s := d.columnHover(pos); s != "" {
		return s
	}
	if s := d.optionsHover(pos); s != "" {
		return s
	}
	i := d.at(pos, true)
	if i < 0 || d.Tokens[i].Kind != "word" {
		return ""
	}
	if _, ok := Type(d.Tokens[i].Value); ok {
		prev, next := d.previous(i), d.next(i)
		// In STRUCT<name type>, the first word is a field name.
		if prev >= 0 && (d.Tokens[prev].Kind == "<" || d.Tokens[prev].Kind == ",") && next < len(d.Tokens) && d.Tokens[next].Kind == "word" {
			return ""
		}
		if prev >= 0 && (d.Tokens[prev].Value == "AS" && d.castType(i) || d.Tokens[prev].Kind == "<" || d.Tokens[prev].Kind == "," || d.Tokens[prev].Kind == "word" && next < len(d.Tokens) && d.Tokens[next].Kind == ">") || next < len(d.Tokens) && (d.Tokens[next].Kind == "(" || d.Tokens[next].Kind == "literal") {
			end := d.typeEnd(i, len(d.Tokens))
			declaration := d.formatType(i, end)
			return "```sql\n" + declaration + "\n```\n\n" + TypeExplanation(declaration)
		}
		return ""
	}
	return d.keywordHover(i)
}

func (d *Document) castType(i int) bool {
	parent := d.Tokens[i].Parent
	if parent < 0 {
		return false
	}
	prior := d.previous(parent)
	return prior >= 0 && (d.Tokens[prior].Value == "CAST" || d.Tokens[prior].Value == "SAFE_CAST")
}
