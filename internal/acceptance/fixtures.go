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
// This is NOT cosmetic. On macOS t.TempDir() hands back a path under /var,
// which is a symlink to /private/var, and scip-java bounds its search for the
// unit's pom.xml by a realpath'd sourceroot. Given an unresolved path the
// search escapes the bound, no pom is found, and every symbol silently
// degrades to `scip-java maven . . ` -- so a suite run on an unresolved
// tmpdir measures the symlink, not clew.
//
// The degradation itself is a real clew defect, not a test artifact; see
// TestAcceptance_SingleRepository_MavenViaSymlink.
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
// CI's scheduled tier 3 job installs the toolchains and a skip there means the
// job is misconfigured, which the log makes obvious.
func requireTools(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
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
