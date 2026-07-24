// Package lsp serves a SCIP index over the Language Server Protocol on stdio.
//
// Framing and dispatch are implemented directly on encoding/json rather than
// pulling in an LSP library. The surface clew needs is small, and keeping the
// dependency set to {scip, protobuf} makes the binary trivial to build and ship.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rafacm/clew/internal/index"
)

type Server struct {
	log io.Writer

	mu        sync.RWMutex
	root      string
	indexPath string
	store     *index.Store
}

// Serve runs the stdio message loop until the client disconnects.
func Serve(ctx context.Context, in io.Reader, out io.Writer, logw io.Writer) error {
	s := &Server{log: logw}
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)

	for {
		msg, err := readMessage(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if msg.Method == "exit" {
			return nil
		}
		resp := s.dispatch(msg)
		if resp == nil {
			continue // notification
		}
		if err := writeMessage(w, resp); err != nil {
			return err
		}
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.log != nil {
		fmt.Fprintf(s.log, "clew-lsp: "+format+"\n", args...)
	}
}

// ---------------------------------------------------------------- dispatch

func (s *Server) dispatch(m *message) *message {
	switch m.Method {
	case "initialize":
		return m.reply(s.initialize(m))
	case "initialized", "textDocument/didOpen", "textDocument/didChange",
		"textDocument/didSave", "textDocument/didClose":
		return nil
	case "workspace/didChangeWatchedFiles":
		s.reload()
		return nil
	case "shutdown":
		return m.reply(nil)
	case "textDocument/definition":
		return m.reply(s.definition(m))
	case "textDocument/references":
		return m.reply(s.references(m))
	case "textDocument/documentSymbol":
		return m.reply(s.documentSymbol(m))
	case "workspace/symbol":
		return m.reply(s.workspaceSymbol(m))
	default:
		if m.ID == nil {
			return nil
		}
		return m.reply(nil)
	}
}

func (s *Server) initialize(m *message) any {
	var params struct {
		RootURI    string `json:"rootUri"`
		InitOpts   any    `json:"initializationOptions"`
		WorkspaceF []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	_ = json.Unmarshal(m.Params, &params)

	root := uriToPath(params.RootURI)
	if root == "" && len(params.WorkspaceF) > 0 {
		root = uriToPath(params.WorkspaceF[0].URI)
	}

	s.mu.Lock()
	s.root = root
	s.indexPath = filepath.Join(root, ".clew", "index.scip")
	s.mu.Unlock()

	s.reload()

	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding":        "utf-16",
			"textDocumentSync":        1, // full; clew does not use the content
			"definitionProvider":      true,
			"referencesProvider":      true,
			"documentSymbolProvider":  true,
			"workspaceSymbolProvider": true,
			"typeDefinitionProvider":  false,
			"implementationProvider":  false,
		},
		"serverInfo": map[string]any{"name": "clew", "version": "0.0.0-dev"},
	}
}

func (s *Server) reload() {
	s.mu.RLock()
	path := s.indexPath
	s.mu.RUnlock()
	if path == "" {
		return
	}
	store, err := index.Load(path)
	if err != nil {
		s.logf("could not load index %s: %v", path, err)
		return
	}
	docs, occ, syms := store.Stats()
	s.logf("loaded %s: %d documents, %d occurrences, %d symbols", path, docs, occ, syms)

	s.mu.Lock()
	s.store = store
	s.mu.Unlock()
}

// ---------------------------------------------------------------- requests

type textPosParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position struct {
		Line      int32 `json:"line"`
		Character int32 `json:"character"`
	} `json:"position"`
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

// resolve maps an LSP request position onto (relative path, symbol).
func (s *Server) resolve(m *message) (*index.Store, string, string, bool) {
	var p textPosParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return nil, "", "", false
	}
	s.mu.RLock()
	store, root := s.store, s.root
	s.mu.RUnlock()
	if store == nil {
		return nil, "", "", false
	}
	rel, err := filepath.Rel(root, uriToPath(p.TextDocument.URI))
	if err != nil {
		return nil, "", "", false
	}
	rel = filepath.ToSlash(rel)
	sym, ok := store.SymbolAt(rel, p.Position.Line, p.Position.Character)
	if !ok {
		return nil, "", "", false
	}
	return store, rel, sym, true
}

