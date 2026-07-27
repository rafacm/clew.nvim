//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rafacm/clew/internal/index"
	"github.com/rafacm/clew/internal/indexer"
)

// Tier 3. Network and toolchains required, excluded from `go test ./...` by the
// build tag above. Run with `make test-acceptance`.
//
// Tests are named for the project layout they exercise, so a gap in coverage is
// visible from the test list alone.
//
// WHICH TESTS RUN IN PARALLEL, AND WHY FOUR DO NOT
//
// Most tests here call t.Parallel(). Four deliberately do not, and marking three
// of them parallel would trade a corrupt cache for a few seconds.
//
// The rule they enforce: NO TWO CONCURRENT TESTS MAY DOWNLOAD THE SAME ARTIFACT.
// A Maven local repository is not safe for concurrent writes of one artifact
// across processes, and a package manager's content-addressed cache is no better;
// two processes fetching the same jar is the classic way to get a truncated file
// and a job that fails for reasons no pull request caused. That is fatal here in
// particular: this suite is advisory by design, so a flake teaches people to
// ignore it, and then it protects nothing.
//
// Go runs every sequential test to completion BEFORE resuming the parallel ones,
// which turns an unmarked test into a barrier. Three of them warm everything the
// parallel batch shares:
//
//   - SingleRepository_Maven downloads spring-petclinic's dependencies, the
//     scip-javac and scip-java classpaths, and the Maven plugins every Maven unit
//     resolves. MavenViaSymlink then reads petclinic's from a warm ~/.m2.
//   - SingleRepository_MavenLarge downloads commons-lang's, which
//     Superproject_JavaCrossSubmodule and Superproject_JavaAndAngular both
//     resolve again.
//   - SingleRepository_Angular fills bun's cache for angular-realworld, which
//     Superproject_JavaAndAngular installs again.
//
// The fourth, SingleRepository_YarnPnP, is sequential for a different reason and
// warms nothing: it puts a pinned yarn berry first on $PATH with t.Setenv, which
// is process-wide, and Go forbids that in a parallel test for exactly that
// reason. Everything it downloads -- yarn 4 through corepack, date-fns -- is
// wanted by no other test here.
//
// What is left in the parallel batch touches only artifacts nothing else in it
// wants: commons-text, commons-math, immer's yarn graph, zod's tarball. Each
// fetches its fixture into its own t.TempDir(), and concurrent downloads of the
// tarball cache are already handled by fetch's stage-and-rename.
//
// Adding a fixture means checking it against that rule. If it shares a dependency
// graph with something already in the batch, either it warms it sequentially or
// it joins the test that owns it. `go test` caps concurrency at GOMAXPROCS, so
// the batch never fans out wider than the runner has cores.

// ------------------------------------------------------- SingleRepository_Maven

// spring-petclinic is the pipeline's original validation target. It carries
// both a pom.xml and a build.gradle, so it also covers producer precedence.
func TestAcceptance_SingleRepository_Maven(t *testing.T) {
	// Not parallel: this is the barrier that warms ~/.m2 with the scip-javac and
	// scip-java classpaths, the Maven plugins and petclinic's dependencies. See
	// the note at the top.
	requireTools(t, "mvn", "javac", "java")

	root := singleRepository(t, petclinic)

	t.Run("Discovery", func(t *testing.T) {
		if got := units(t, root); len(got) != 1 || got[0] != ":maven" {
			t.Fatalf("units = %v, want the root classified as a single maven unit", got)
		}
	})

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed spring-petclinic in %.1fs", elapsed.Seconds())

	// This is the subtest doc/adr/0001-testing-strategy.md and
	// internal/indexer/java.go refer to. Without javacopts.txt every symbol
	// degrades to `scip-java maven . . ...`, which is internally consistent --
	// navigation inside the unit keeps working -- so nothing but this assertion
	// catches it before indexes merge.
	t.Run("SymbolsCarryCoordinates", func(t *testing.T) {
		var withCoords, degraded int
		var example string
		for _, si := range store.SearchSymbols("org/springframework/samples/petclinic", 0) {
			if hasMavenCoordinate(si.Symbol) {
				withCoords++
				if example == "" {
					example = si.Symbol
				}
			} else if strings.Contains(si.Symbol, "maven . . ") {
				degraded++
			}
		}
		if degraded > 0 {
			t.Errorf("%d symbols carry the degraded `maven . . ` coordinate; javacopts.txt was not honoured", degraded)
		}
		if withCoords == 0 {
			t.Fatal("no petclinic symbol carries a maven/<group>/<artifact> <version> coordinate")
		}
		t.Logf("%d symbols carry real coordinates, e.g. %s", withCoords, example)
	})

	t.Run("DefinitionsResolve", func(t *testing.T) {
		sym := findSymbol(t, store, "a petclinic class", func(s string) bool {
			return hasMavenCoordinate(s) &&
				strings.Contains(s, "org/springframework/samples/petclinic/owner/Owner#")
		})
		assertResolves(t, store, sym, "")
	})

	t.Run("DocumentPathsAreRelativeAndSlashSeparated", func(t *testing.T) {
		docs, _, _ := store.Stats()
		if docs == 0 {
			t.Fatal("the index has no documents")
		}
		if _, ok := store.Document("src/main/java/org/springframework/samples/petclinic/owner/Owner.java"); !ok {
			t.Error("Owner.java is not present at its expected relative path")
		}
	})
}

