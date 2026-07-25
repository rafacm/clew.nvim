package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// Tier 1 support. Indexes are constructed programmatically with the Go bindings
// and marshalled in memory or into t.TempDir(), so there are no committed .scip
// blobs and no indexer is ever invoked.
//
// The builders below exist so a test reads as the *shape* it is asserting on --
// a definition here, a reference there -- rather than as protobuf plumbing.

// def builds a definition occurrence using the single_line_range encoding, which
// is what modern writers emit.
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

// ref builds a reference occurrence: no roles set.
func ref(symbol string, line, startChar, endChar int32) *scip.Occurrence {
	o := def(symbol, line, startChar, endChar)
	o.SymbolRoles = 0
	return o
}

// multiLineDef builds a definition using the multi_line_range encoding.
func multiLineDef(symbol string, startLine, startChar, endLine, endChar int32) *scip.Occurrence {
	return &scip.Occurrence{
		Symbol:      symbol,
		SymbolRoles: int32(scip.SymbolRole_Definition),
		TypedRange: &scip.Occurrence_MultiLineRange{
			MultiLineRange: &scip.MultiLineRange{
				StartLine: startLine, StartCharacter: startChar,
				EndLine: endLine, EndCharacter: endChar,
			},
		},
	}
}

// packedDef builds a definition using the DEPRECATED packed int32 range. No
// modern writer emits this, but a consumer must still read it.
func packedDef(symbol string, r ...int32) *scip.Occurrence {
	return &scip.Occurrence{
		Symbol:      symbol,
		SymbolRoles: int32(scip.SymbolRole_Definition),
		Range:       r,
	}
}

func doc(path string, occs ...*scip.Occurrence) *scip.Document {
	d := &scip.Document{RelativePath: path, Occurrences: occs}
	for _, o := range occs {
		if o.GetSymbolRoles()&int32(scip.SymbolRole_Definition) != 0 && !IsLocal(o.Symbol) {
			d.Symbols = append(d.Symbols, &scip.SymbolInformation{Symbol: o.Symbol})
		}
	}
	return d
}

func indexOf(projectRoot string, docs ...*scip.Document) *scip.Index {
	return &scip.Index{
		Metadata: &scip.Metadata{
			Version:              scip.ProtocolVersion_UnspecifiedProtocolVersion,
			ProjectRoot:          "file://" + projectRoot,
			TextDocumentEncoding: scip.TextEncoding_UTF8,
			ToolInfo:             &scip.ToolInfo{Name: "test-indexer", Version: "1.0.0"},
		},
		Documents: docs,
	}
}

// writeIndex marshals idx to a file under dir and returns its path, standing in
// for whatever a producer would have written.
func writeIndex(t *testing.T, dir, name string, idx *scip.Index) string {
	t.Helper()
	blob, err := proto.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadIndex reads a marshalled index back, which is what Load and the LSP layer
// see.
func loadIndex(t *testing.T, path string) *scip.Index {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var idx scip.Index
	if err := proto.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	return &idx
}

// storeFrom marshals an index, writes it, and loads it through the real Load
// path -- never bypassing serialisation, since the deprecated packed range only
// misbehaves once it has been through the wire format.
func storeFrom(t *testing.T, idx *scip.Index) *Store {
	t.Helper()
	path := writeIndex(t, t.TempDir(), "index.scip", idx)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func docPaths(idx *scip.Index) []string {
	out := make([]string, len(idx.Documents))
	for i, d := range idx.Documents {
		out[i] = d.RelativePath
	}
	return out
}

func assertLocations(t *testing.T, got []Location, want []Location) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d locations %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("locations = %v, want %v", got, want)
		}
	}
}
