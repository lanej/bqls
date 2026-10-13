package langserver

import (
	"encoding/json"
	"strings"
	"testing"

	"cloud.google.com/go/bigquery"
	"github.com/golang/mock/gomock"
	"github.com/kitagry/bqls/langserver/internal/bigquery/mock_bigquery"
	"github.com/kitagry/bqls/langserver/internal/lsp"
	"github.com/kitagry/bqls/langserver/internal/source"
	"github.com/sirupsen/logrus"
	"github.com/sourcegraph/jsonrpc2"
)

func TestSchemaNavigationDocument(t *testing.T) {
	client := mock_bigquery.NewMockClient(gomock.NewController(t))
	client.EXPECT().GetTableMetadata(gomock.Any(), "demo", "deliveries", "journeys").Return(&bigquery.TableMetadata{
		FullID: "demo:deliveries.journeys", Type: bigquery.RegularTable,
		Schema: bigquery.Schema{{Name: "carrier", Type: bigquery.StringFieldType, Description: "Canonical carrier"}},
	}, nil)
	h := &Handler{logger: logrus.New(), project: source.NewProjectWithBQClient(t.TempDir(), client, logrus.New())}
	// Schema navigation is metadata-only even for clients that request async
	// details/preview documents. Any row read is an unexpected mock call.
	h.initializeParams.InitializationOptions.SupportsAsyncVirtualTextDocument = true
	params := json.RawMessage(`{"textDocument":{"uri":"bqls://project/demo/dataset/deliveries/schema/journeys"}}`)
	result, err := h.handleVirtualTextDocument(t.Context(), nil, &jsonrpc2.Request{Method: "bqls/virtualTextDocument", Params: &params})
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := result.(lsp.VirtualTextDocument)
	if !ok || doc.Pending || len(doc.Schema) != 1 || len(doc.Result.Data) != 0 {
		t.Fatalf("schema response: %#v", result)
	}
	if len(doc.Contents) != 2 || !strings.Contains(doc.Contents[1].Value, "Canonical carrier") {
		t.Fatalf("schema text missing: %#v", doc.Contents)
	}
}