// ------------------------------------------- SingleRepository_MavenViaSymlink

// A project reached through a symlink must index exactly like one that is not.
//
// It did not, until indexer.resolveRoot. scip-java bounds its search for the
// unit's pom.xml by a realpath'd sourceroot; clew used to hand it the spelling
// the user typed, so for any project behind a symlink -- a symlinked workspace,
// everything under /tmp on macOS -- the search escaped the bound, no pom was
// found, and EVERY symbol degraded to the anonymous `scip-java maven . . `
// coordinate.
//
// That failure was invisible by construction: the degraded form is internally
// consistent, so navigation inside the unit kept working and nothing surfaced
// until units merged and collapsed into the same anonymous package. Nothing
// short of this assertion catches a regression, which is why it is on the
// coordinates rather than on the resolved path -- tier 1's
// TestDiscover_SymlinkInTheRootPathIsResolved covers the mechanism. See #2.
func TestAcceptance_SingleRepository_MavenViaSymlink(t *testing.T) {
	t.Parallel()
	requireTools(t, "mvn", "javac", "java")

	base := tempRoot(t)
	checkout(t, petclinic, filepath.Join(base, "workspace", petclinic.Name()))
	if err := os.Symlink(filepath.Join(base, "workspace"), filepath.Join(base, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	// An INTERMEDIATE symlink is the shape a symlinked workspace takes; --root
	// pointed directly at one is the second half of the same defect, since
	// filepath.WalkDir does not follow a symlink either.
	t.Run("Discovery", func(t *testing.T) {
		got := units(t, filepath.Join(base, "link", petclinic.Name()))
		if len(got) != 1 || got[0] != ":maven" {
			t.Fatalf("units through an intermediate symlink = %v, want [:maven]", got)
		}
		link := filepath.Join(base, "link-to-project")
		if err := os.Symlink(filepath.Join(base, "workspace", petclinic.Name()), link); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		if got := units(t, link); len(got) != 1 || got[0] != ":maven" {
			t.Fatalf("units with --root at a symlink = %v, want [:maven]", got)
		}
	})

	store, _ := buildIndex(t, filepath.Join(base, "link", petclinic.Name()))

	var degraded, withCoords int
	for _, si := range store.SearchSymbols("petclinic", 0) {
		if hasMavenCoordinate(si.Symbol) {
			withCoords++
		} else if strings.Contains(si.Symbol, "maven . . ") {
			degraded++
		}
	}

	if degraded > 0 {
		t.Errorf("%d symbols degraded to the anonymous `maven . . ` coordinate; "+
			"the symlinked project path is reaching scip-java unresolved again", degraded)
	}
	if withCoords == 0 {
		t.Fatal("no petclinic symbol carries a maven/<group>/<artifact> <version> coordinate")
	}
	t.Logf("%d symbols carry real coordinates through a symlinked path", withCoords)
}

// -------------------------------------------------- SingleRepository_MavenLarge

// commons-lang is a single module with a real src/main/java, and the
// indexing-time measurement.
func TestAcceptance_SingleRepository_MavenLarge(t *testing.T) {
	// Not parallel: this is the barrier that warms commons-lang's dependencies
	// for JavaCrossSubmodule and JavaAndAngular. See the note at the top.
	requireTools(t, "mvn", "javac", "java")

	root := singleRepository(t, commonsLang)
	store, elapsed := buildIndex(t, root)

	docs, occs, syms := store.Stats()
	t.Logf("indexed commons-lang in %.1fs -- %d documents, %d occurrences, %d symbols",
		elapsed.Seconds(), docs, occs, syms)

	sym := findSymbol(t, store, "org/apache/commons/lang3/StringUtils#", func(s string) bool {
		return strings.Contains(s, "org/apache/commons/lang3/StringUtils#") && hasMavenCoordinate(s)
	})
	if !strings.Contains(sym, "maven/org.apache.commons/commons-lang3 3.20.0") {
		t.Errorf("symbol %q does not carry the 3.20.0 coordinate; the pin and the pom disagree", sym)
	}
	assertResolves(t, store, sym, "")
}

// -------------------------------------------------- SingleRepository_TypeScript

// immer: package.json + tsconfig.json, no workspace file.
//
// It is here because it is YARN-managed. `npm install` on this tree fails
// outright -- @vitest/coverage-v8 pins a peer vitest npm's resolver will not
// reconcile -- so before package-manager detection existed the unit produced no
// index at all. That is issue #3, and this is its regression test: the claim is
// not merely that immer indexes, but that it indexes through the manager the
// project actually declares.
//
// The TypeScript pipeline itself is proven by SingleRepository_Angular too;
// what is exercised here and nowhere else is the yarn branch of planInstall.
func TestAcceptance_SingleRepository_TypeScript(t *testing.T) {
	t.Parallel()
	requireTools(t, "node", "npm", "npx", "yarn")

	root := singleRepository(t, immer)

	if got := units(t, root); len(got) != 1 || got[0] != ":typescript" {
		t.Fatalf("units = %v, want the root classified as a single typescript unit", got)
	}

	// The fixture's premise. A package-lock.json appearing upstream would make
	// this pass through npm and quietly stop testing anything.
	lock := filepath.Join(root, "yarn.lock")
	before, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("immer no longer ships a yarn.lock; this fixture's premise has changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "package-lock.json")); err == nil {
		t.Fatal("immer now ships a package-lock.json as well; this fixture no longer proves yarn detection")
	}

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed immer in %.1fs", elapsed.Seconds())

	// A definition in immer's OWN source: the claim is that the yarn-installed
	// tree was indexed, not merely that scip-typescript ran. No identifier is
	// pinned -- immer's tsconfig names four entry files and reorganises them
	// between releases, so `Immer#` itself is not in the index at this pin.
	sym := findSymbol(t, store, "a definition under immer's src/", func(s string) bool {
		defs := store.Definitions("", s)
		return len(defs) > 0 && strings.HasPrefix(defs[0].Path, "src/")
	})
	assertResolves(t, store, sym, "")

	// `yarn install --frozen-lockfile`, not `yarn install`. Every producer is
	// bound by "no build file is ever modified", and a lockfile is a build file:
	// clew must not silently re-resolve a tree the project has pinned.
	t.Run("TheLockfileIsNotRewritten", func(t *testing.T) {
		after, err := os.ReadFile(lock)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Errorf("yarn.lock changed during indexing (%d bytes before, %d after); "+
				"the install is not frozen", len(before), len(after))
		}
	})
}

// ------------------------------------------------------ SingleRepository_YarnPnP

// A Yarn Plug'n'Play project: yarn 2+'s DEFAULT install mode, and one that
// creates no node_modules at all -- dependencies stay zipped under .yarn/cache
// and resolve through a generated .pnp.cjs.
//
// This is issue #8, and the claim it exists to make is about a DEPENDENCY's
// symbol. scip-typescript resolves imports through ordinary Node resolution, so
// against a PnP tree it finds nothing and says nothing: exit 0, and an index that
// looks complete while every external symbol is missing. Every other TypeScript
// test here would have passed on that index, because they all assert on the
// project's OWN source, which resolves with no install at all. That blind spot is
// not hypothetical -- SingleRepository_Angular was green for months while its
// dependencies were installed by the wrong package manager entirely (issue #3).
//
// The fixture is written rather than downloaded, which is the exception to how
// every other fixture here works. What is under test is a linker, not any
// repository's code, and yarnpkg/berry would cost a large download to say what
// these four files say. Both versions that matter are still pinned: yarn by the
// `packageManager` field corepack reads, date-fns by an exact dependency whose
// version is part of the symbol asserted below.
func TestAcceptance_SingleRepository_YarnPnP(t *testing.T) {
	// Not parallel, for two reasons. yarnBerryOnPath uses t.Setenv, which Go
	// forbids in a parallel test because $PATH is process-wide. And it is the
	// only test that fetches yarn berry or date-fns, so unlike the barriers above
	// it warms nothing and blocks nothing.
	requireTools(t, "node", "npm", "npx", "corepack")

	root := syntheticProject(t, map[string]string{
		// The version pin corepack reads is `packageManager`; date-fns is exact
		// because the assertion names its version.
		"package.json": `{
  "name": "clew-pnp-probe",
  "version": "1.0.0",
  "packageManager": "yarn@` + yarnBerryVersion + `",
  "dependencies": { "date-fns": "4.1.0" }
}
`,
		"tsconfig.json": `{
  "compilerOptions": { "target": "ES2020", "module": "commonjs", "moduleResolution": "node", "strict": true },
  "include": ["src"]
}
`,
		// One import from a dependency. Without node_modules this line indexes to
		// nothing whatsoever, and that is the whole bug.
		"src/index.ts": `import { addDays } from "date-fns";

export function tomorrow(from: Date): Date {
  return addDays(from, 1);
}
`,
		// Berry defaults to PnP with no .yarnrc.yml at all; the key is written
		// explicitly so the fixture states its own premise.
		".yarnrc.yml": "nodeLinker: pnp\n",
	})
	yarnBerryOnPath(t)

	// Installed as the project's own developer would, with no override: this is
	// the tree clew has to cope with, so the test must not build it with the fix
	// already applied.
	runIn(t, root, "yarn", "install")

	if _, err := os.Stat(filepath.Join(root, ".pnp.cjs")); err != nil {
		t.Fatalf("yarn %s produced no .pnp.cjs, so this fixture is not a Plug'n'Play project "+
			"and proves nothing: %v", yarnBerryVersion, err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err == nil {
		t.Fatal("the Plug'n'Play install created a node_modules; the fixture no longer reproduces issue #8")
	}

	if got := units(t, root); len(got) != 1 || got[0] != ":typescript" {
		t.Fatalf("units = %v, want the root classified as a single typescript unit", got)
	}

	// Captured AFTER the install, which is what writes yarn.lock.
	before := readAll(t, root, ".yarnrc.yml", "yarn.lock")

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed the Plug'n'Play fixture in %.1fs", elapsed.Seconds())

	// The assertion the issue turns on. `date-fns 4.1.0` is the package and the
	// version the symbol carries; the descriptor after it is scip-typescript's
	// business and is not pinned.
	symbols := occurrenceSymbols(t, store, "src/index.ts")
	if !slices.ContainsFunc(symbols, func(s string) bool { return strings.Contains(s, "date-fns 4.1.0") }) {
		t.Errorf("no occurrence in src/index.ts carries a `date-fns 4.1.0` symbol: the import resolved "+
			"to nothing, which is issue #8 exactly -- an index that reports success with every "+
			"external symbol missing.\nsymbols present: %q", symbols)
	}

	// clew overrides the linker with an environment variable rather than by
	// editing the project, so both files it could have written are unchanged.
	t.Run("TheProjectsYarnConfigurationIsNotRewritten", func(t *testing.T) {
		for name, content := range readAll(t, root, ".yarnrc.yml", "yarn.lock") {
			if content != before[name] {
				t.Errorf("%s changed during indexing (%d bytes before, %d after); the linker override "+
					"must not touch the project's own configuration", name, len(before[name]), len(content))
			}
		}
	})

	// The other half of issue #8: `node_modules` missing is what makes clew
	// install, so an install that leaves none reinstalls on every invocation
	// forever. Its existence here is what ends that loop.
	t.Run("TheInstallDoesNotRepeatForever", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
			t.Errorf("indexing left no node_modules, so the next invocation installs again: %v", err)
		}
	})

	// The cost of the override, and the escape from it. Switching linkers does
	// not merely add a node_modules: yarn removes the .pnp.cjs it replaces, and a
	// zero-install repository commits that file. clew says so in its log, and
	// what makes that acceptable rather than destructive is that ONE command puts
	// it back -- so the claim is asserted rather than merely written down. See
	// doc/adr/0003-yarn-pnp-units-install-with-the-node-modules-linker.md.
	t.Run("PlugNPlayIsRestoredByTheProjectsOwnInstall", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(root, ".pnp.cjs")); err == nil {
			t.Skip("indexing left the .pnp.cjs in place; there is nothing to restore")
		}
		runIn(t, root, "yarn", "install")

		if _, err := os.Stat(filepath.Join(root, ".pnp.cjs")); err != nil {
			t.Errorf("`yarn install` did not restore .pnp.cjs, so clew's install is not undone by "+
				"one command as documented: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "node_modules")); err == nil {
			t.Error("`yarn install` left clew's node_modules behind; restoring Plug'n'Play is " +
				"documented as removing it")
		}
	})
}

