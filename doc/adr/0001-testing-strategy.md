# 1. Testing strategy: three tiers, hermetic by default

- **Status:** Accepted
- **Date:** 2026-07-25

## Context

clew has no tests. Not few — none. `make test` runs `go test ./...` against zero
test files, there is no Lua harness, and there is no CI. `README.md:15` describes
an intended v1, and `internal/indexer/java.go:40` cites
`TestMavenSymbolsCarryCoordinates` as though it exists. It does not; it is a plan,
recorded here.

Three things make this more urgent than the usual backlog item.

**The expensive lessons are undefended.** The Maven coordinate-stamping bug
documented at `java.go:25-40` is *invisible locally* — navigation inside a unit
keeps working, and the damage only appears once units merge and every symbol
collapses into the same anonymous package. Nothing currently stops it recurring.

**A dependency move just changed the code under the query layer.** Repinning to
`github.com/scip-code/scip/bindings/go/scip v0.9.0` is what made
`single_line_range` and `multi_line_range` exist. `OccurrenceRange` handles those
plus the deprecated packed `range`, and no test asserts any of the three.

**Platform support is claimed but unverified.** `README.md` now states macOS, Linux
and WSL. Nothing checks any of them.

ADR 2 helps here in a way worth naming: once a producer is a declaration rather
than Go code, a producer can be *faked*. A declaration whose command copies a
prepared index into `$CLEW_OUTPUT` exercises discovery, dispatch, path prefixing
and merge without any indexer installed.

## Decision

### Three tiers

**Tier 1 — unit. Hermetic: no network, no toolchain, no fixtures on disk.**
Discovery against directory trees the test builds in `t.TempDir()`. Merge and
query against `scip.Index` values constructed programmatically with the Go
bindings and marshalled in memory, so there are no committed `.scip` blobs and no
indexer involved. Lua logic under plenary. This tier runs on every change and
finishes in seconds.

**Tier 2 — producer contract. Hermetic.** A declared producer whose command copies
a prepared index into `$CLEW_OUTPUT`, proving the path from discovery through
dispatch, prefixing and merge without `mvn`, `npx` or a JDK. Also covers config
loading: unknown keys rejected, user declarations shadowing embedded defaults by
`kind`, ordering as precedence. Depends on ADR 2 being implemented.

**Tier 3 — acceptance. Network and toolchains required, and excluded by default.**
Real projects, downloaded at test time, indexed for real, asserted on. Behind
`//go:build acceptance` so `go test ./...` never reaches the network, with
`make test-acceptance` to run it deliberately.

`make test` runs tiers 1 and 2. `make test-go`, `make test-lua` and
`make test-acceptance` address them individually.

### Test projects are downloaded, never committed

Acceptance projects are fetched at test time rather than vendored into the
repository.

- **Pinned to a commit SHA**, fetched as a tarball rather than cloned. A branch
  would let an upstream change break clew's suite with no change on clew's side.
- **Cached outside `t.TempDir()`**, under `$XDG_CACHE_HOME/clew-test/<sha>/`, so
  repeated local runs do not re-download. Indexing output still goes to a fresh
  temp directory per run.
- **Asserted on properties, not bytes.** A golden-file diff against a known-good
  index is too brittle: SCIP output moves with indexer versions, and
  `ScipTypeScriptPackage` is pinned to `@latest` (`typescript.go:11`), so the
  producer moves underneath the baseline. Assert that a symbol carries a real
  `maven/<group>/<artifact> <version>` coordinate, that a named definition
  resolves, that a cross-unit reference resolves — claims that survive an indexer
  upgrade.

### Lua tests use plenary.nvim

Surveyed five plugins: parrot.nvim, aerial.nvim and telescope.nvim all use
plenary's busted runner with `*_spec.lua` and a `tests/minimal_init.lua`;
lazy.nvim uses real busted via `.busted` and `nvim -l`; blink.cmp has no Lua suite
at all, so the repo-layout precedent `AGENTS.md` records borrowing from it does not
extend to testing.

plenary wins on setup cost: one `git clone --depth 1` and a minimal init, with no
LuaRocks and no `busted` install. That matters because CI already needs Go, and
tier 3 needs a JDK, Maven and Node. clew's Lua surface is also small and pure —
root detection, config merge, binary resolution — with no async and no UI, so
plenary's `describe`/`it` and luassert are sufficient.

Real busted under `nvim -l` is the better long-term shape and plausibly where the
ecosystem drifts. plenary's API is a subset of busted's, so migrating `*_spec.lua`
later is mechanical. Revisit if the Lua surface grows.

### CI

