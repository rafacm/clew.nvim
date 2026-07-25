package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// Tier 1. The server is exercised end-to-end over an in-memory stdio pipe
// against an index built programmatically with the Go bindings: no editor, no
// indexer, no toolchain.

// ------------------------------------------------------------------- fixtures

func def(symbol string, line, startChar, endChar int32) *scip.Occurrence {
	return &scip.Occurrence{
		Symbol:      symbol,
		SymbolRoles: int32(scip.SymbolRole_Definition),
		TypedRange: &scip.Occurrence_SingleLineRange{
			SingleLineRange: &scip.SingleLineRange{
				Line: line, StartCharacter: startChar, EndCharacter: endChar,
			},
		},
	}
}

func ref(symbol string, line, startChar, endChar int32) *scip.Occurrence {
	o := def(symbol, line, startChar, endChar)
	o.SymbolRoles = 0
	return o
}

const fooSymbol = "scip-java maven maven/org.example/svc 1.0.0 org/example/Foo#"

// project writes a project root containing .clew/index.scip and returns the
// root. Foo is defined in Foo.java and referenced twice from Bar.java.
func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	idx := &scip.Index{
		Metadata: &scip.Metadata{
			ProjectRoot:          "file://" + root,
			TextDocumentEncoding: scip.TextEncoding_UTF8,
			ToolInfo:             &scip.ToolInfo{Name: "clew", Version: "test"},
		},
		Documents: []*scip.Document{
			{
				RelativePath: "src/Foo.java",
				Occurrences:  []*scip.Occurrence{def(fooSymbol, 2, 13, 16), def("local 0", 4, 8, 9)},
				Symbols:      []*scip.SymbolInformation{{Symbol: fooSymbol}},
			},
			{
				RelativePath: "src/Bar.java",
				Occurrences:  []*scip.Occurrence{ref(fooSymbol, 5, 8, 11), ref(fooSymbol, 9, 2, 5)},
			},
		},
	}

	blob, err := proto.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".clew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".clew", "index.scip"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// ------------------------------------------------------------ session harness

// session drives Serve over a pipe: it writes every request, reads every
// response, and returns the results keyed by request id.
type session struct {
	t   *testing.T
	in  bytes.Buffer
	id  int
	log bytes.Buffer
}

func newSession(t *testing.T) *session { return &session{t: t} }

func (s *session) request(method string, params any) int {
	s.t.Helper()
	s.id++
	s.write(map[string]any{
		"jsonrpc": "2.0", "id": s.id, "method": method, "params": params,
	})
	return s.id
}

func (s *session) notify(method string, params any) {
	s.t.Helper()
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *session) write(msg map[string]any) {
	s.t.Helper()
	blob, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatal(err)
	}
	fmt.Fprintf(&s.in, "Content-Length: %d\r\n\r\n%s", len(blob), blob)
}

// run serves the queued requests to completion and returns id -> result.
func (s *session) run() map[int]json.RawMessage {
	s.t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), &s.in, &out, &s.log); err != nil {
		s.t.Fatalf("Serve: %v", err)
	}

	results := map[int]json.RawMessage{}
	r := bufio.NewReader(&out)
	for {
		m, err := readMessage(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			s.t.Fatalf("reading response: %v", err)
		}
		var id int
		if err := json.Unmarshal(m.ID, &id); err != nil {
			s.t.Fatalf("response with a non-integer id %s", m.ID)
		}
		blob, err := json.Marshal(m.Result)
		if err != nil {
			s.t.Fatal(err)
		}
		results[id] = blob
	}
	return results
}

func (s *session) initialize(root string) {
	s.t.Helper()
	s.request("initialize", map[string]any{"rootUri": pathToURI(root)})
	s.notify("initialized", map[string]any{})
}

type lspLocation struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct{ Line, Character int32 } `json:"start"`
		End   struct{ Line, Character int32 } `json:"end"`
	} `json:"range"`
}

func decodeLocations(t *testing.T, raw json.RawMessage) []lspLocation {
	t.Helper()
	var locs []lspLocation
	if err := json.Unmarshal(raw, &locs); err != nil {
		t.Fatalf("decoding locations from %s: %v", raw, err)
	}
	return locs
}

func textPos(root, rel string, line, char int32) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(filepath.Join(root, rel))},
		"position":     map[string]any{"line": line, "character": char},
	}
}