// ----------------------------------------------------- SingleRepository_Angular

// angular-realworld exercises the angular.json detection branch, and pins the
// known template gap.
//
// It is also BUN-managed -- bun.lock, no package-lock.json -- which went
// unnoticed while every unit was installed with npm regardless. It passed then
// only because npm's fresh resolution of this particular tree happens to work;
// the bun branch of planInstall is what runs now.
func TestAcceptance_SingleRepository_Angular(t *testing.T) {
	// Not parallel: this is the barrier that warms bun's cache for
	// angular-realworld, which JavaAndAngular installs again. See the note at the
	// top.
	requireTools(t, "node", "npm", "npx", "bun")

	root := singleRepository(t, angularReal)

	if got := units(t, root); len(got) != 1 || got[0] != ":typescript" {
		t.Fatalf("units = %v, want the root classified as a single typescript unit", got)
	}
	if _, err := os.Stat(filepath.Join(root, "bun.lock")); err != nil {
		t.Fatalf("angular-realworld no longer ships a bun.lock, so this fixture no longer "+
			"covers the bun branch; drop bun from requireTools above if that is intended: %v", err)
	}

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed angular-realworld in %.1fs", elapsed.Seconds())

	// Component members ARE symbolised: the definitions exist.
	t.Run("ComponentMembersAreSymbolised", func(t *testing.T) {
		sym := findSymbol(t, store, "an Angular component", func(s string) bool {
			return strings.Contains(s, "Component#")
		})
		assertResolves(t, store, sym, "")
	})

	// What is missing is references FROM templates. scip-typescript reads .ts
	// and ignores .html entirely. Asserted rather than skipped, so closing the
	// gap -- a scip-angular producer -- flips this test rather than going
	// unnoticed.
	t.Run("TemplatesAreNotIndexed", func(t *testing.T) {
		templates := findFiles(t, root, ".component.html")
		if len(templates) == 0 {
			t.Fatal("the fixture has no component templates; the layout has changed")
		}
		var indexed int
		for _, rel := range templates {
			if _, ok := store.Document(rel); ok {
				indexed++
			}
		}
		if indexed > 0 {
			t.Errorf("%d of %d Angular templates are present in the index -- "+
				"the known template gap has closed, and AGENTS.md plus doc/README.md need updating",
				indexed, len(templates))
		}
		t.Logf("%d component templates on disk, %d in the index", len(templates), indexed)
	})

	// Document.language is left EMPTY by scip-typescript where scip-java sets
	// "java". Nothing downstream may branch on it; this is the assertion that
	// says so out loud, and the one that will notice if it stops being true.
	t.Run("DocumentLanguageIsEmpty", func(t *testing.T) {
		for _, rel := range findFiles(t, root, ".component.ts") {
			doc, ok := store.Document(rel)
			if !ok {
				continue
			}
			if doc.Language != "" {
				t.Errorf("scip-typescript now sets Document.language=%q for %s -- "+
					"the invariant in AGENTS.md and doc/README.md can be revisited",
					doc.Language, rel)
			}
			return
		}
		t.Fatal("no indexed component found to check Document.language on")
	})
}

