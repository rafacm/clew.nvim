package index

import (
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
)

// ------------------------------------------------------------ OccurrenceRange

// SCIP uses three range encodings and a consumer must handle all of them. The
// deprecated packed `range` comes back EMPTY from modern writers, so code that
// reads only GetRange() panics with an index-out-of-range on the first
// occurrence of real data. This is the single most common way to get a
// working-looking SCIP consumer that crashes.

func TestOccurrenceRange_SingleLine(t *testing.T) {
	got, ok := OccurrenceRange(def("S", 10, 4, 12))
	if !ok {
		t.Fatal("OccurrenceRange returned ok=false for a single_line_range")
	}
	if want := (Range{10, 4, 10, 12}); got != want {
		t.Errorf("range = %v, want %v", got, want)
	}
}

func TestOccurrenceRange_MultiLine(t *testing.T) {
	got, ok := OccurrenceRange(multiLineDef("S", 10, 4, 14, 1))
	if !ok {
		t.Fatal("OccurrenceRange returned ok=false for a multi_line_range")
	}
	if want := (Range{10, 4, 14, 1}); got != want {
		t.Errorf("range = %v, want %v", got, want)
	}
}

// Three elements: [startLine, startCharacter, endCharacter]; the end line is
// inferred to equal the start line.
func TestOccurrenceRange_DeprecatedPackedThreeElement(t *testing.T) {
	got, ok := OccurrenceRange(packedDef("S", 10, 4, 12))
	if !ok {
		t.Fatal("OccurrenceRange returned ok=false for a three-element packed range")
	}
	if want := (Range{10, 4, 10, 12}); got != want {
		t.Errorf("range = %v, want %v", got, want)
	}
}

// Four elements: [startLine, startCharacter, endLine, endCharacter].
func TestOccurrenceRange_DeprecatedPackedFourElement(t *testing.T) {
	got, ok := OccurrenceRange(packedDef("S", 10, 4, 14, 1))
	if !ok {
		t.Fatal("OccurrenceRange returned ok=false for a four-element packed range")
	}
	if want := (Range{10, 4, 14, 1}); got != want {
		t.Errorf("range = %v, want %v", got, want)
	}
}

// An occurrence with no range at all must be reported, not guessed at. This is
// the case that panics a naive consumer.
func TestOccurrenceRange_NoRangeIsReportedNotGuessed(t *testing.T) {
	for _, tc := range []struct {
		name string
		occ  *scip.Occurrence
	}{
		{"no encoding set", &scip.Occurrence{Symbol: "S"}},
		{"empty packed range", packedDef("S")},
		{"malformed packed range", packedDef("S", 1, 2)},
		{"over-long packed range", packedDef("S", 1, 2, 3, 4, 5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := OccurrenceRange(tc.occ); ok {
				t.Error("OccurrenceRange returned ok=true, want false")
			}
		})
	}
}

// A typed range wins over the deprecated packed field when both are set: the
// SCIP schema says typed_range takes precedence.
func TestOccurrenceRange_TypedRangeWinsOverPacked(t *testing.T) {
	o := def("S", 10, 4, 12)
	o.Range = []int32{99, 99, 99}
	got, ok := OccurrenceRange(o)
	if !ok {
		t.Fatal("OccurrenceRange returned ok=false")
	}
	if want := (Range{10, 4, 10, 12}); got != want {
		t.Errorf("range = %v, want %v (the packed range must not win)", got, want)
	}
}

// Ranges survive a marshal/unmarshal round trip, which is where the deprecated
// packed field's emptiness actually bites.
func TestOccurrenceRange_SurvivesSerialisation(t *testing.T) {
	idx := indexOf("/proj", doc("A.java",
		def("single", 1, 0, 5),
		multiLineDef("multi", 2, 0, 4, 3),
	))
	loaded := loadIndex(t, writeIndex(t, t.TempDir(), "i.scip", idx))

	want := map[string]Range{
		"single": {1, 0, 1, 5},
		"multi":  {2, 0, 4, 3},
	}
	for _, o := range loaded.Documents[0].Occurrences {
		got, ok := OccurrenceRange(o)
		if !ok {
			t.Fatalf("%s: OccurrenceRange returned ok=false after a round trip", o.Symbol)
		}
		if got != want[o.Symbol] {
			t.Errorf("%s: range = %v, want %v", o.Symbol, got, want[o.Symbol])
		}
	}
}

