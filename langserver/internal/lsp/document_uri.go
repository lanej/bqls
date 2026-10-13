package lsp

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type DocumentURI string

func (d DocumentURI) IsFile() bool {
	return strings.HasPrefix(string(d), "file://")
}

func (d DocumentURI) FilePath() (string, error) {
	suffix, ok := strings.CutPrefix(string(d), "file://")
	if !ok {
		return "", errors.New("invalid file URI")
	}
	return url.PathUnescape(suffix)
}

func (d DocumentURI) IsVirtualTextDocument() bool {
	return strings.HasPrefix(string(d), "bqls://")
}

func (d DocumentURI) VirtualTextDocumentInfo() (VirtualTextDocumentInfo, error) {
	suffix, ok := strings.CutPrefix(string(d), "bqls://")
	if !ok {
		return VirtualTextDocumentInfo{}, errors.New("invalid text document URI")
	}

	result := VirtualTextDocumentInfo{}
	for suffix != "" {
		previous := suffix
		for _, prefix := range []string{"project/", "dataset/", "table/", "schema/", "job/", "location/"} {
			after, ok := strings.CutPrefix(suffix, prefix)
			if !ok {
				continue
			}

			ind := strings.Index(after, "/")
			var val string
			if ind == -1 {
				val = after
				suffix = ""
			} else {
				val = after[:ind]
				suffix = after[ind+1:]
			}

			if prefix == "project/" {
				result.ProjectID = val
			} else if prefix == "dataset/" {
				result.DatasetID = val
			} else if prefix == "table/" {
				result.TableID = val
			} else if prefix == "schema/" {
				result.TableID = val
				result.SchemaOnly = true
			} else if prefix == "job/" {
				result.JobID = val
			} else if prefix == "location/" {
				result.Location = val
			}
		}
		if suffix == previous {
			return VirtualTextDocumentInfo{}, errors.New("invalid virtual document path")
		}
	}

	if err := result.validate(); err != nil {
		return VirtualTextDocumentInfo{}, err
	}

	return result, nil
}

type VirtualTextDocumentInfo struct {
	SchemaOnly bool
	ProjectID  string
	DatasetID  string
	TableID    string
	JobID      string
	Location   string
}

func (v VirtualTextDocumentInfo) validate() error {
	if v.ProjectID == "" {
		return errors.New("project ID is required")
	}

	if v.DatasetID != "" && v.TableID != "" {
		return nil
	}

	if v.JobID != "" && v.Location != "" {
		return nil
	}

	return fmt.Errorf("either dataset ID and table ID or job ID and location ID is required")
}

func toPtr[T any](t T) *T {
	return &t
}

func NewTableSchemaDocumentURI(project, dataset, table string) DocumentURI {
	return DocumentURI(fmt.Sprintf("bqls://project/%s/dataset/%s/schema/%s", project, dataset, table))
}