// -------------------------------------------------------- SingleRepository_Python

func TestAcceptance_SingleRepository_Python(t *testing.T) {
	t.Skip("no Python producer exists yet; flask is the intended first fixture " +
		"(src/ layout, every dependency pure Python)")
}

// ------------------------------------------------------- Monorepo_PnpmWorkspace

// zod: root package.json + tsconfig.json + pnpm-workspace.yaml. clew classifies
// the root as ONE unit and never descends into packages/. This test pins that
// behaviour so a change to it is deliberate rather than a side effect.
//
// Discovery only: the classification is the claim, and a pnpm install of the
// whole workspace buys nothing the immer fixture does not already cover.
func TestAcceptance_Monorepo_PnpmWorkspace(t *testing.T) {
	t.Parallel()
	root := singleRepository(t, zod)

	got := units(t, root)
	if len(got) != 1 || got[0] != ":typescript" {
		t.Fatalf("units = %v, want exactly one unit at the workspace root", got)
	}
	// The packages exist and are individually classifiable; not descending is a
	// decision, not an accident.
	if _, err := indexer.Discover(filepath.Join(root, "packages")); err != nil {
		t.Fatal(err)
	}
	if inner := units(t, filepath.Join(root, "packages")); len(inner) == 0 {
		t.Log("NOTE: zod's packages/ no longer holds classifiable units; the fixture layout has changed")
	}
}