// --------------------------------------------------------------------- Covers

func TestRange_Covers(t *testing.T) {
	single := Range{5, 4, 5, 10}
	multi := Range{5, 4, 8, 2}

	cases := []struct {
		name string
		r    Range
		line int32
		char int32
		want bool
	}{
		{"single: start boundary", single, 5, 4, true},
		{"single: end boundary", single, 5, 10, true},
		{"single: inside", single, 5, 7, true},
		{"single: before start char", single, 5, 3, false},
		{"single: after end char", single, 5, 11, false},
		{"single: line above", single, 4, 7, false},
		{"single: line below", single, 6, 7, false},
		{"multi: on start line, inside", multi, 5, 4, true},
		{"multi: on start line, before", multi, 5, 3, false},
		{"multi: interior line, any char", multi, 6, 999, true},
		{"multi: on end line, inside", multi, 8, 2, true},
		{"multi: on end line, after", multi, 8, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Covers(tc.line, tc.char); got != tc.want {
				t.Errorf("Covers(%d, %d) = %v, want %v", tc.line, tc.char, got, tc.want)
			}
		})
	}
}

// ------------------------------------------------------------ roles and scope

func TestIsDefinition(t *testing.T) {
	if !IsDefinition(def("S", 0, 0, 1)) {
		t.Error("IsDefinition = false for an occurrence carrying the Definition role")
	}
	if IsDefinition(ref("S", 0, 0, 1)) {
		t.Error("IsDefinition = true for a reference")
	}
	// The role field is a bitset: Definition must still be seen alongside others.
	o := def("S", 0, 0, 1)
	o.SymbolRoles |= int32(scip.SymbolRole_Import)
	if !IsDefinition(o) {
		t.Error("IsDefinition = false when Definition is combined with another role")
	}
	// A non-Definition role must not read as a definition.
	o2 := ref("S", 0, 0, 1)
	o2.SymbolRoles = int32(scip.SymbolRole_ReadAccess)
	if IsDefinition(o2) {
		t.Error("IsDefinition = true for a ReadAccess-only occurrence")
	}
}

func TestIsLocal(t *testing.T) {
	for _, s := range []string{"local 0", "local 12"} {
		if !IsLocal(s) {
			t.Errorf("IsLocal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"scip-java maven maven/org.example/svc 1.0.0 org/example/Foo#",
		"locale 0",
		"",
	} {
		if IsLocal(s) {
			t.Errorf("IsLocal(%q) = true, want false", s)
		}
	}
}

// ------------------------------------------------------------------- SymbolAt

func TestStore_SymbolAt(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("A.java", def("Foo#", 3, 13, 16), ref("Bar#", 7, 4, 7)),
	))

	if sym, ok := s.SymbolAt("A.java", 3, 14); !ok || sym != "Foo#" {
		t.Errorf("SymbolAt inside a definition = (%q, %v), want (Foo#, true)", sym, ok)
	}
	if sym, ok := s.SymbolAt("A.java", 7, 5); !ok || sym != "Bar#" {
		t.Errorf("SymbolAt inside a reference = (%q, %v), want (Bar#, true)", sym, ok)
	}
	if _, ok := s.SymbolAt("A.java", 99, 0); ok {
		t.Error("SymbolAt outside any occurrence returned ok=true")
	}
	if _, ok := s.SymbolAt("Nonexistent.java", 0, 0); ok {
		t.Error("SymbolAt in an unknown document returned ok=true")
	}
}

