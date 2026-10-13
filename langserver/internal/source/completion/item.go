package completion

import (
	"github.com/kitagry/bqls/langserver/internal/lsp"
	"github.com/kitagry/bqls/langserver/internal/source/sqldoc"
	"strings"
)

type CompletionItem struct {
	TextEdit      *lsp.TextEdit
	Description   string
	Kind          lsp.CompletionItemKind
	NewText       string
	Detail        string
	SnippetText   string // Optional: if set, used when snippets are supported
	Documentation lsp.MarkupContent
	TypedPrefix   string
}

func (c CompletionItem) ToLspCompletionItem(position lsp.Position, supportSnippet bool) lsp.CompletionItem {
	result := c.toLspCompletionItem(position, supportSnippet)
	description := c.Description
	if c.Kind == lsp.CIKField && c.Documentation.Kind == lsp.MKPlainText {
		_, description, _ = strings.Cut(c.Documentation.Value, "\n")
		if info, ok := sqldoc.Type(c.Detail); ok {
			if description == "" {
				description = info.Summary
			}
			result.Documentation = lsp.MarkupContent{Kind: lsp.MKMarkdown, Value: "```sql\n" + c.NewText + " " + c.Detail + "\n```\n\n" + c.Documentation.Value + "\n\n" + sqldoc.TypeExplanation(c.Detail)}
		}
	}
	if description != "" {
		result.LabelDetails = &lsp.CompletionItemLabelDetails{Description: description}
	}
	return result
}
func (c CompletionItem) toLspCompletionItem(position lsp.Position, supportSnippet bool) lsp.CompletionItem {
	if c.TextEdit != nil {
		return lsp.CompletionItem{InsertTextFormat: lsp.ITFPlainText, Kind: c.Kind, Label: c.NewText, Detail: c.Detail, Documentation: c.Documentation, TextEdit: c.TextEdit}
	}

	if !supportSnippet {
		return lsp.CompletionItem{
			InsertTextFormat: lsp.ITFPlainText,
			Kind:             c.Kind,
			Label:            c.NewText,
			Detail:           c.Detail,
			Documentation:    c.Documentation,
		}
	}

	// Use SnippetText if available, otherwise fall back to NewText
	textToInsert := c.NewText
	if c.SnippetText != "" {
		textToInsert = c.SnippetText
	}

	startPosition := position
	startPosition.Character -= len(c.TypedPrefix)
	return lsp.CompletionItem{
		InsertTextFormat: lsp.ITFSnippet,
		Kind:             c.Kind,
		Label:            c.NewText,
		Detail:           c.Detail,
		Documentation:    c.Documentation,
		TextEdit: &lsp.TextEdit{
			NewText: textToInsert,
			Range: lsp.Range{
				Start: startPosition,
				End:   position,
			},
		},
	}
}