// ---------------------------------------------------- Monorepo_MultiModuleMaven

// commons-math has nine <module> entries and no root src/main/java.
//
// This CURRENTLY FAILS, and the test asserts the failure rather than skipping
// it: indexMaven reads only <unit>/src/main/java while Discover refuses to
// descend, so an aggregator pom yields a unit with no sources. Recording the
// gap in the suite means closing it flips a test from red to green.
func TestAcceptance_Monorepo_MultiModuleMaven(t *testing.T) {
	t.Parallel()
	requireTools(t, "mvn", "javac", "java")

	root := singleRepository(t, commonsMath)

	got := units(t, root)
	if len(got) != 1 || got[0] != ":maven" {
		t.Fatalf("units = %v, want the aggregator root as a single maven unit", got)
	}

	err := indexer.Run(context.Background(), indexer.Options{Root: root, Log: testLogger{t}})
	if err == nil {
		t.Fatal("multi-module Maven now indexes successfully -- " +
			"remove this expectation, assert on the resulting index, and update " +
			"the known-gaps list in AGENTS.md and doc/adr/0001-testing-strategy.md")
	}
	if !strings.Contains(err.Error(), "every unit failed") {
		t.Errorf("indexing failed with %v; expected the no-sources failure at the aggregator root", err)
	}
	t.Logf("known gap reproduced: %v", err)
}