// Occurrences nest: a method body's range encloses the identifiers inside it.
// The innermost match is the one the user pointed at.
func TestStore_SymbolAt_PrefersTheTightestEnclosingOccurrence(t *testing.T) {
	s := storeFrom(t, indexOf("/proj", doc("A.java",
		multiLineDef("Class#", 0, 0, 20, 1),         // whole class
		multiLineDef("Class#method().", 3, 2, 9, 3), // whole method
		def("Class#method().(arg)", 3, 20, 23),      // the identifier
	)))

	if sym, _ := s.SymbolAt("A.java", 3, 21); sym != "Class#method().(arg)" {
		t.Errorf("SymbolAt = %q, want the innermost symbol", sym)
	}
	if sym, _ := s.SymbolAt("A.java", 5, 0); sym != "Class#method()." {
		t.Errorf("SymbolAt = %q, want the enclosing method", sym)
	}
	if sym, _ := s.SymbolAt("A.java", 18, 0); sym != "Class#" {
		t.Errorf("SymbolAt = %q, want the enclosing class", sym)
	}
}

// ------------------------------------------------------- definitions and refs

func TestStore_DefinitionsAndReferences(t *testing.T) {
	sym := "scip-java maven maven/org.example/svc 1.0.0 org/example/Foo#"
	s := storeFrom(t, indexOf("/proj",
		doc("src/Foo.java", def(sym, 2, 13, 16)),
		doc("src/Bar.java", ref(sym, 5, 8, 11), ref(sym, 9, 2, 5)),
	))

	assertLocations(t, s.Definitions("src/Bar.java", sym), []Location{
		{Path: "src/Foo.java", Range: Range{2, 13, 2, 16}},
	})

	assertLocations(t, s.References("src/Bar.java", sym, false), []Location{
		{Path: "src/Bar.java", Range: Range{5, 8, 5, 11}},
		{Path: "src/Bar.java", Range: Range{9, 2, 9, 5}},
	})

	assertLocations(t, s.References("src/Bar.java", sym, true), []Location{
		{Path: "src/Bar.java", Range: Range{5, 8, 5, 11}},
		{Path: "src/Bar.java", Range: Range{9, 2, 9, 5}},
		{Path: "src/Foo.java", Range: Range{2, 13, 2, 16}},
	})
}

func TestStore_ReferencesAreSortedByPathThenLine(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("z.java", ref("S", 1, 0, 1)),
		doc("a.java", ref("S", 9, 0, 1), ref("S", 2, 0, 1)),
	))
	assertLocations(t, s.References("a.java", "S", false), []Location{
		{Path: "a.java", Range: Range{2, 0, 2, 1}},
		{Path: "a.java", Range: Range{9, 0, 9, 1}},
		{Path: "z.java", Range: Range{1, 0, 1, 1}},
	})
}

// References must not hand out the Store's own slice: appending the definitions
// to it would corrupt the cached references for every later caller.
func TestStore_ReferencesDoesNotAliasInternalState(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("Foo.java", def("S", 0, 0, 1)),
		doc("Bar.java", ref("S", 1, 0, 1)),
	))
	if n := len(s.References("Bar.java", "S", true)); n != 2 {
		t.Fatalf("first call returned %d locations, want 2", n)
	}
	if n := len(s.References("Bar.java", "S", false)); n != 1 {
		t.Errorf("second call returned %d locations, want 1 -- includeDefinition leaked", n)
	}
}

// `local 0` recurs in every document of every unit, so it must never be used as
// a global join key. Two files each defining `local 0` must not see each other.
func TestStore_LocalSymbolsAreDocumentScoped(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("A.java", def("local 0", 1, 4, 5), ref("local 0", 3, 8, 9)),
		doc("B.java", def("local 0", 7, 4, 5), ref("local 0", 8, 8, 9)),
	))

	assertLocations(t, s.Definitions("A.java", "local 0"), []Location{
		{Path: "A.java", Range: Range{1, 4, 1, 5}},
	})
	assertLocations(t, s.Definitions("B.java", "local 0"), []Location{
		{Path: "B.java", Range: Range{7, 4, 7, 5}},
	})
	assertLocations(t, s.References("A.java", "local 0", false), []Location{
		{Path: "A.java", Range: Range{3, 8, 3, 9}},
	})
}

