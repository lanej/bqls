package source_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/kitagry/bqls/langserver/internal/bigquery/mock_bigquery"
	"github.com/kitagry/bqls/langserver/internal/lsp"
	"github.com/kitagry/bqls/langserver/internal/source"
	"github.com/kitagry/bqls/langserver/internal/source/helper"

	"github.com/sirupsen/logrus"
)

func TestSourceLocalHoverDocumentation(t *testing.T) {
	client := mock_bigquery.NewMockClient(gomock.NewController(t))
	p := source.NewProjectWithBQClient(t.TempDir(), client, logrus.New())
	uri := lsp.DocumentURI("file:///local.bqsql")
	sql, pos := cursorSQL(t, `CREATE TABLE journeys (
  id STRING|(36) OPTIONS(description = 'Journey\'s identifier\nUnicode: \u6771'),
  metadata ARRAY<STRUCT<`+"`postal  code`"+` STRING OPTIONS(description = 'Nested')>> OPTIONS(description = 'Outer')
);`)
	p.UpdateFile(uri, sql, 1)
	docs, err := p.TermDocument(t.Context(), uri, pos)
	if err != nil || len(docs) != 1 {
		t.Fatalf("hover: %v %v", docs, err)
	}
	if !strings.Contains(docs[0].Value, "Journey's identifier\nUnicode: 東") || !strings.Contains(docs[0].Value, "Declared maximum: 36 Unicode characters.") {
		t.Fatalf("column/type docs missing: %s", docs[0].Value)
	}
	sql, pos = cursorSQL(t, "CREATE TABLE journeys (id STRING OPTIONS|(description = 'id')) OPTIONS(require_partition_filter = TRUE);")
	p.UpdateFile(uri, sql, 2)
	docs, err = p.TermDocument(t.Context(), uri, pos)
	if err != nil || len(docs) != 1 || !strings.Contains(docs[0].Value, "rounding_mode") || strings.Contains(docs[0].Value, "require_partition_filter") {
		t.Fatalf("column options leaked: %v %v", docs, err)
	}
	sql, pos = cursorSQL(t, "CREATE TABLE journeys (id STRING NOT| NULL);")
	p.UpdateFile(uri, sql, 3)
	docs, err = p.TermDocument(t.Context(), uri, pos)
	if err != nil || len(docs) != 1 || !strings.Contains(docs[0].Value, "Makes a column required") {
		t.Fatalf("keyword docs missing: %v %v", docs, err)
	}
}

func TestSourceLocalDDLCompletion(t *testing.T) {
	client := mock_bigquery.NewMockClient(gomock.NewController(t))
	p := source.NewProjectWithBQClient(t.TempDir(), client, logrus.New())
	uri := lsp.DocumentURI("file:///local.bqsql")
	sql, pos := cursorSQL(t, "CREATE TABLE journeys (\ncarrier_id STRING(36), `case` STRING OPTIONS(description = 'Carrier category'), events ARRAY<STRING>, estimate FLOAT64, tenant STRING COLLATE 'und:ci'\n) CLUSTER BY carrier_id, ca|;")
	p.UpdateFile(uri, sql, 1)
	items, err := p.Complete(t.Context(), uri, pos)
	if err != nil || len(items) != 1 {
		t.Fatalf("clustering candidates: %v %v", items, err)
	}
	item := items[0].ToLspCompletionItem(pos, false)
	if item.Label != "case" || item.Detail != "STRING" || item.TextEdit == nil || item.TextEdit.NewText != "`case`" || item.LabelDetails == nil || item.LabelDetails.Description != "Carrier category" {
		t.Fatalf("column protocol details: %#v", item)
	}
	if !strings.Contains(item.Documentation.Value, "Unicode text encoded as UTF-8.") {
		t.Fatal("type explanation missing")
	}
	sql, pos = cursorSQL(t, "CREATE TABLE journeys (amount NUMERIC OPTIONS(round| /* preserve */ = 'ROUND_HALF_EVEN'));")
	p.UpdateFile(uri, sql, 2)
	items, err = p.Complete(t.Context(), uri, pos)
	if err != nil {
		t.Fatal(err)
	}
	var rounding *lsp.CompletionItem
	for _, candidate := range items {
		if candidate.NewText == "rounding_mode" {
			value := candidate.ToLspCompletionItem(pos, true)
			rounding = &value
		}
	}
	if rounding == nil || rounding.TextEdit == nil || rounding.TextEdit.NewText != "rounding_mode" {
		t.Fatalf("existing assignment not preserved: %v", rounding)
	}
	sql, pos = cursorSQL(t, "CREATE TABLE journeys (id STRING) OPTIONS(require_partition_filter = TRUE, -- further options|\n)")
	p.UpdateFile(uri, sql, 3)
	items, err = p.Complete(t.Context(), uri, pos)
	if err != nil || len(items) != 0 {
		t.Fatalf("completion inside a comment: %v %v", items, err)
	}
}

func TestSourceLocalDefinitionNavigation(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	client := mock_bigquery.NewMockClient(gomock.NewController(t))
	p := source.NewProjectWithBQClient(root, client, logrus.New())
	ddl := filepath.Join(root, "create journeys.sql")
	query := filepath.Join(root, "query.bqsql")
	uri := func(path string) lsp.DocumentURI {
		return lsp.DocumentURI((&url.URL{Scheme: "file", Path: path}).String())
	}
	ddlSQL := "-- CREATE TABLE demo.journeys (decoy STRING);\nCREATE TABLE demo.journeys (metadata STRUCT<id STRING>, id STRING, cohort DATE)\nPARTITION BY DATE_TRUNC(cohort, MONTH) CLUSTER BY id;"
	if err := os.WriteFile(ddl, []byte(ddlSQL), 0600); err != nil {
		t.Fatal(err)
	}
	sql, pos := cursorSQL(t, "WITH journeys AS (SELECT * FROM demo.journeys)\nSELECT * FROM journeys|;")
	p.UpdateFile(uri(query), sql, 1)
	locs, err := p.LookupIdent(context.Background(), uri(query), pos)
	if err != nil || len(locs) != 1 || locs[0].URI != uri(query) || locs[0].Range.Start.Character != 5 {
		t.Fatalf("CTE navigation: %v %v", locs, err)
	}
	_, pos = cursorSQL(t, "WITH journeys AS (SELECT * FROM demo.jour|neys)\nSELECT * FROM journeys;")
	locs, err = p.LookupIdent(t.Context(), uri(query), pos)
	if err != nil || len(locs) != 1 || locs[0].URI != uri(ddl) || locs[0].Range.Start.Line != 1 {
		t.Fatalf("local DDL navigation: %v %v", locs, err)
	}
	p.UpdateFile(uri(ddl), ddlSQL, 1)
	_, pos = cursorSQL(t, strings.Replace(ddlSQL, "CLUSTER BY id", "CLUSTER BY i|d", 1))
	locs, err = p.LookupIdent(t.Context(), uri(ddl), pos)
	if err != nil || len(locs) != 1 || locs[0].Range.Start.Character != 56 {
		t.Fatalf("top-level column navigation: %v %v", locs, err)
	}
}

func cursorSQL(t *testing.T, sql string) (string, lsp.Position) {
	t.Helper()
	files, _, pos, err := helper.GetLspPosition(map[lsp.DocumentURI]string{"test.sql": sql})
	if err != nil {
		t.Fatal(err)
	}
	return files["test.sql"], pos
}