// -------------------------------------------------- Superproject_JavaCrossSubmodule

// The test for clew's central claim: cross-unit resolution is a plain string
// join on symbol names, which embed the Maven coordinate including version.
//
// commons-text pins commons.lang3.version to 3.20.0, and commons-lang is pinned
// to the release that declares exactly that version. Indexing commons-lang from
// source yields definitions symbolised as
//
//	scip-java maven maven/org.apache.commons/commons-lang3 3.20.0 org/apache/commons/lang3/StringUtils#
//
// and indexing commons-text yields references carrying the identical string,
// because scip-javac stamps classpath symbols with the same coordinate.
//
// THE VERSION ALIGNMENT IS THE FIXTURE. Moving either pin without the other
// makes the symbol strings diverge and this test fails for a reason that has
// nothing to do with clew.
func TestAcceptance_Superproject_JavaCrossSubmodule(t *testing.T) {
	t.Parallel()
	requireTools(t, "mvn", "javac", "java")

	root := superproject(t, map[string]Project{
		"commons-lang": commonsLang,
		"commons-text": commonsText,
	})

	if got := units(t, root); len(got) != 2 {
		t.Fatalf("units = %v, want one per submodule", got)
	}

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed the superproject in %.1fs", elapsed.Seconds())

	symbol, defPath, refPath := findCrossUnitSymbol(t, store, "commons-lang", "commons-text")
	t.Logf("cross-unit symbol: %s\n  defined  %s\n  referenced %s", symbol, defPath, refPath)

	if !strings.Contains(symbol, "maven/org.apache.commons/commons-lang3 3.20.0") {
		t.Errorf("the joining symbol %q does not carry the expected 3.20.0 coordinate; "+
			"check that the commons-lang and commons-text pins are still aligned", symbol)
	}
}

