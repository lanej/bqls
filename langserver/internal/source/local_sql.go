package source

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/kitagry/bqls/langserver/internal/lsp"
	"github.com/kitagry/bqls/langserver/internal/source/completion"
	"github.com/kitagry/bqls/langserver/internal/source/helper"
	"github.com/kitagry/bqls/langserver/internal/source/sqldoc"
)

func sourceOffset(text string, pos lsp.Position) int {
	pos = helper.UTF16PositionToBytePosition(text, pos)
	return rawByteOffset(text, pos)
}

func sourcePosition(text string, offset int) lsp.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	prefix := text[:offset]
	last := strings.LastIndexByte(prefix, '\n')
	return lsp.Position{Line: strings.Count(prefix, "\n"), Character: len(utf16.Encode([]rune(prefix[last+1:])))}
}

func sourceRange(text string, start, end int) lsp.Range {
	return lsp.Range{Start: sourcePosition(text, start), End: sourcePosition(text, end)}
}

func localCompletions(text string, pos lsp.Position) ([]completion.CompletionItem, bool) {
	doc := sqldoc.Parse(text)
	items, handled := doc.Complete(sourceOffset(text, pos))
	out := make([]completion.CompletionItem, 0, len(items))
	for _, item := range items {
		kind := lsp.CIKField
		if item.Kind == "property" {
			kind = lsp.CIKProperty
		}
		out = append(out, completion.CompletionItem{
			NewText: item.Name, Detail: item.Type, Kind: kind, Description: item.Description,
			Documentation: lsp.MarkupContent{Kind: lsp.MKMarkdown, Value: item.Documentation},
			TextEdit:      &lsp.TextEdit{NewText: item.NewText, Range: sourceRange(text, item.Start, item.End)},
		})
	}
	return out, handled
}

func fileURI(path string) lsp.DocumentURI {
	return lsp.DocumentURI((&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String())
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}

	// Unsaved documents do not exist yet, but their parent can still contain
	// symlinks (notably /var -> /private/var on macOS).
	parent := filepath.Dir(path)
	if parent != path {
		return filepath.Join(canonicalPath(parent), filepath.Base(path))
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// localTableDefinitions indexes source only for a table navigation request.
// Open documents take precedence over disk and ignored/generated trees are
// excluded when Git provides the workspace file list.
func (p *Project) localTableDefinitions(ctx context.Context, uri lsp.DocumentURI, target string) []lsp.Location {
	path, err := uri.FilePath()
	if err != nil {
		return nil
	}
	root := canonicalPath(p.rootPath)
	if p.rootPath == "" || !within(root, canonicalPath(path)) {
		root = filepath.Dir(canonicalPath(path))
	}
	texts := map[string]string{}
	for _, openURI := range p.cache.URIs() {
		openPath, err := openURI.FilePath()
		if err != nil {
			continue
		}
		openPath = canonicalPath(openPath)
		if within(root, openPath) {
			if text, ok := p.GetFile(openURI); ok {
				texts[openPath] = text
			}
		}
	}
	var paths []string
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.sql", "*.bqsql", "*.bigquery.sql", "*.bq.sql")
	if output, err := cmd.Output(); err == nil {
		for _, rel := range strings.Split(string(output), "\x00") {
			if rel != "" {
				paths = append(paths, filepath.Join(root, rel))
			}
		}
	} else {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".worktrees", "node_modules", "vendor", ".venv", "target":
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".sql" || ext == ".bqsql" {
				paths = append(paths, path)
			}
			return nil
		})
	}
	for _, candidate := range paths {
		if ctx.Err() != nil {
			return nil
		}
		candidate = canonicalPath(candidate)
		if !within(root, candidate) {
			continue
		}
		if _, open := texts[candidate]; open {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.Size() > 4<<20 {
			continue
		}
		data, err := os.ReadFile(candidate)
		if err == nil {
			texts[candidate] = string(data)
		}
	}
	var exact, candidates []lsp.Location
	for candidate, text := range texts {
		if ctx.Err() != nil {
			return nil
		}
		for _, table := range sqldoc.Parse(text).Tables {
			loc := lsp.Location{URI: fileURI(candidate), Range: sourceRange(text, table.NameStart, table.NameEnd)}
			if table.Name == target {
				exact = append(exact, loc)
			} else if !strings.Contains(target, ".") && table.Name[strings.LastIndexByte(table.Name, '.')+1:] == target {
				loc.CandidateName = table.Name
				candidates = append(candidates, loc)
			}
		}
	}
	result := candidates
	if len(exact) > 0 {
		result = exact
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].URI != result[j].URI {
			return result[i].URI < result[j].URI
		}
		return result[i].Range.Start.Line < result[j].Range.Start.Line
	})
	return result
}