// ------------------------------------------------------------------ lifecycle

func TestServer_InitializeAdvertisesTheCapabilitiesClewActuallyServes(t *testing.T) {
	s := newSession(t)
	id := s.request("initialize", map[string]any{"rootUri": pathToURI(project(t))})

	var result struct {
		Capabilities map[string]any `json:"capabilities"`
		ServerInfo   struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(s.run()[id], &result); err != nil {
		t.Fatal(err)
	}

	for _, cap := range []string{
		"definitionProvider", "referencesProvider",
		"documentSymbolProvider", "workspaceSymbolProvider",
	} {
		if result.Capabilities[cap] != true {
			t.Errorf("capability %s = %v, want true", cap, result.Capabilities[cap])
		}
	}
	// Declared false rather than omitted: clew has no type information.
	for _, cap := range []string{"typeDefinitionProvider", "implementationProvider"} {
		if result.Capabilities[cap] != false {
			t.Errorf("capability %s = %v, want false", cap, result.Capabilities[cap])
		}
	}
	if result.ServerInfo.Name != "clew" {
		t.Errorf("serverInfo.name = %q, want %q", result.ServerInfo.Name, "clew")
	}
}

// An editor that sends workspaceFolders but no rootUri must still get a root,
// or every subsequent request silently returns nothing.
func TestServer_InitializeFallsBackToWorkspaceFolders(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.request("initialize", map[string]any{
		"workspaceFolders": []map[string]any{{"uri": pathToURI(root)}},
	})
	id := s.request("textDocument/definition", textPos(root, "src/Bar.java", 5, 9))

	if got := decodeLocations(t, s.run()[id]); len(got) != 1 {
		t.Fatalf("definition returned %d locations, want 1 -- the root was not resolved", len(got))
	}
}

// A project with no index yet must serve empty results rather than fail: the
// user's next action is `:ClewIndex`, not a crash.
func TestServer_MissingIndexServesEmptyResults(t *testing.T) {
	root := t.TempDir()
	s := newSession(t)
	s.initialize(root)
	id := s.request("textDocument/definition", textPos(root, "src/Bar.java", 5, 9))

	if got := decodeLocations(t, s.run()[id]); len(got) != 0 {
		t.Errorf("definition returned %v, want an empty list", got)
	}
	if !strings.Contains(s.log.String(), "could not load index") {
		t.Errorf("the missing index was not logged; log = %q", s.log.String())
	}
}

// The result of a request must be an empty JSON array, never null: some clients
// treat null as an error rather than as "no results".
func TestServer_EmptyResultsAreArraysNotNull(t *testing.T) {
	root := t.TempDir()
	s := newSession(t)
	s.initialize(root)
	ids := map[string]int{
		"definition":     s.request("textDocument/definition", textPos(root, "x.java", 0, 0)),
		"references":     s.request("textDocument/references", textPos(root, "x.java", 0, 0)),
		"documentSymbol": s.request("textDocument/documentSymbol", textPos(root, "x.java", 0, 0)),
		"workspaceSymbol": s.request("workspace/symbol",
			map[string]any{"query": "anything"}),
	}
	results := s.run()
	for name, id := range ids {
		if got := string(results[id]); got != "[]" {
			t.Errorf("%s returned %s, want []", name, got)
		}
	}
}

func TestServer_NotificationsGetNoReply(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	for _, m := range []string{
		"textDocument/didOpen", "textDocument/didChange",
		"textDocument/didSave", "textDocument/didClose",
	} {
		s.notify(m, map[string]any{})
	}
	id := s.request("shutdown", nil)

	results := s.run()
	if len(results) != 2 { // initialize + shutdown
		t.Errorf("got %d responses, want 2 -- a notification was replied to", len(results))
	}
	if _, ok := results[id]; !ok {
		t.Error("shutdown got no reply")
	}
}

// `exit` ends the loop, and anything queued behind it must never be served.
func TestServer_ExitStopsTheLoop(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	s.notify("exit", nil)
	after := s.request("textDocument/definition", textPos(root, "src/Bar.java", 5, 9))

	if _, ok := s.run()[after]; ok {
		t.Error("a request queued after exit was served")
	}
}

// An unknown request still needs a reply, or the client blocks forever waiting
// for one. An unknown notification must not produce one.
func TestServer_UnknownMethods(t *testing.T) {
	s := newSession(t)
	s.initialize(project(t))
	id := s.request("textDocument/hover", map[string]any{})
	s.notify("$/cancelRequest", map[string]any{})

	results := s.run()
	if _, ok := results[id]; !ok {
		t.Error("an unknown request got no reply; the client would block")
	}
	if len(results) != 2 { // initialize + hover
		t.Errorf("got %d responses, want 2 -- the unknown notification was replied to", len(results))
	}
}

// The watcher notification is how the server picks up a freshly built index
// without a restart.
func TestServer_DidChangeWatchedFilesReloadsTheIndex(t *testing.T) {
	root := t.TempDir()
	s := newSession(t)
	s.initialize(root) // no index yet

	// The index appears between initialize and the reload.
	built := project(t)
	blob, err := os.ReadFile(filepath.Join(built, ".clew", "index.scip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".clew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".clew", "index.scip"), blob, 0o644); err != nil {
		t.Fatal(err)
	}

	s.notify("workspace/didChangeWatchedFiles", map[string]any{})
	id := s.request("textDocument/definition", textPos(root, "src/Bar.java", 5, 9))

	if got := decodeLocations(t, s.run()[id]); len(got) != 1 {
		t.Fatalf("definition returned %d locations after a reload, want 1", len(got))
	}
}

// ------------------------------------------------------------------- requests

func TestServer_Definition(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	id := s.request("textDocument/definition", textPos(root, "src/Bar.java", 5, 9))

	locs := decodeLocations(t, s.run()[id])
	if len(locs) != 1 {
		t.Fatalf("got %d locations, want 1", len(locs))
	}
	if want := pathToURI(filepath.Join(root, "src/Foo.java")); locs[0].URI != want {
		t.Errorf("uri = %q, want %q", locs[0].URI, want)
	}
	if locs[0].Range.Start.Line != 2 || locs[0].Range.Start.Character != 13 {
		t.Errorf("start = %v, want line 2 char 13", locs[0].Range.Start)
	}
	if locs[0].Range.End.Line != 2 || locs[0].Range.End.Character != 16 {
		t.Errorf("end = %v, want line 2 char 16", locs[0].Range.End)
	}
}

func TestServer_Definition_OutsideAnyOccurrence(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	id := s.request("textDocument/definition", textPos(root, "src/Bar.java", 99, 0))

	if got := decodeLocations(t, s.run()[id]); len(got) != 0 {
		t.Errorf("got %v, want no locations", got)
	}
}

func TestServer_References(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)

	params := textPos(root, "src/Foo.java", 2, 14)
	without := s.request("textDocument/references", params)

	withDecl := map[string]any{}
	for k, v := range params {
		withDecl[k] = v
	}
	withDecl["context"] = map[string]any{"includeDeclaration": true}
	with := s.request("textDocument/references", withDecl)

	results := s.run()
	if got := decodeLocations(t, results[without]); len(got) != 2 {
		t.Errorf("references without the declaration returned %d, want 2", len(got))
	}
	if got := decodeLocations(t, results[with]); len(got) != 3 {
		t.Errorf("references with the declaration returned %d, want 3", len(got))
	}
}

