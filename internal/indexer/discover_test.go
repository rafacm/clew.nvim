package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

// Tier 1. Hermetic: every tree is built in t.TempDir() and no indexer, network
// or toolchain is involved. See doc/adr/0001-testing-strategy.md.
//
// Tests are named for the project layout they exercise -- SingleRepository,
// Superproject, Monorepo -- so a gap in coverage is visible from the test list.

// tree builds a directory tree under t.TempDir() from a set of file paths and
// returns the root. A path ending in "/" is a directory; anything else is a file
// with placeholder content, since discovery only ever stats.
func tree(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if p[len(p)-1] == '/' {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// discovered reduces units to "prefix:kind" pairs, which is what the assertions
// in this file actually care about.
func discovered(t *testing.T, root string) []string {
	t.Helper()
	units, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Prefix + ":" + string(u.Kind)
	}
	return out
}

func assertUnits(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("units = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("units = %v, want %v", got, want)
		}
	}
}

func TestDiscover_SingleRepository_Maven(t *testing.T) {
	root := tree(t, "pom.xml", "src/main/java/com/example/Foo.java")
	assertUnits(t, discovered(t, root), []string{":maven"})
}

func TestDiscover_SingleRepository_Gradle(t *testing.T) {
	root := tree(t, "build.gradle", "src/main/java/com/example/Foo.java")
	assertUnits(t, discovered(t, root), []string{":gradle"})
}

func TestDiscover_SingleRepository_GradleKotlinDSL(t *testing.T) {
	root := tree(t, "build.gradle.kts", "src/main/kotlin/Foo.kt")
	assertUnits(t, discovered(t, root), []string{":gradle"})
}

func TestDiscover_SingleRepository_TypeScript(t *testing.T) {
	root := tree(t, "package.json", "tsconfig.json", "src/index.ts")
	assertUnits(t, discovered(t, root), []string{":typescript"})
}

func TestDiscover_SingleRepository_Angular(t *testing.T) {
	root := tree(t, "package.json", "angular.json", "src/app/app.component.ts")
	assertUnits(t, discovered(t, root), []string{":typescript"})
}

// A package.json with no tsconfig and no angular.json is not a unit. Plenty of
// repositories carry one purely for a linter or a git hook.
func TestDiscover_SingleRepository_BarePackageJSONIsNotAUnit(t *testing.T) {
	root := tree(t, "package.json", "index.js")
	assertUnits(t, discovered(t, root), nil)
}

func TestDiscover_NoBuildFileYieldsNoUnits(t *testing.T) {
	root := tree(t, "README.md", "docs/guide.md")
	assertUnits(t, discovered(t, root), nil)
}

// Producer precedence is the order of the `producers` slice, and TypeScript
// deliberately precedes the JVM producers: an Angular app may sit beside a
// pom.xml, and package.json plus a tsconfig is the more specific signal.
func TestDiscover_ProducerPrecedence_TypeScriptBeatsMaven(t *testing.T) {
	root := tree(t, "pom.xml", "package.json", "tsconfig.json")
	assertUnits(t, discovered(t, root), []string{":typescript"})
}

// spring-petclinic carries both a pom.xml and a build.gradle, so this ordering
// decides which producer indexes it.
func TestDiscover_ProducerPrecedence_MavenBeatsGradle(t *testing.T) {
	root := tree(t, "pom.xml", "build.gradle")
	assertUnits(t, discovered(t, root), []string{":maven"})
}

func TestDiscover_Superproject(t *testing.T) {
	root := tree(t,
		".gitmodules",
		"java/svc-a/pom.xml", "java/svc-a/src/main/java/A.java",
		"java/svc-b/pom.xml", "java/svc-b/src/main/java/B.java",
		"web/package.json", "web/tsconfig.json", "web/src/index.ts",
	)
	assertUnits(t, discovered(t, root), []string{
		"java/svc-a:maven",
		"java/svc-b:maven",
		"web:typescript",
	})
}