GitHub Actions, matrix over Linux and macOS, running tiers 1 and 2 on every push
and pull request. Tier 3 runs on a schedule rather than per-change, because it
needs toolchains and minutes. WSL is claimed in `README.md` but will not be
verified by CI; that gap is stated rather than hidden.

### Tests are named for the project layout they exercise

`README.md` claims three shapes -- single repository, git superproject, monorepo --
and each is a distinct path through discovery and merge. Test names say which shape
they cover, at every tier, so a gap in coverage is visible from the test list
alone: `TestDiscover_Superproject`, `TestAcceptance_Monorepo_MultiModuleMaven`.

| Scenario | Project | Notes |
| --- | --- | --- |
| `SingleRepository_Maven` | `spring-projects/spring-petclinic` | The pipeline's original validation target (`java.go:18`). Carries both `pom.xml` and `build.gradle`, so it also covers producer precedence |
| `SingleRepository_MavenLarge` | `apache/commons-lang` | Single module, real `src/main/java`. The indexing-time measurement |
| `SingleRepository_TypeScript` | `immerjs/immer` | `package.json` + `tsconfig.json`, no workspace file |
| `SingleRepository_Angular` | `gothinkster/angular-realworld-example-app` | The `angular.json` branch, and the known template gap |
| `SingleRepository_Python` | `pallets/flask` | Once a Python producer exists. `src/` layout, and every dependency is pure Python, so nothing builds from source on either platform |
| `Monorepo_PnpmWorkspace` | `colinhacks/zod` | Root `package.json` + `tsconfig.json` + `pnpm-workspace.yaml`. clew classifies the root as one unit and never descends into `packages/`; the test pins that behaviour so a change to it is deliberate |
| `Monorepo_MultiModuleMaven` | `apache/commons-math` | Nine `<module>` entries, no root `src/main/java`. **Currently fails** -- see below |
| `Superproject_JavaCrossSubmodule` | `apache/commons-lang` + `apache/commons-text` | Cross-submodule symbol resolution between two Java repositories |
| `Superproject_JavaAndAngular` | `commons-lang` + `angular-realworld` | The polyglot superproject, mirroring the layout clew was built for |

Superproject fixtures are **composed at test time** from separately downloaded
repositories. Nothing is committed; only the arrangement is synthetic, and the
arrangement is the thing under test.

### The cross-submodule fixture, and why this pair

`Superproject_JavaCrossSubmodule` is the test for clew's central claim, so its
construction matters. `commons-text` pins `commons.lang3.version` to `3.20.0`, and
`commons-lang` publishes a `rel/commons-lang-3.20.0` tag whose pom declares exactly
that version. Indexing `commons-lang` from source therefore yields definitions
symbolised as:

    scip-java maven maven/org.apache.commons/commons-lang3 3.20.0 org/apache/commons/lang3/StringUtils#

and indexing `commons-text` yields references carrying the identical string,
because `scip-javac` stamps classpath symbols with the same coordinate. Resolution
across the two is a string match, which is the federation mechanism described in
`doc/README.md`, demonstrated on real code rather than a hand-built pair.

**The version alignment is the fixture, not an incidental detail.** Pinning either
side to a SHA whose declared version differs -- a `-SNAPSHOT` pom, a newer tag --
makes the symbol strings diverge and the test fail for a reason that has nothing to
do with clew. Both pins must be updated together.

## Known gaps this makes visible

**Multi-module Maven does not work.** `indexMaven` collects sources from
`<unit>/src/main/java` only (`java.go:74`) and errors when it finds none, while
`Discover` stops at the first `pom.xml` without descending (`discover.go:69`). An
aggregator pom has no sources at its root, so indexing fails outright.

`discover.go:46` justifies not descending on the grounds that "scip-java already
handles multi-module Maven and Gradle builds as a single unit" -- but `indexMaven`
does not use scip-java's build integration, it drives `javac` directly. The comment
describes a design the code does not implement.

`Monorepo_MultiModuleMaven` asserts the current failure rather than skipping it, so
the gap is recorded in the suite and closing it flips a test from red to green.

One project per language to begin with. The suite proves the pipeline before it
proves breadth, and every fixture costs a download, a toolchain and minutes on
every scheduled run.

`fastapi/fastapi` is the intended second Python fixture, not a rejected one. It is
annotation-dense -- Pydantic models, generics, `Annotated[...]` -- which is where a
semantic indexer earns its keep over a regex, whereas flask's older style proves
the pipeline runs without proving much about resolution quality. It is deferred
only because `pydantic-core` is a Rust-built wheel and a first fixture should have
nothing that can fail to install.

`django/django` was considered and dropped: its value was indexing-time
measurement, which `commons-lang` already supplies, and 282 MB per scheduled run
buys little beyond that.