// findCrossUnitSymbol returns a symbol defined under defPrefix and referenced
// from refPrefix -- the federation mechanism, demonstrated rather than asserted
// in the abstract.
func findCrossUnitSymbol(t *testing.T, store *index.Store, defPrefix, refPrefix string) (symbol, defPath, refPath string) {
	t.Helper()
	for _, si := range store.SearchSymbols("commons-lang3", 0) {
		if !hasMavenCoordinate(si.Symbol) {
			continue
		}
		defs := store.Definitions("", si.Symbol)
		if len(defs) == 0 || !strings.HasPrefix(defs[0].Path, defPrefix+"/") {
			continue
		}
		for _, r := range store.References("", si.Symbol, false) {
			if strings.HasPrefix(r.Path, refPrefix+"/") {
				return si.Symbol, defs[0].Path, r.Path
			}
		}
	}
	t.Fatalf("no symbol is both defined under %s/ and referenced from %s/ -- "+
		"cross-unit resolution is broken, or the coordinates the two units stamp have diverged",
		defPrefix, refPrefix)
	return "", "", ""
}

// ------------------------------------------------- Superproject_JavaAndAngular

// The polyglot superproject, mirroring the layout clew was built for: a Java
// service and an Angular frontend under one root, indexed into one index.
//
// bun is required for the same reason as in SingleRepository_Angular: the web
// unit is the same bun-managed fixture, and clew installs it with the manager it
// declares.
func TestAcceptance_Superproject_JavaAndAngular(t *testing.T) {
	t.Parallel()
	requireTools(t, "mvn", "javac", "java", "node", "npm", "npx", "bun")

	root := superproject(t, map[string]Project{
		"java/commons-lang": commonsLang,
		"web":               angularReal,
	})

	got := units(t, root)
	want := map[string]bool{"java/commons-lang:maven": true, "web:typescript": true}
	if len(got) != 2 {
		t.Fatalf("units = %v, want %v", got, want)
	}
	for _, u := range got {
		if !want[u] {
			t.Errorf("unexpected unit %q; want one of %v", u, want)
		}
	}

	store, elapsed := buildIndex(t, root)
	t.Logf("indexed the polyglot superproject in %.1fs", elapsed.Seconds())

	// Both units are present in one index, and their paths are disambiguated by
	// prefix -- the merge rule that exists because Document.relative_path is the
	// only disambiguator SCIP has.
	var java, web int
	for _, si := range store.SearchSymbols("", 0) {
		for _, d := range store.Definitions("", si.Symbol) {
			switch {
			case strings.HasPrefix(d.Path, "java/commons-lang/"):
				java++
			case strings.HasPrefix(d.Path, "web/"):
				web++
			}
		}
	}
	if java == 0 {
		t.Error("no definitions under java/commons-lang/ -- the Java unit is missing from the merged index")
	}
	if web == 0 {
		t.Error("no definitions under web/ -- the Angular unit is missing from the merged index")
	}
	t.Logf("merged index holds %d Java and %d TypeScript definition sites", java, web)
}