// Units are discovered by build file, not by git boundary, so a monorepo with
// no submodules takes exactly the same path as a superproject.
func TestDiscover_Monorepo_MixedLanguages(t *testing.T) {
	root := tree(t,
		"backend/pom.xml", "backend/src/main/java/A.java",
		"frontend/package.json", "frontend/tsconfig.json",
	)
	assertUnits(t, discovered(t, root), []string{
		"backend:maven",
		"frontend:typescript",
	})
}

// zod's shape: a root package.json + tsconfig + pnpm-workspace.yaml. clew
// classifies the root as one unit and never descends into packages/. Pinned
// here so that changing it is a deliberate act, not a side effect.
func TestDiscover_Monorepo_PnpmWorkspace(t *testing.T) {
	root := tree(t,
		"package.json", "tsconfig.json", "pnpm-workspace.yaml",
		"packages/core/package.json", "packages/core/tsconfig.json",
		"packages/cli/package.json", "packages/cli/tsconfig.json",
	)
	assertUnits(t, discovered(t, root), []string{":typescript"})
}

// An aggregator pom claims the root and the modules are never seen. This is the
// discovery half of the multi-module Maven gap recorded in ADR 1: indexMaven
// then fails because an aggregator has no src/main/java of its own.
func TestDiscover_Monorepo_MultiModuleMaven_DoesNotDescend(t *testing.T) {
	root := tree(t,
		"pom.xml",
		"commons-math-core/pom.xml", "commons-math-core/src/main/java/A.java",
		"commons-math-legacy/pom.xml", "commons-math-legacy/src/main/java/B.java",
	)
	assertUnits(t, discovered(t, root), []string{":maven"})
}

func TestDiscover_SkipsVendoredAndBuildDirectories(t *testing.T) {
	// Every one of these holds a build file that would otherwise classify.
	root := tree(t,
		"pom.xml",
		"node_modules/dep/package.json", "node_modules/dep/tsconfig.json",
		"vendor/lib/pom.xml",
		"third_party/lib/pom.xml",
		"sub/target/generated/pom.xml",
		"sub/build/pom.xml",
		"sub/dist/package.json", "sub/dist/tsconfig.json",
		"sub/out/pom.xml",
		".gradle/cache/pom.xml",
		".clew/tools/pom.xml",
		".git/modules/x/pom.xml",
	)
	assertUnits(t, discovered(t, root), []string{":maven"})
}

// The skip list is matched on directory name, so a root literally named
// `build` or `dist` must still be walked -- otherwise `clew index` in such a
// directory silently finds nothing.
func TestDiscover_SkipListDoesNotApplyToTheRootItself(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "build")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertUnits(t, discovered(t, root), []string{":maven"})
}

func TestDiscover_UnitsAreSortedByPrefix(t *testing.T) {
	root := tree(t,
		"zeta/pom.xml",
		"alpha/pom.xml",
		"middle/pom.xml",
	)
	assertUnits(t, discovered(t, root), []string{
		"alpha:maven", "middle:maven", "zeta:maven",
	})
}

func TestDiscover_RecordsBuildFileAndAbsoluteDir(t *testing.T) {
	root := tree(t, "web/package.json", "web/tsconfig.json")
	units, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("got %d units, want 1", len(units))
	}
	u := units[0]
	if u.BuildFile != "package.json" {
		t.Errorf("BuildFile = %q, want %q", u.BuildFile, "package.json")
	}
	if !filepath.IsAbs(u.Dir) {
		t.Errorf("Dir = %q, want an absolute path", u.Dir)
	}
	if got := filepath.Base(u.Dir); got != "web" {
		t.Errorf("Dir base = %q, want %q", got, "web")
	}
}

