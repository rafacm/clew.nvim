package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
)

// Tier 1. Every input index is constructed programmatically and marshalled into
// t.TempDir(): no committed .scip blobs, no indexer, no network.
//
// The four merge rules each corrupt the result *silently* when broken, which is
// why each one has a test naming the failure it prevents.

// mergeInto runs Merge over the given (prefix, index) pairs and returns the
// merged index plus its stats.
func mergeInto(t *testing.T, projectRoot string, units ...unit) (*scip.Index, Stats) {
	t.Helper()
	dir := t.TempDir()

	inputs := make([]Input, len(units))
	for i, u := range units {
		name := u.prefix
		if name == "" {
			name = "root"
		}
		inputs[i] = Input{
			Prefix: u.prefix,
			Path:   writeIndex(t, dir, strings.ReplaceAll(name, "/", "_")+".scip", u.index),
		}
	}

	out := filepath.Join(dir, "merged.scip")
	stats, err := Merge(MergeOptions{ProjectRoot: projectRoot, Inputs: inputs, Output: out})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	return loadIndex(t, out), stats
}

type unit struct {
	prefix string
	index  *scip.Index
}

// A single-unit project still goes through Merge -- keeping one code path means
// path rewriting and metadata normalisation are exercised on every run rather
// than being a rarely-taken branch that rots.
func TestMerge_SingleRepository_LeavesPathsAlone(t *testing.T) {
	merged, stats := mergeInto(t, "/proj", unit{"", indexOf("/proj",
		doc("src/main/java/A.java", def("Foo#", 0, 0, 3)),
		doc("src/main/java/B.java", def("Bar#", 0, 0, 3)),
	)})

	want := []string{"src/main/java/A.java", "src/main/java/B.java"}
	if got := docPaths(merged); !equalStrings(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if stats.Documents != 2 {
		t.Errorf("stats.Documents = %d, want 2", stats.Documents)
	}
	if stats.Occurrences != 2 {
		t.Errorf("stats.Occurrences = %d, want 2", stats.Occurrences)
	}
	if stats.PathCollisions != 0 {
		t.Errorf("stats.PathCollisions = %d, want 0", stats.PathCollisions)
	}
}

// Rule 2. Document has no module or root field: relative_path is the ONLY
// disambiguator, and every Java unit on earth contains src/main/java/...
func TestMerge_Superproject_PrefixesEveryDocumentPath(t *testing.T) {
	merged, stats := mergeInto(t, "/umbrella",
		unit{"java/svc-a", indexOf("/umbrella/java/svc-a",
			doc("src/main/java/App.java", def("A#", 0, 0, 1)))},
		unit{"java/svc-b", indexOf("/umbrella/java/svc-b",
			doc("src/main/java/App.java", def("B#", 0, 0, 1)))},
		unit{"web", indexOf("/umbrella/web",
			doc("src/index.ts", def("C#", 0, 0, 1)))},
	)

	want := []string{
		"java/svc-a/src/main/java/App.java",
		"java/svc-b/src/main/java/App.java",
		"web/src/index.ts",
	}
	if got := docPaths(merged); !equalStrings(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if stats.PathCollisions != 0 {
		t.Errorf("stats.PathCollisions = %d, want 0 -- identical src/main/java paths must be disambiguated by prefix", stats.PathCollisions)
	}
}

// Rule 1. Protobuf singular-message merge is last-wins, so concatenating indexes
// without normalising metadata leaves the FINAL unit's project_root -- and every
// path in the merged index is then resolved against the wrong directory.
func TestMerge_EmitsOneMetadataCarryingTheUmbrellaRoot(t *testing.T) {
	merged, _ := mergeInto(t, "/umbrella",
		unit{"a", indexOf("/umbrella/a", doc("A.java", def("A#", 0, 0, 1)))},
		unit{"b", indexOf("/umbrella/b", doc("B.java", def("B#", 0, 0, 1)))},
	)

	md := merged.GetMetadata()
	if md == nil {
		t.Fatal("merged index has no metadata")
	}
	if want := "file:///umbrella"; md.GetProjectRoot() != want {
		t.Errorf("project_root = %q, want %q (the last unit's root must not win)", md.GetProjectRoot(), want)
	}
	if got := md.GetToolInfo().GetName(); got != "clew" {
		t.Errorf("tool_info.name = %q, want %q", got, "clew")
	}
	if md.GetTextDocumentEncoding() != scip.TextEncoding_UTF8 {
		t.Errorf("text_document_encoding = %v, want UTF8", md.GetTextDocumentEncoding())
	}
}

// Rule 4. external_symbols are deduplicated by symbol string. Two units
// depending on the same library both describe its symbols.
func TestMerge_DeduplicatesExternalSymbols(t *testing.T) {
	shared := "scip-java maven maven/org.apache.commons/commons-lang3 3.20.0 org/apache/commons/lang3/StringUtils#"

	a := indexOf("/umbrella/a", doc("A.java", ref(shared, 0, 0, 1)))
	a.ExternalSymbols = []*scip.SymbolInformation{
		{Symbol: shared}, {Symbol: "only-in-a"},
	}
	b := indexOf("/umbrella/b", doc("B.java", ref(shared, 0, 0, 1)))
	b.ExternalSymbols = []*scip.SymbolInformation{
		{Symbol: shared}, {Symbol: "only-in-b"},
	}

	merged, stats := mergeInto(t, "/umbrella", unit{"a", a}, unit{"b", b})

	if stats.ExternalSymbols != 3 {
		t.Errorf("stats.ExternalSymbols = %d, want 3", stats.ExternalSymbols)
	}
	seen := map[string]int{}
	for _, s := range merged.ExternalSymbols {
		seen[s.Symbol]++
	}
	if seen[shared] != 1 {
		t.Errorf("the shared external symbol appears %d times, want 1", seen[shared])
	}
	for _, want := range []string{"only-in-a", "only-in-b"} {
		if seen[want] != 1 {
			t.Errorf("external symbol %q appears %d times, want 1", want, seen[want])
		}
	}
}

// The central claim: cross-unit resolution is a plain string join on symbol
// names, which embed the Maven coordinate including version. This is the
// hermetic version of TestAcceptance_Superproject_JavaCrossSubmodule.
func TestMerge_Superproject_CrossUnitResolutionIsAStringJoin(t *testing.T) {
	// commons-lang defines it; commons-text references it under the identical
	// symbol string, because scip-javac stamps classpath symbols with the same
	// coordinate.
	sym := "scip-java maven maven/org.apache.commons/commons-lang3 3.20.0 org/apache/commons/lang3/StringUtils#"

	provider := indexOf("/umbrella/commons-lang",
		doc("src/main/java/org/apache/commons/lang3/StringUtils.java", def(sym, 100, 13, 24)))
	consumer := indexOf("/umbrella/commons-text",
		doc("src/main/java/org/apache/commons/text/WordUtils.java", ref(sym, 42, 8, 19)))

	dir := t.TempDir()
	out := filepath.Join(dir, "merged.scip")
	if _, err := Merge(MergeOptions{
		ProjectRoot: "/umbrella",
		Inputs: []Input{
			{Prefix: "commons-lang", Path: writeIndex(t, dir, "lang.scip", provider)},
			{Prefix: "commons-text", Path: writeIndex(t, dir, "text.scip", consumer)},
		},
		Output: out,
	}); err != nil {
		t.Fatal(err)
	}

	s, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}

	consumerPath := "commons-text/src/main/java/org/apache/commons/text/WordUtils.java"
	got, ok := s.SymbolAt(consumerPath, 42, 10)
	if !ok {
		t.Fatal("no symbol at the reference site in the consuming unit")
	}
	if got != sym {
		t.Fatalf("symbol = %q, want %q", got, sym)
	}

	// Go-to-definition from the consumer lands in the provider.
	assertLocations(t, s.Definitions(consumerPath, got), []Location{{
		Path:  "commons-lang/src/main/java/org/apache/commons/lang3/StringUtils.java",
		Range: Range{100, 13, 100, 24},
	}})

	// And find-references from the provider sees the consumer.
	providerPath := "commons-lang/src/main/java/org/apache/commons/lang3/StringUtils.java"
	assertLocations(t, s.References(providerPath, sym, false), []Location{{
		Path:  consumerPath,
		Range: Range{42, 8, 42, 19},
	}})
}

// The coordinate includes the VERSION, so a version skew between two units
// silently severs navigation between them -- nothing else about the index looks
// wrong. This is what couples the commons-lang and commons-text pins.
func TestMerge_Superproject_VersionSkewSeversResolution(t *testing.T) {
	const base = "scip-java maven maven/org.apache.commons/commons-lang3 %s org/apache/commons/lang3/StringUtils#"
	defined := strings.Replace(base, "%s", "3.20.0", 1)
	referenced := strings.Replace(base, "%s", "3.19.0", 1)

	dir := t.TempDir()
	out := filepath.Join(dir, "merged.scip")
	if _, err := Merge(MergeOptions{
		ProjectRoot: "/umbrella",
		Inputs: []Input{
			{Prefix: "lang", Path: writeIndex(t, dir, "lang.scip",
				indexOf("/x", doc("StringUtils.java", def(defined, 1, 0, 5))))},
			{Prefix: "text", Path: writeIndex(t, dir, "text.scip",
				indexOf("/y", doc("WordUtils.java", ref(referenced, 2, 0, 5))))},
		},
		Output: out,
	}); err != nil {
		t.Fatal(err)
	}

	s, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if locs := s.Definitions("text/WordUtils.java", referenced); len(locs) != 0 {
		t.Errorf("a 3.19.0 reference resolved to a 3.20.0 definition: %v", locs)
	}
}

// Rule 3. `local 0` recurs in every document of every unit. Prefixing keeps the
// documents apart, and document-scoped keying keeps the symbols apart.
func TestMerge_Superproject_LocalSymbolsDoNotCollideAcrossUnits(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "merged.scip")
	if _, err := Merge(MergeOptions{
		ProjectRoot: "/umbrella",
		Inputs: []Input{
			{Prefix: "a", Path: writeIndex(t, dir, "a.scip", indexOf("/x",
				doc("Main.java", def("local 0", 1, 4, 5), ref("local 0", 2, 4, 5))))},
			{Prefix: "b", Path: writeIndex(t, dir, "b.scip", indexOf("/y",
				doc("Main.java", def("local 0", 8, 4, 5), ref("local 0", 9, 4, 5))))},
		},
		Output: out,
	}); err != nil {
		t.Fatal(err)
	}

	s, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	assertLocations(t, s.Definitions("a/Main.java", "local 0"), []Location{
		{Path: "a/Main.java", Range: Range{1, 4, 1, 5}},
	})
	assertLocations(t, s.Definitions("b/Main.java", "local 0"), []Location{
		{Path: "b/Main.java", Range: Range{8, 4, 8, 5}},
	})
}