func TestServer_DocumentSymbol(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	id := s.request("textDocument/documentSymbol", textPos(root, "src/Foo.java", 0, 0))

	var syms []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location lspLocation
	}
	if err := json.Unmarshal(s.run()[id], &syms); err != nil {
		t.Fatal(err)
	}

	// Only the global definition: references and `local 0` are both excluded.
	if len(syms) != 1 {
		t.Fatalf("got %d symbols, want 1: %+v", len(syms), syms)
	}
	if syms[0].Name != "Foo" {
		t.Errorf("name = %q, want %q", syms[0].Name, "Foo")
	}
}

func TestServer_WorkspaceSymbol(t *testing.T) {
	root := project(t)
	s := newSession(t)
	s.initialize(root)
	match := s.request("workspace/symbol", map[string]any{"query": "Foo"})
	miss := s.request("workspace/symbol", map[string]any{"query": "Nonexistent"})

	results := s.run()
	var syms []struct {
		Name     string `json:"name"`
		Location lspLocation
	}
	if err := json.Unmarshal(results[match], &syms); err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "Foo" {
		t.Fatalf("got %+v, want one symbol named Foo", syms)
	}
	if want := pathToURI(filepath.Join(root, "src/Foo.java")); syms[0].Location.URI != want {
		t.Errorf("uri = %q, want %q", syms[0].Location.URI, want)
	}
	if got := string(results[miss]); got != "[]" {
		t.Errorf("unmatched query returned %s, want []", got)
	}
}