// Prefix becomes the prefix of every relative_path in the merged index, so it
// must be slash-separated on every platform.
func TestDiscover_PrefixIsSlashSeparated(t *testing.T) {
	root := tree(t, "java/nested/svc/pom.xml")
	assertUnits(t, discovered(t, root), []string{"java/nested/svc:maven"})
}

func TestDiscover_RelativeRootIsResolved(t *testing.T) {
	root := tree(t, "pom.xml")
	t.Chdir(root)
	assertUnits(t, discovered(t, "."), []string{":maven"})
}

// A symlink in the root path must not survive into Unit.Dir. scip-java bounds
// its search for the unit's pom.xml by a realpath'd sourceroot, so an
// unresolved Dir degrades every Maven coordinate the unit defines -- and does
// it silently, since navigation inside the unit keeps working. See resolveRoot.
func TestDiscover_SymlinkInTheRootPathIsResolved(t *testing.T) {
	base := realTempDir(t)
	svc := filepath.Join(base, "workspace", "svc")
	if err := os.MkdirAll(svc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "pom.xml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "workspace"), filepath.Join(base, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	units, err := Discover(filepath.Join(base, "link", "svc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("got %d units, want 1", len(units))
	}
	if units[0].Dir != svc {
		t.Errorf("Dir = %q, want the resolved %q", units[0].Dir, svc)
	}
}

// --root pointed directly at a symlink. filepath.WalkDir does not follow one,
// so without resolution this finds nothing at all rather than degrading.
func TestDiscover_RootItselfIsASymlink(t *testing.T) {
	base := realTempDir(t)
	svc := filepath.Join(base, "svc")
	if err := os.MkdirAll(svc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "pom.xml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(svc, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	assertUnits(t, discovered(t, link), []string{":maven"})
}

// realTempDir is t.TempDir() with its own symlinks resolved, so that a test
// about symlinks measures the one it created rather than macOS's /var.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiscover_MissingRootIsNotFatal(t *testing.T) {
	units, err := Discover(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Discover on a missing root returned %v, want no error", err)
	}
	if len(units) != 0 {
		t.Fatalf("got %d units, want 0", len(units))
	}
}

// Every Kind must have a registered Producer. Discovery stamps a Kind and Run
// dispatches by looking it up, so adding one without the other produces a unit
// that discovers cleanly and fails at index time.
func TestEveryKindHasARegisteredProducer(t *testing.T) {
	for _, k := range []Kind{KindMaven, KindGradle, KindTypeScript} {
		if _, ok := producerFor(k); !ok {
			t.Errorf("no producer registered for kind %q", k)
		}
	}
}

func TestEveryProducerHasAUniqueKind(t *testing.T) {
	seen := map[Kind]bool{}
	for _, p := range producers {
		if seen[p.Kind()] {
			t.Errorf("kind %q is claimed by more than one producer", p.Kind())
		}
		seen[p.Kind()] = true
	}
}

func TestFilter(t *testing.T) {
	units := []Unit{
		{Prefix: ""},
		{Prefix: "java/svc-a"},
		{Prefix: "java/svc-b"},
		{Prefix: "web"},
	}

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"empty returns everything", "", []string{"", "java/svc-a", "java/svc-b", "web"}},
		{"exact prefix", "java/svc-a", []string{"java/svc-a"}},
		{"trailing slash is trimmed", "java/svc-a/", []string{"java/svc-a"}},
		{"bare leaf name", "svc-b", []string{"java/svc-b"}},
		{"top-level leaf", "web", []string{"web"}},
		{"no match", "nope", nil},
		{"partial segment does not match", "svc", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Filter(units, tc.query)
			if len(got) != len(tc.want) {
				t.Fatalf("Filter(%q) returned %d units, want %d", tc.query, len(got), len(tc.want))
			}
			for i := range got {
				if got[i].Prefix != tc.want[i] {
					t.Fatalf("Filter(%q) = %v, want %v", tc.query, got, tc.want)
				}
			}
		})
	}
}