// Two units sharing a prefix would silently overwrite each other's documents in
// any path-keyed consumer. Merge cannot fix it, but it must count it so `clew
// index` can warn.
func TestMerge_CountsPathCollisions(t *testing.T) {
	_, stats := mergeInto(t, "/proj",
		unit{"same", indexOf("/a", doc("Main.java", def("A#", 0, 0, 1)))},
		unit{"same", indexOf("/b", doc("Main.java", def("B#", 0, 0, 1)))},
	)
	if stats.PathCollisions != 1 {
		t.Errorf("stats.PathCollisions = %d, want 1", stats.PathCollisions)
	}
}

func TestMerge_MissingInputIsAnError(t *testing.T) {
	dir := t.TempDir()
	_, err := Merge(MergeOptions{
		ProjectRoot: "/proj",
		Inputs:      []Input{{Prefix: "a", Path: filepath.Join(dir, "nope.scip")}},
		Output:      filepath.Join(dir, "out.scip"),
	})
	if err == nil {
		t.Fatal("Merge with a missing input returned no error")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error = %q, want it to name the read failure", err)
	}
}

func TestMerge_CorruptInputIsAnError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.scip")
	// Protobuf is permissive about garbage, so use a byte sequence that cannot
	// be a valid wire-format message: field number 0 is illegal.
	if err := os.WriteFile(bad, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Merge(MergeOptions{
		ProjectRoot: "/proj",
		Inputs:      []Input{{Prefix: "a", Path: bad}},
		Output:      filepath.Join(dir, "out.scip"),
	})
	if err == nil {
		t.Fatal("Merge of an unparseable input returned no error")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error = %q, want it to name the parse failure", err)
	}
}

func TestJoinPrefix(t *testing.T) {
	cases := []struct{ prefix, path, want string }{
		{"", "src/A.java", "src/A.java"},
		{"web", "src/index.ts", "web/src/index.ts"},
		{"java/svc-a", "src/A.java", "java/svc-a/src/A.java"},
	}
	for _, tc := range cases {
		if got := joinPrefix(tc.prefix, tc.path); got != tc.want {
			t.Errorf("joinPrefix(%q, %q) = %q, want %q", tc.prefix, tc.path, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