// -------------------------------------------------------------------- helpers

func TestDisplayName(t *testing.T) {
	cases := []struct{ symbol, want string }{
		{"scip-java maven maven/org.example/svc 1.0.0 org/example/Foo#", "Foo"},
		{"scip-java maven maven/org.example/svc 1.0.0 org/example/Foo#bar().", "bar"},
		{"scip-typescript npm pkg 1.0.0 src/`index.ts`/Widget#", "Widget"},
		{"Foo#", "Foo"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := displayName(tc.symbol); got != tc.want {
			t.Errorf("displayName(%q) = %q, want %q", tc.symbol, got, tc.want)
		}
	}
}

func TestURIRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("clew supports Windows through WSL, where paths are POSIX")
	}
	for _, path := range []string{
		"/proj/src/Foo.java",
		"/proj/a b/Foo.java",  // a space must survive percent-encoding
		"/proj/über/Foo.java", // and so must non-ASCII
		"/proj/a+b/Foo.java",  // '+' is not a space in a path
		"/proj/100%/Foo.java", // a literal percent
	} {
		if got := uriToPath(pathToURI(path)); got != path {
			t.Errorf("round trip of %q gave %q", path, got)
		}
	}
}

func TestURIToPath_Empty(t *testing.T) {
	if got := uriToPath(""); got != "" {
		t.Errorf("uriToPath(\"\") = %q, want empty", got)
	}
}

// ------------------------------------------------------------------- framing

func TestReadMessage_Framing(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	raw := fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc\r\n\r\n%s", len(body), body)

	m, err := readMessage(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Method != "initialize" {
		t.Errorf("method = %q, want %q", m.Method, "initialize")
	}
}

// The header name is case-insensitive per the LSP base protocol.
func TestReadMessage_ContentLengthIsCaseInsensitive(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"exit"}`
	raw := fmt.Sprintf("content-length: %d\r\n\r\n%s", len(body), body)

	if _, err := readMessage(bufio.NewReader(strings.NewReader(raw))); err != nil {
		t.Fatalf("readMessage rejected a lowercase header: %v", err)
	}
}

func TestReadMessage_Errors(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"no Content-Length", "X-Other: 1\r\n\r\n{}"},
		{"unparseable Content-Length", "Content-Length: abc\r\n\r\n{}"},
		{"body is not JSON", "Content-Length: 3\r\n\r\nnot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readMessage(bufio.NewReader(strings.NewReader(tc.raw))); err == nil {
				t.Error("readMessage returned no error")
			}
		})
	}
}

func TestReadMessage_EOFIsCleanTermination(t *testing.T) {
	if _, err := readMessage(bufio.NewReader(strings.NewReader(""))); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestWriteMessage_EmitsAContentLengthHeader(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := writeMessage(w, &message{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: []any{}}); err != nil {
		t.Fatal(err)
	}

	head, body, ok := strings.Cut(buf.String(), "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in %q", buf.String())
	}
	var length int
	if _, err := fmt.Sscanf(head, "Content-Length: %d", &length); err != nil {
		t.Fatalf("bad header %q: %v", head, err)
	}
	if length != len(body) {
		t.Errorf("Content-Length = %d, body is %d bytes", length, len(body))
	}
}

// A reply to a notification is a protocol violation, so reply() must refuse to
// build one.
func TestMessage_ReplyToANotificationIsNil(t *testing.T) {
	if got := (&message{Method: "initialized"}).reply("x"); got != nil {
		t.Errorf("reply = %v, want nil for a message with no id", got)
	}
}

// A nil result marshals to `null`, which some clients reject. reply()
// substitutes an empty object.
func TestMessage_ReplyToShutdownIsNotNull(t *testing.T) {
	m := (&message{ID: json.RawMessage("1")}).reply(nil)
	blob, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), `"result":null`) {
		t.Errorf("reply = %s, want a non-null result", blob)
	}
}