// A local symbol is not a symbol anyone can search for, and putting it in the
// symbol table would fill workspace/symbol with `local 0` from every file.
func TestStore_LocalSymbolsAreNotInTheSymbolTable(t *testing.T) {
	idx := indexOf("/proj", doc("A.java", def("Foo#", 0, 0, 3)))
	idx.Documents[0].Symbols = append(idx.Documents[0].Symbols,
		&scip.SymbolInformation{Symbol: "local 0"})

	s := storeFrom(t, idx)
	for _, si := range s.SearchSymbols("", 0) {
		if IsLocal(si.Symbol) {
			t.Errorf("SearchSymbols returned the local symbol %q", si.Symbol)
		}
	}
}

// --------------------------------------------------------------------- search

func TestStore_SearchSymbols(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("A.java", def("org/example/StringUtils#", 0, 0, 1)),
		doc("B.java", def("org/example/NumberUtils#", 0, 0, 1)),
		doc("C.java", def("org/example/Widget#", 0, 0, 1)),
	))

	if got := len(s.SearchSymbols("", 0)); got != 3 {
		t.Errorf("empty query matched %d symbols, want all 3", got)
	}
	if got := len(s.SearchSymbols("utils", 0)); got != 2 {
		t.Errorf("query %q matched %d symbols, want 2 (matching is case-insensitive)", "utils", got)
	}
	if got := len(s.SearchSymbols("StringUtils", 0)); got != 1 {
		t.Errorf("query %q matched %d symbols, want 1", "StringUtils", got)
	}
	if got := len(s.SearchSymbols("nothinghere", 0)); got != 0 {
		t.Errorf("unmatched query returned %d symbols, want 0", got)
	}
	if got := len(s.SearchSymbols("", 2)); got != 2 {
		t.Errorf("limit=2 returned %d symbols, want 2", got)
	}
}

// external_symbols describe symbols this index references but does not define.
// They belong in the symbol table so workspace/symbol can name them.
func TestStore_ExternalSymbolsAreInTheSymbolTable(t *testing.T) {
	idx := indexOf("/proj", doc("A.java", ref("org/other/Dep#", 0, 0, 3)))
	idx.ExternalSymbols = []*scip.SymbolInformation{{Symbol: "org/other/Dep#"}}

	s := storeFrom(t, idx)
	if got := len(s.SearchSymbols("Dep", 0)); got != 1 {
		t.Errorf("external symbol not searchable: matched %d, want 1", got)
	}
}

// ------------------------------------------------------------ load and stats

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/index.scip"); err == nil {
		t.Fatal("Load of a missing file returned no error")
	}
}

func TestStore_Document(t *testing.T) {
	s := storeFrom(t, indexOf("/proj", doc("src/A.java", def("Foo#", 0, 0, 3))))
	if d, ok := s.Document("src/A.java"); !ok || d.RelativePath != "src/A.java" {
		t.Errorf("Document = (%v, %v), want the document", d, ok)
	}
	if _, ok := s.Document("nope.java"); ok {
		t.Error("Document returned ok=true for an unknown path")
	}
}

func TestStore_Stats(t *testing.T) {
	s := storeFrom(t, indexOf("/proj",
		doc("A.java", def("Foo#", 0, 0, 3), ref("Bar#", 1, 0, 3)),
		doc("B.java", def("Bar#", 0, 0, 3)),
	))
	docs, occs, syms := s.Stats()
	if docs != 2 {
		t.Errorf("documents = %d, want 2", docs)
	}
	if occs != 3 {
		t.Errorf("occurrences = %d, want 3", occs)
	}
	if syms != 2 {
		t.Errorf("symbols = %d, want 2 (Foo# and Bar#)", syms)
	}
}
