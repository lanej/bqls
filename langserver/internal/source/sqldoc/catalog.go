package sqldoc

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Reference catalogs are embedded so docs remain available without credentials.
//
//go:embed keywords.json
var keywordData []byte

//go:embed options.json
var optionData []byte

type Keyword struct{ Keyword, Summary, Syntax, URL string }
type Option struct {
	Name, Type, Summary, URL string
	Values                   []string
	ModelTypes               []string `json:"applies_to_model_types"`
}
type OptionSet struct {
	Title, URL string
	Options    []Option
	ModelTypes []string `json:"model_types"`
}
type Catalog struct {
	Contexts map[string]OptionSet
	Families map[string]OptionSet `json:"model_families"`
}

var keywords = load[map[string]Keyword](keywordData)
var options = load[Catalog](optionData)
var keywordPhrases = phrases()

func load[T any](data []byte) T {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		panic(err)
	}
	return v
}

func phrases() []Keyword {
	var out []Keyword
	seen := map[string]bool{}
	for _, k := range keywords {
		if !seen[k.Keyword] {
			out = append(out, k)
			seen[k.Keyword] = true
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Keyword) == len(out[j].Keyword) {
			return out[i].Keyword < out[j].Keyword
		}
		return len(out[i].Keyword) > len(out[j].Keyword)
	})
	return out
}

func optionDoc(o Option) string {
	s := fmt.Sprintf("```sql\n%s = %s\n```\n\n%s", o.Name, o.Type, o.Summary)
	if len(o.Values) > 0 {
		s += "\n\nSupported values: " + strings.Join(o.Values, ", ") + "."
	}
	return s + "\n\n[GoogleSQL option reference](" + o.URL + ")"
}

type TypeInfo struct{ Canonical, Summary, Anchor string }

var typeInfo = map[string]TypeInfo{
	"STRING":     {"STRING", "Unicode text encoded as UTF-8. STRING(L) limits length to L characters, not bytes; longer assignments raise OUT_OF_RANGE.", "string_type"},
	"BYTES":      {"BYTES", "Binary data. BYTES(L) limits length to L bytes.", "bytes_type"},
	"BOOL":       {"BOOL", "Logical TRUE or FALSE.", "boolean_type"},
	"INT64":      {"INT64", "Signed 64-bit integer.", "integer_type"},
	"FLOAT64":    {"FLOAT64", "Approximate double-precision floating-point number.", "floating_point_type"},
	"NUMERIC":    {"NUMERIC", "Exact decimal with precision 38 and scale 9.", "decimal_types"},
	"BIGNUMERIC": {"BIGNUMERIC", "Exact decimal with scale 38 and approximately 76.8 digits of precision.", "decimal_types"},
	"DATE":       {"DATE", "Calendar date without time or time zone.", "date_type"},
	"DATETIME":   {"DATETIME", "Calendar date and time without a time zone.", "datetime_type"},
	"TIME":       {"TIME", "Time of day without a date or time zone.", "time_type"},
	"TIMESTAMP":  {"TIMESTAMP", "Absolute instant with microsecond precision.", "timestamp_type"},
	"INTERVAL":   {"INTERVAL", "Duration expressed in years, months, days, hours, minutes and seconds.", "interval_type"},
	"GEOGRAPHY":  {"GEOGRAPHY", "Points, lines and polygons on Earth's surface.", "geography_type"},
	"JSON":       {"JSON", "Structured JSON data.", "json_type"},
	"ARRAY":      {"ARRAY", "Ordered values of one type. Directly nested arrays are unsupported.", "array_type"},
	"STRUCT":     {"STRUCT", "Ordered fields, each with its own type and optional name.", "struct_type"},
	"RANGE":      {"RANGE", "Date, datetime or timestamp interval; lower bound inclusive, upper bound exclusive.", "range_type"},
}
var aliases = map[string]string{"BOOLEAN": "BOOL", "INT": "INT64", "SMALLINT": "INT64", "INTEGER": "INT64", "BIGINT": "INT64", "TINYINT": "INT64", "BYTEINT": "INT64", "DECIMAL": "NUMERIC", "BIGDECIMAL": "BIGNUMERIC"}

func Type(declaration string) (TypeInfo, bool) {
	word := strings.ToUpper(strings.TrimSpace(declaration))
	if i := strings.IndexAny(word, "(< \t\n"); i >= 0 {
		word = word[:i]
	}
	if canonical, ok := aliases[word]; ok {
		word = canonical
	}
	info, ok := typeInfo[word]
	return info, ok
}

func TypeExplanation(declaration string) string {
	info, ok := Type(declaration)
	if !ok {
		return ""
	}
	s := info.Summary
	t := scan(declaration)
	if info.Canonical == "STRING" && len(t) == 4 && t[1].Kind == "(" && t[3].Kind == ")" {
		s += "\n\nDeclared maximum: " + t[2].Value + " Unicode characters."
	}
	return s + "\n\n[GoogleSQL type reference](https://docs.cloud.google.com/bigquery/docs/reference/standard-sql/data-types#" + info.Anchor + ")"
}

func (d *Document) keywordHover(i int) string {
	if i < 0 || d.Tokens[i].Kind != "word" {
		return ""
	}
	prev, next := d.previous(i), d.next(i)
	if prev >= 0 && d.Tokens[prev].Kind == "." || next < len(d.Tokens) && d.Tokens[next].Kind == "." {
		return ""
	}
	for _, k := range keywordPhrases {
		words := strings.Fields(k.Keyword)
		for offset := range words {
			start := i
			for n := 0; n < offset && start >= 0; n++ {
				start = d.previous(start)
			}
			if start < 0 {
				continue
			}
			j := start
			match := true
			for _, w := range words {
				if j >= len(d.Tokens) || d.Tokens[j].Kind != "word" || d.Tokens[j].Value != w {
					match = false
					break
				}
				j = d.next(j)
			}
			if match {
				return fmt.Sprintf("```sql\n%s\n```\n\n%s\n\n```sql\n%s\n```\n\n[GoogleSQL reference](%s)", k.Keyword, k.Summary, k.Syntax, k.URL)
			}
		}
	}
	return ""
}
