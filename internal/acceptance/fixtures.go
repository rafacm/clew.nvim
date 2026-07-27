//go:build acceptance

package acceptance

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafacm/clew/internal/index"
	"github.com/rafacm/clew/internal/indexer"
)

// A Project is one upstream repository, pinned to a commit.
//
// The pin is a COMMIT SHA, never a tag name. Apache's release tags are
// annotated, so the tag object and the commit have different hashes and a
// tarball URL built from the tag object is simply broken. Ref is carried for
// error messages and for whoever has to update the pin.
type Project struct {
	Repo string // "apache/commons-lang"
	Ref  string // the human-readable ref the SHA was resolved from
	SHA  string
}

// Name is the last path element of the repository, used as the directory name
// when a project is checked out.
func (p Project) Name() string {
	_, name, _ := strings.Cut(p.Repo, "/")
	return name
}

// The pinned fixtures. Release tags where the project publishes meaningful
// ones, default-branch commits where it does not.
//
// commonsLang and commonsText are COUPLED: rel/commons-text-1.15.0 declares
// commons.lang3.version as 3.20.0, which is why commons-lang is pinned to
// exactly that release. Their symbol strings embed the version, so moving one
// without the other silently severs Superproject_JavaCrossSubmodule and the
// failure looks like a clew bug rather than a fixture bug.
var (
	commonsLang = Project{"apache/commons-lang", "rel/commons-lang-3.20.0", "598dfc163b8b410fb3bb8794521206ec8dcec82a"}
	commonsText = Project{"apache/commons-text", "rel/commons-text-1.15.0", "04e937470d3679cc163df85d82d5b6d2e3e71128"}
	commonsMath = Project{"apache/commons-math", "master", "912fd9c4ebc56a78293deb703443fe0f5d5f8f89"}
	petclinic   = Project{"spring-projects/spring-petclinic", "main", "f182358d02e4a68e52bdbabf55ca7800288511e7"}
	// NOTE: ADR 1 records this pin's ref as v11.1.15, but the tree at this SHA
	// carries "version": "10.0.3-beta". The Ref label and the commit disagree
	// and the pin needs re-resolving; the SHA is kept as recorded rather than
	// quietly changed, because the ADR owns the table.
	immer       = Project{"immerjs/immer", "v11.1.15", "a3be9df762c1dbe9959a011ddbab0ce838cbc468"}
	zod         = Project{"colinhacks/zod", "v4.4.3", "1fb56a5c18c27102dbc92260a4007c7732a0ccca"}
	flask       = Project{"pallets/flask", "3.1.3", "22d924701a6ae2e4cd01e9a15bbaf3946094af65"}
	angularReal = Project{"gothinkster/angular-realworld-example-app", "main", "dd99ed2cf39c805d719f943c5d7061a5683d98a8"}
)

// ------------------------------------------------------------------ downloads

// cacheRoot is where downloaded projects live between runs. It is deliberately
// outside t.TempDir(): re-downloading commons-lang on every local run is the
// difference between a suite people run and one they do not.
func cacheRoot(t *testing.T) string {
	t.Helper()
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("no cache directory available: %v", err)
		}
		base = dir
	}
	return filepath.Join(base, "clew-test")
}