### Pinned commits

Release tags where the project publishes meaningful ones, default-branch commits
where it does not. `spring-petclinic`'s only tag is the ancient `1.5.x` Spring Boot
line and `angular-realworld`'s tags are CI build numbers, so both pin to `main`.

| Project | Ref | Commit |
| --- | --- | --- |
| `apache/commons-lang` | `rel/commons-lang-3.20.0` | `598dfc163b8b410fb3bb8794521206ec8dcec82a` |
| `apache/commons-text` | `rel/commons-text-1.15.0` | `04e937470d3679cc163df85d82d5b6d2e3e71128` |
| `spring-projects/spring-petclinic` | `main` | `f182358d02e4a68e52bdbabf55ca7800288511e7` |
| `immerjs/immer` | `v11.1.15` | `a3be9df762c1dbe9959a011ddbab0ce838cbc468` |
| `pallets/flask` | `3.1.3` | `22d924701a6ae2e4cd01e9a15bbaf3946094af65` |
| `colinhacks/zod` | `v4.4.3` | `1fb56a5c18c27102dbc92260a4007c7732a0ccca` |
| `apache/commons-math` | `master` | `912fd9c4ebc56a78293deb703443fe0f5d5f8f89` |
| `gothinkster/angular-realworld-example-app` | `main` | `dd99ed2cf39c805d719f943c5d7061a5683d98a8` |

**Pin commit SHAs, not tag names.** Apache's release tags are *annotated*, so
`git/ref/tags/...` returns the tag object rather than the commit —
`rel/commons-lang-3.20.0` resolves to `5027883…` as a tag and `598dfc1…` as a
commit. A tarball URL built from the former is simply broken, and every SHA above
was verified by resolving it back to a commit.

The `commons-lang` and `commons-text` pins are coupled: `rel/commons-text-1.15.0`
declares `commons.lang3.version` as `3.20.0`, which is why `commons-lang` is pinned
to exactly that release. Moving one without the other silently breaks
`Superproject_JavaCrossSubmodule`.

## Consequences

- The default test run stays offline and fast, so a network failure or a missing
  JDK never blocks development. This is not hypothetical: a firewall rule blocking
  the Go binary cost an afternoon on 2026-07-25.
- Real indexer behaviour is only covered on a schedule, so a regression in the
  Maven pipeline may be found a day late rather than on the commit that caused it.
  Accepted deliberately: the alternative is a per-change suite slow enough that
  people stop running it.
- Downloads make acceptance tests dependent on GitHub availability and on the
  pinned projects remaining public.
- plenary.nvim becomes a development dependency, cloned in CI and by
  `tests/minimal_init.lua` locally. It is not a runtime dependency.

## Implementation status

Added 2026-07-25, after the decision above was carried out. The decision itself
is unchanged; this records what landed and what the suite found on its first
run.

**Tiers 1 and 3 are implemented. Tier 2 is not**, because a producer can only be
faked once it is a declaration rather than Go code, which is ADR 2. `make test`
therefore runs tier 1 alone today, and will pick up tier 2 without changing.

The `TestMavenSymbolsCarryCoordinates` the Context describes as a plan now
exists, as `TestAcceptance_SingleRepository_Maven/SymbolsCarryCoordinates`; the
reference in `internal/indexer/java.go` points at it.

### What the suite found immediately

Two defects, neither previously known, both now asserted as current failures in
the same style as `Monorepo_MultiModuleMaven`:

- **A symlink in the project path degrades every Maven coordinate.** The
  aggregator recovers coordinates by walking up from the `-d` directory to a
  `pom.xml`, bounded by a *realpath'd* sourceroot, while clew passes the path it
  was given. The two disagree for any project behind a symlink, and the result
  is precisely the `scip-java maven . . ` collapse this ADR was written to
  defend against — reached by a route nobody had considered.
  `TestAcceptance_SingleRepository_MavenViaSymlink`.
- **`npm install` is assumed for every TypeScript unit,** so a yarn- or
  pnpm-managed repository fails before `scip-typescript` runs.
  `TestAcceptance_SingleRepository_TypeScript`.

The first is worth dwelling on: it validates the argument in the Context. The
bug is invisible locally, survives every hermetic test, and was found by the
first acceptance run on a real project.

### A correction to the pinned-commit table

`immerjs/immer` is recorded above as `v11.1.15` at
`a3be9df762c1dbe9959a011ddbab0ce838cbc468`, but the tree at that SHA declares
`"version": "10.0.3-beta"`. The ref label and the commit disagree and the pin
needs re-resolving. The SHA is left as recorded rather than quietly changed,
since this table is the decision; `internal/acceptance/fixtures.go` carries the
same note.
