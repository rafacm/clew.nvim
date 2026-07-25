package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tier 1. No mvn, no javac, no JDK: these cover the pure parts of the Maven
// producer. Whether a real scip-java run actually stamps coordinates is a tier 3
// claim, asserted by TestAcceptance_SingleRepository_Maven.

// javacopts.txt is what scip-java's aggregator reads to recover Maven
// coordinates. Without it every symbol degrades to `scip-java maven . . ...`,
// which is internally consistent -- so navigation inside the unit keeps working
// and the damage is invisible until units merge. This test asserts the file is
// written, and the format it is written in.
func TestWriteJavacOpts_FormatIsWhatTheAggregatorReads(t *testing.T) {
	targetroot := t.TempDir()
	unitDir := "/proj/svc"
	sources := []string{"/proj/svc/src/main/java/A.java", "/proj/svc/src/main/java/B.java"}

	if err := writeJavacOpts(targetroot, "/proj/svc/target/classes", "/cp/a.jar", unitDir, sources); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(targetroot, "javacopts.txt"))
	if err != nil {
		t.Fatalf("javacopts.txt was not written: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	if lines[0] != "-version" {
		t.Errorf("first line = %q, want %q", lines[0], "-version")
	}
	// Every line after the first is one individually double-quoted token: an
	// option, its value, or a source file.
	for i, l := range lines[1:] {
		if !strings.HasPrefix(l, `"`) || !strings.HasSuffix(l, `"`) {
			t.Errorf("line %d = %s, want a double-quoted token", i+2, l)
		}
	}

	want := []string{`"-d"`, `"-classpath"`, `"/cp/a.jar"`, `"-sourcepath"`}
	for _, w := range want {
		if !slicesContain(lines, w) {
			t.Errorf("javacopts.txt is missing line %s\ngot:\n%s", w, raw)
		}
	}
	for _, s := range sources {
		if !slicesContain(lines, `"`+s+`"`) {
			t.Errorf("javacopts.txt is missing source %s", s)
		}
	}
}

// The sourcepath entry carries a trailing path-list separator. scip-java reads
// it as a list, and dropping the separator changes how it is parsed.
func TestWriteJavacOpts_SourcepathIsAPathList(t *testing.T) {
	targetroot := t.TempDir()
	if err := writeJavacOpts(targetroot, "/classes", "/cp", "/proj/svc", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(targetroot, "javacopts.txt"))
	if err != nil {
		t.Fatal(err)
	}
	wantSrc := filepath.Join("/proj/svc", "src", "main", "java") + string(os.PathListSeparator)
	if !strings.Contains(string(raw), `"`+wantSrc+`"`) {
		t.Errorf("sourcepath entry missing or unterminated\ngot:\n%s\nwant to contain: %q", raw, wantSrc)
	}
}

// A Windows classpath contains backslashes and a classpath of any size contains
// separators. %q must survive both, or the aggregator reads a truncated list.
func TestWriteJavacOpts_QuotesPathsContainingSeparators(t *testing.T) {
	targetroot := t.TempDir()
	cp := strings.Join([]string{`/a b/x.jar`, `/c/d.jar`}, string(os.PathListSeparator))
	if err := writeJavacOpts(targetroot, "/classes", cp, "/proj", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(targetroot, "javacopts.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"`+cp+`"`) {
		t.Errorf("classpath was not written as a single quoted token\ngot:\n%s", raw)
	}
	// One classpath, one line: the separator must not have split it.
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, "x.jar") && !strings.Contains(l, "d.jar") {
			t.Errorf("classpath was split across lines: %q", l)
		}
	}
}

func TestCollectSources(t *testing.T) {
	root := tree(t,
		"src/main/java/com/example/A.java",
		"src/main/java/com/example/nested/B.java",
		"src/main/java/README.md",
		"src/main/java/com/example/C.kt",
	)
	got, err := collectSources(filepath.Join(root, "src", "main", "java"), ".java")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("collectSources returned %d files, want 2: %v", len(got), got)
	}
	for _, p := range got {
		if !strings.HasSuffix(p, ".java") {
			t.Errorf("collectSources returned a non-.java file: %s", p)
		}
		if !filepath.IsAbs(p) {
			t.Errorf("collectSources returned a relative path: %s", p)
		}
	}
}

// A missing source directory is not an error -- Index turns the empty result
// into its own diagnostic. A walk error here would mask that message.
func TestCollectSources_MissingDirectoryIsEmptyNotAnError(t *testing.T) {
	got, err := collectSources(filepath.Join(t.TempDir(), "nope"), ".java")
	if err != nil {
		t.Fatalf("collectSources on a missing directory returned %v, want no error", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d sources, want 0", len(got))
	}
}

// Gradle is detected so `clew units` reports it honestly, and fails loudly
// rather than silently omitting the unit when asked to index it.
func TestGradleProducer_IndexReportsUnimplemented(t *testing.T) {
	_, err := gradleProducer{}.Index(context.Background(), nil, Unit{Prefix: "svc", Kind: KindGradle})
	if err == nil {
		t.Fatal("gradle Index returned no error, want an unimplemented error")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %q, want it to say gradle is not implemented", err)
	}
}

func slicesContain(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