// fetch returns a path to the project's source, downloading it on first use.
//
// The tree is keyed by SHA and never mutated: callers get a copy. A tarball is
// used rather than a clone because it is one request, no history, and no git
// dependency.
func fetch(t *testing.T, p Project) string {
	t.Helper()

	dir := filepath.Join(cacheRoot(t), p.SHA)
	done := filepath.Join(dir, ".clew-test-complete")
	if _, err := os.Stat(done); err == nil {
		return filepath.Join(dir, "src")
	}

	// A previous run may have died mid-extract. Anything without the marker is
	// not trustworthy.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	// Extract into a sibling and rename, so a concurrent or interrupted run
	// never leaves a half-tree behind the completion marker.
	staging, err := os.MkdirTemp(cacheRoot(t), "staging-")
	if err != nil {
		if err := os.MkdirAll(cacheRoot(t), 0o755); err != nil {
			t.Fatal(err)
		}
		if staging, err = os.MkdirTemp(cacheRoot(t), "staging-"); err != nil {
			t.Fatal(err)
		}
	}
	defer os.RemoveAll(staging)

	url := fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", p.Repo, p.SHA)
	t.Logf("downloading %s @ %s (%s)", p.Repo, p.Ref, p.SHA[:12])

	started := time.Now()
	if err := downloadAndExtract(url, filepath.Join(staging, "src")); err != nil {
		t.Fatalf("fetching %s @ %s: %v", p.Repo, p.Ref, err)
	}
	t.Logf("  downloaded %s in %.1fs", p.Repo, time.Since(started).Seconds())

	if err := os.WriteFile(filepath.Join(staging, ".clew-test-complete"), []byte(p.SHA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staging, dir); err != nil {
		// Another test in the same run won the race; its tree is equivalent.
		if _, statErr := os.Stat(done); statErr != nil {
			t.Fatalf("publishing %s: %v", p.Repo, err)
		}
	}
	return filepath.Join(dir, "src")
}

// downloadAndExtract streams a GitHub tarball into dest, stripping the
// `<repo>-<sha>/` directory GitHub wraps everything in.
func downloadAndExtract(url, dest string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		// Strip the wrapper directory.
		_, rel, ok := strings.Cut(filepath.ToSlash(hdr.Name), "/")
		if !ok || rel == "" {
			continue
		}
		// Refuse anything that would escape dest. Upstream tarballs are
		// trusted, but a path-traversal check costs nothing.
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry escapes the destination: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// ---------------------------------------------------------------- workspaces

// checkout copies a fetched project into a fresh directory.
//
// Indexing writes into the tree it indexes -- target/, node_modules/, .clew/ --
// so the cached download is never handed to the indexer directly. The cache
// holds pristine sources; every run gets its own working copy.
func checkout(t *testing.T, p Project, dest string) string {
	t.Helper()
	if err := copyTree(fetch(t, p), dest); err != nil {
		t.Fatalf("checking out %s: %v", p.Repo, err)
	}
	return dest
}

// tempRoot is t.TempDir() with every symlink resolved.
//
// On macOS t.TempDir() hands back a path under /var, which is a symlink to
// /private/var. clew now resolves its own root -- indexer.resolveRoot -- so an
// unresolved tmpdir no longer breaks indexing, but it does make every fixture
// tree reachable by two spellings, which is not what any test here means to
// measure. TestAcceptance_SingleRepository_MavenViaSymlink depends on it
// outright: it asserts something about a symlink, so the ONLY symlink in its
// path must be the one it created.
func tempRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// singleRepository returns a working copy of one project as the project root.
func singleRepository(t *testing.T, p Project) string {
	t.Helper()
	return checkout(t, p, filepath.Join(tempRoot(t), p.Name()))
}

// superproject composes several projects under one root, mirroring a
// git-submodule umbrella.
//
// Nothing is committed and no submodules are involved: only the ARRANGEMENT is
// synthetic, and the arrangement is the thing under test. clew discovers units
// by build file, not by git boundary, so a composed tree exercises exactly the
// same code path as a real umbrella.
func superproject(t *testing.T, layout map[string]Project) string {
	t.Helper()
	root := tempRoot(t)
	for prefix, p := range layout {
		checkout(t, p, filepath.Join(root, filepath.FromSlash(prefix)))
	}
	// A marker so the tree looks like the umbrella it is standing in for.
	if err := os.WriteFile(filepath.Join(root, ".gitmodules"), []byte("# composed by clew's acceptance suite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// syntheticProject writes a project from literal file contents and returns its
// root.
//
// Downloading a real repository is the default here, and for good reason: a
// fixture nobody wrote is a fixture nobody can accidentally shape to pass. This
// is for the case where the thing under test is a package manager's BEHAVIOUR
// rather than any repository's code -- SingleRepository_YarnPnP is the only one
// so far -- where a real project would cost a large download to say what a
// handful of written lines say exactly. Whatever version matters must still be pinned in the
// content written here.
func syntheticProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := tempRoot(t)
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// yarnBerryVersion is the yarn major this suite needs a PnP project from. Yarn
// 2+ is a different program from the `yarn` on $PATH: npm's `yarn` package stops
// at 2.4.3, and berry is distributed through corepack, which reads the version
// out of the fixture's own `packageManager` field.
const yarnBerryVersion = "4.5.0"

// yarnBerryOnPath makes `yarn` resolve to the berry release the fixture declares,
// for this test process and everything it spawns.
//
// A shim rather than an install: `npm install --global yarn` gives classic 1.x,
// which every other yarn fixture here needs, and replacing it would break them.
// The shim defers to corepack, which reads `packageManager` from the project it
// is run in -- so the version pin lives in the fixture, where a reader looking at
// the project can see it.
//
// t.Setenv is process-wide, so any test calling this cannot be parallel.
func yarnBerryOnPath(t *testing.T) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "shim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "yarn")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec corepack yarn \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Non-interactive: corepack otherwise asks before downloading a manager it
	// has not seen, and a prompt in CI is a hang, not a question.
	t.Setenv("COREPACK_ENABLE_DOWNLOAD_PROMPT", "0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// readAll reads several files under root, keyed by their name, so a test can
// compare a tree before and after indexing in one comparison.
func readAll(t *testing.T, root string, names ...string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(names))
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	return out
}

// runIn runs a command in dir and fails the test with its output if it errors.
// For setting a fixture up -- clew's own process execution is runner.run.
func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	})
}

// -------------------------------------------------------------------- driving

// requireTools skips the test unless every named executable is on $PATH.
//
// A skip, not a failure: a missing JDK on a laptop is not a clew regression.
//
// In CI that reasoning inverts. A tier 3 job whose toolchain setup silently
// broke would skip every test and report a green tick having verified nothing,
// which is strictly worse than a red one -- the whole suite would evaporate and
// the only trace would be a line in a log nobody reads. Setting
// CLEW_TEST_REQUIRE_TOOLS turns every such skip into a failure, and
// acceptance.yml sets it.
//
// TestAcceptance_SingleRepository_Python is a plain t.Skip and is unaffected:
// it is waiting on a producer, not on a toolchain.
func requireTools(t *testing.T, names ...string) {
	t.Helper()
	strict := os.Getenv("CLEW_TEST_REQUIRE_TOOLS") != ""
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			if strict {
				t.Fatalf("%s is not on $PATH, and CLEW_TEST_REQUIRE_TOOLS is set: "+
					"tier 3 needs it, so this is a broken CI configuration rather than "+
					"a test to skip", n)
			}
			t.Skipf("%s is not on $PATH; tier 3 needs it", n)
		}
	}
}