func (s *Server) definition(m *message) any {
	store, rel, sym, ok := s.resolve(m)
	if !ok {
		return []any{}
	}
	return s.toLSPLocations(store.Definitions(rel, sym))
}

func (s *Server) references(m *message) any {
	var p textPosParams
	_ = json.Unmarshal(m.Params, &p)
	store, rel, sym, ok := s.resolve(m)
	if !ok {
		return []any{}
	}
	return s.toLSPLocations(store.References(rel, sym, p.Context.IncludeDeclaration))
}

func (s *Server) documentSymbol(m *message) any {
	var p textPosParams
	_ = json.Unmarshal(m.Params, &p)
	s.mu.RLock()
	store, root := s.store, s.root
	s.mu.RUnlock()
	if store == nil {
		return []any{}
	}
	rel, err := filepath.Rel(root, uriToPath(p.TextDocument.URI))
	if err != nil {
		return []any{}
	}
	doc, ok := store.Document(filepath.ToSlash(rel))
	if !ok {
		return []any{}
	}

	out := []any{}
	for _, occ := range doc.Occurrences {
		if !index.IsDefinition(occ) || index.IsLocal(occ.Symbol) {
			continue
		}
		r, ok := index.OccurrenceRange(occ)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":          displayName(occ.Symbol),
			"kind":          12, // Function; refined once SymbolInformation.kind is wired
			"location":      s.location(rel, r),
			"containerName": "",
		})
	}
	return out
}

func (s *Server) workspaceSymbol(m *message) any {
	var p struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(m.Params, &p)

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()
	if store == nil {
		return []any{}
	}

	out := []any{}
	for _, si := range store.SearchSymbols(p.Query, 200) {
		locs := store.Definitions("", si.Symbol)
		if len(locs) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"name":     displayName(si.Symbol),
			"kind":     12,
			"location": s.location(locs[0].Path, locs[0].Range),
		})
	}
	return out
}

// ---------------------------------------------------------------- helpers

func (s *Server) toLSPLocations(locs []index.Location) any {
	out := []any{}
	for _, l := range locs {
		out = append(out, s.location(l.Path, l.Range))
	}
	return out
}

func (s *Server) location(rel string, r index.Range) map[string]any {
	s.mu.RLock()
	root := s.root
	s.mu.RUnlock()
	return map[string]any{
		"uri": pathToURI(filepath.Join(root, rel)),
		"range": map[string]any{
			// NOTE: SCIP indexes here are UTF-8 offset based while LSP defaults to
			// UTF-16. For ASCII source they coincide. Correct handling needs the
			// document text to transcode offsets -- tracked as a known limitation.
			"start": map[string]any{"line": r.StartLine, "character": r.StartChar},
			"end":   map[string]any{"line": r.EndLine, "character": r.EndChar},
		},
	}
}

// displayName extracts a human-readable trailing name from a SCIP symbol.
func displayName(symbol string) string {
	fields := strings.Fields(symbol)
	if len(fields) == 0 {
		return symbol
	}
	desc := fields[len(fields)-1]
	desc = strings.TrimRight(desc, "#.()/:")
	if i := strings.LastIndexAny(desc, "/#."); i >= 0 && i+1 < len(desc) {
		desc = desc[i+1:]
	}
	return strings.Trim(desc, "`")
}

func uriToPath(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return u.Path
	}
	return p
}

func pathToURI(path string) string {
	return "file://" + (&url.URL{Path: path}).EscapedPath()
}

// ---------------------------------------------------------------- framing

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
}

func (m *message) reply(result any) *message {
	if m.ID == nil {
		return nil
	}
	if result == nil {
		result = struct{}{}
	}
	return &message{JSONRPC: "2.0", ID: m.ID, Result: result}
}

func readMessage(r *bufio.Reader) (*message, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok &&
			strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
		}
	}
	if length == 0 {
		return nil, fmt.Errorf("message without Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func writeMessage(w *bufio.Writer, m *message) error {
	blob, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(blob)); err != nil {
		return err
	}
	if _, err := w.Write(blob); err != nil {
		return err
	}
	return w.Flush()
}