// buildIndex runs the real pipeline over root and returns the loaded index.
func buildIndex(t *testing.T, root string) (*index.Store, time.Duration) {
	t.Helper()

	started := time.Now()
	err := indexer.Run(context.Background(), indexer.Options{
		Root: root,
		Log:  testLogger{t},
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("clew index: %v", err)
	}

	store, err := index.Load(filepath.Join(root, ".clew", "index.scip"))
	if err != nil {
		t.Fatalf("loading the index clew just wrote: %v", err)
	}
	return store, elapsed
}

type testLogger struct{ t *testing.T }

func (l testLogger) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// units is a convenience wrapper reporting what discovery found, as
// "prefix:kind" pairs.
func units(t *testing.T, root string) []string {
	t.Helper()
	us, err := indexer.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Prefix + ":" + string(u.Kind)
	}
	return out
}

// findFiles returns every path under root whose name ends in suffix, relative
// to root and slash-separated -- the form a Document.relative_path takes.
func findFiles(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (name == "node_modules" || name == ".git" || name == "target" || name == ".clew") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), suffix) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ----------------------------------------------------------------- assertions

// Assertions are on PROPERTIES, never on bytes. A golden-file diff against a
// known-good index is too brittle: SCIP output moves with indexer versions, and
// ScipTypeScriptPackage is pinned to @latest, so the producer moves underneath
// any baseline. The claims below survive an indexer upgrade.

// mavenCoordinate matches the coordinate a correctly-stamped scip-java symbol
// carries: `maven/<group>/<artifact> <version>`. The degraded form produced
// without javacopts.txt is `maven . . `, which this does not match.
func hasMavenCoordinate(symbol string) bool {
	fields := strings.Fields(symbol)
	// scip-java <manager> <package> <version> <descriptor>
	if len(fields) < 5 || fields[1] != "maven" {
		return false
	}
	pkg, version := fields[2], fields[3]
	if pkg == "." || version == "." {
		return false
	}
	return strings.HasPrefix(pkg, "maven/") && strings.Count(pkg, "/") == 2
}

// occurrenceSymbols returns every symbol occurring in one document.
//
// SearchSymbols does not answer this question: a symbol a document merely
// REFERENCES, defined in a dependency, has no SymbolInformation of its own here.
// That is precisely the symbol an unresolved import loses, so it is the one an
// import has to be checked by.
func occurrenceSymbols(t *testing.T, store *index.Store, path string) []string {
	t.Helper()
	doc, ok := store.Document(path)
	if !ok {
		t.Fatalf("the index holds no document at %q", path)
	}
	out := make([]string, 0, len(doc.Occurrences))
	for _, occ := range doc.Occurrences {
		out = append(out, occ.Symbol)
	}
	return out
}

// findSymbol returns the first global symbol satisfying pred, or fails.
func findSymbol(t *testing.T, store *index.Store, what string, pred func(string) bool) string {
	t.Helper()
	for _, si := range store.SearchSymbols("", 0) {
		if pred(si.Symbol) {
			return si.Symbol
		}
	}
	t.Fatalf("no symbol found matching %s", what)
	return ""
}

// assertResolves checks that a symbol has a definition and that the definition
// lives under the expected unit prefix.
func assertResolves(t *testing.T, store *index.Store, symbol, wantPrefix string) {
	t.Helper()
	defs := store.Definitions("", symbol)
	if len(defs) == 0 {
		t.Fatalf("symbol %q has no definition", symbol)
	}
	if wantPrefix != "" && !strings.HasPrefix(defs[0].Path, wantPrefix+"/") {
		t.Errorf("symbol %q is defined at %q, want it under %q/", symbol, defs[0].Path, wantPrefix)
	}
}
