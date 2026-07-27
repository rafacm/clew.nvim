# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Sections are headed by date (`YYYY-MM-DD`) rather than version number, newest first.

## 2026-07-27

### Added

- **`TestAcceptance_SingleRepository_YarnPnP`**, tier 3, and the first fixture
  here that is *written* rather than downloaded. What it tests is a package
  manager's linker, not any repository's code, so four files pin it where
  `yarnpkg/berry` would have cost a large download to say the same thing; ADR 1
  carries the exception and the rule it leaves. It asserts on a **dependency's**
  symbol, which is the assertion whose absence let issue #8 through — every
  other TypeScript test here asserts on the project's own source, which resolves
  with no install at all. It also asserts the documented recovery, that one
  `yarn install` restores Plug'n'Play and removes clew's `node_modules`.
- Tier 1 cases for the berry branch of `planInstall`, including that a
  `.yarnrc.yml` with no `nodeLinker` key gets the override — absence of the key
  is berry's PnP default, not the absence of PnP.

### Decided

- **ADR 3: Yarn Plug'n'Play units are installed with the node-modules linker.**
  The alternative that would retire it is named there: running `scip-typescript`
  under yarn's own PnP loader materialises nothing and is the honest fix, but it
  needs PnP-aware resolution inside a tool clew does not own. The override
  exists only while that route is closed. Editing `.yarnrc.yml` and refusing to
  index a PnP unit were both rejected, and the reasoning is recorded rather than
  left to be re-derived.
- **The product review in issue #5 is answered and closed**, split into issues
  #10 through #16. Nothing here is implemented; this entry records the
  conclusions so they are not re-derived. The review was directionally right and
  factually half-stale: two of its six proposed v1 blockers had already shipped
  as issues #2 and #3, one was inverted, and its strongest suggestion pointed at
  the wrong target.
- **ADR 2 must be amended before it is implemented** (issue #11). It remains
  Accepted and unbuilt. Four things learned since it was written change what
  implementing it means, and two contradict the ADR's own text. First,
  `scip-typescript` no longer qualifies to move to a TOML declaration: it meets
  the ADR's own criterion 2, per-step error policy, as of `8193ab6` — which
  landed ten hours after `cc07ae1` finalised the ADR — so the stated outcome
  "after this it is `scip-java` alone" is wrong. Second, the "environment
  variables in, a SCIP index out" contract has nowhere to declare what indexers
  omit, and three fields are already known to be omitted: `SymbolInformation.kind`,
  `Document.language` and `Document.position_encoding`.
- **`scip-io` is not a replacement for clew's indexing half**, and the review's
  suggestion to lean on it was declined on evidence. Its merge deduplicates
  overlapping documents where clew must prefix them per unit — every Java unit
  contains `src/main/java/...`, so that is the collision, not a duplicate — and
  nothing indicates it stamps Maven coordinates. It does belong in ADR 2 as a
  worked example of a *declared* producer, which is the suggestion in the form
  the repository can act on.
- **The upstream SCIP bindings now own six primitives clew hand-rolls**
  (issue #13). `github.com/scip-code/scip/bindings/go/scip` v0.9.0 is already a
  direct dependency and ships `SourceRange()`, `Range`/`Position`/`Contains`,
  `FindOccurrences`, `IsLocalSymbol`, `ParseSymbol` and `SymbolTable()`. Its
  range decoder is better than ours: it also guards nil inner messages and covers
  `enclosing_range`, which clew never reads. The three-encodings invariant in
  `AGENTS.md` should become a pointer to the bindings.

### Fixed

- **Yarn Plug'n'Play projects index with their dependencies** (issue #8). Yarn
  2+ installs PnP *by default* — no `node_modules`, dependencies zipped under
  `.yarn/cache`, resolution through a generated `.pnp.cjs` — and
  `scip-typescript` resolves imports the way Node does, so against a PnP tree it
  resolved nothing and said nothing: exit 0, and an index missing every external
  symbol while the project's own symbols and TypeScript's lib types made it look
  complete. A berry unit is now installed with `YARN_NODE_LINKER=node-modules`.
  Measured on a `date-fns@4.1.0` fixture: 18 occurrences and no `date-fns`
  symbol before, 19 and the `addDays()` symbol after, matching a
  `nodeLinker: node-modules` control built from the same lockfile exactly.
- **A PnP unit no longer reinstalls on every single invocation.** `node_modules`
  missing is what makes clew install, so under PnP that condition never became
  false. The same override ends the loop, and an install that leaves no
  `node_modules` is now reported rather than passed over — the second and third
  runs of the fixture install nothing.
- **The linker override is announced, including what it costs.** Yarn does not
  merely add a `node_modules`: it removes the `.pnp.cjs` it replaces, which a
  zero-install repository commits. clew logs the override, the reason, and the
  one command that reverses both. Nothing of the project's own is written —
  `.yarnrc.yml` and `yarn.lock` are asserted byte-identical across an index.

### Known gaps

- **`packageManager` in `package.json` is still not read** (issue #18), which is
  issue #8's other half and is untouched by the fix above. clew runs whichever
  `yarn` is on `$PATH`, so a project pinning yarn 4 with yarn 1 installed fails
  — loudly, in yarn's own words, naming corepack, which is why it is the lesser
  problem. It is also why the new tier 3 fixture reaches berry through a corepack
  shim on `$PATH` rather than by clew's own resolution.
- **`clew lsp` ignores `index_path`** (issue #12). `lua/clew/lsp.lua` sends
  `settings.clew.indexPath` and `internal/lsp/server.go` hardcodes
  `<root>/.clew/index.scip`, never reading `settings` or `initializationOptions`.
  Indexing, `:checkhealth clew` and the file watcher all honour the configured
  path, so setting it reports healthy and serves nothing. Documented in three
  places and dead in one.
- **The position-encoding comment in `internal/lsp/server.go` is backwards**
  (issue #14), and misled the review into promoting a non-bug to a v1 blocker.
  Both shipping indexers emit UTF-16 code units — `scip-typescript` via
  TypeScript's `getLineAndCharacterOfPosition`, `scip-java` via the JVM
  `LineMap` — and neither stamps `Document.position_encoding`, so it stays
  `Unspecified`. LSP's default is UTF-16, which is what clew advertises, so clew
  is correct today. The real exposure is the mirror image: the first producer for
  an indexer written in Go, Rust or C++ emits UTF-8 byte offsets, and that
  arrives precisely when ADR 2 makes such producers cheap.
- **`SymbolInformation.kind` cannot be fixed by reading `SymbolInformation.kind`**
  (issue #10). `scip-java` populates it; `scip-typescript` never does, so reading
  it alone passes against the Java fixture and leaves every TypeScript symbol
  unkinded. Upstream has known since 2024 — `sourcegraph/scip-typescript#360`,
  with a draft PR at `#361` abandoned since 2024-08-12 — so no fix is arriving on
  its own.

## 2026-07-25

### Added

- **Tiers 1 and 3 of ADR 1 are implemented; the repository has tests.** Tier 1 is
  hermetic — discovery against trees built in `t.TempDir()`, merge and query
  against `scip.Index` values constructed with the Go bindings, the language
  server driven end to end over an in-memory pipe, and the Lua surface under
  plenary. No `.scip` blob is committed and no indexer is invoked. Tier 3 lives
  in `internal/acceptance` behind `//go:build acceptance`, downloading the
  pinned projects from ADR 1's table and asserting on properties rather than
  bytes. `make test` runs the hermetic tiers; `make test-go`, `make test-lua`
  and `make test-acceptance` address them individually.
- **Tier 2 is not implemented.** A producer can only be faked once it is a
  declaration rather than Go code, so it waits on ADR 2. `make test` therefore
  runs tier 1 alone today.
- `TestAcceptance_Superproject_JavaCrossSubmodule` demonstrates clew's central
  claim on real code: `org/apache/commons/lang3/Validate#`, carrying the
  coordinate `maven/org.apache.commons/commons-lang3 3.20.0`, is defined in a
  `commons-lang` checkout and resolved from a reference in a separately indexed
  `commons-text`. Cross-unit resolution is a string join, demonstrated rather
  than asserted in the abstract.
- CI: GitHub Actions over Linux and macOS. `test.yml` runs tier 1, `gofmt`,
  `go vet` (including under the `acceptance` tag, which `./...` otherwise never
  compiles) and a check that committed `doc/tags` is current. `acceptance.yml`
  runs tier 3 daily, with the fixture download cached by commit SHA. WSL is
  claimed in `README.md` and is not verified by CI; the gap is stated rather
  than hidden.
- `AGENTS.md`, the instructions for working in this repository: commands, the
  invariants that fail silently when broken, the three documentation surfaces
  that must stay in sync, known gaps and open questions. `CLAUDE.md` imports it,
  so every agent reads one file.
- ADR 1, a testing strategy: three tiers, hermetic by default. Unit and
  producer-contract tests need no network and no toolchain; acceptance tests
  download real projects at pinned commits and sit behind a build tag so
  `go test ./...` never reaches the network. Lua tests use plenary.nvim, the
  harness parrot.nvim, aerial.nvim and telescope.nvim all use. Tests are named
  for the project layout they exercise, and `Superproject_JavaCrossSubmodule`
  covers cross-submodule symbol resolution using `commons-text`'s dependency on
  `commons-lang` at a matching version.
- `doc/adr/`, holding architectural decision records in the Nygard format, and
  ADR 2: a producer is a TOML declaration, read by the clew binary rather than
  by the Neovim plugin's Lua, so a new language needs no clew release and
  `clew index` keeps working in CI and from other editors. The producers clew
  ships with are the same declarations, embedded with `go:embed`, so there is
  one mechanism rather than two. `scip-java` stays in Go as a documented
  exception: it needs cross-unit state, per-step error policy and a generated
  file, which are the three criteria for justifying any future Go producer.
  Not implemented yet; the record is the decision, not the feature.
- `make docs`, regenerating `doc/tags` so `:help clew` works from a source
  checkout and a malformed `*tag*` fails in the Makefile rather than for a user.
- Supported platforms stated in `README.md` and `doc/clew.txt`: macOS, Linux,
  and Windows via WSL. Native Windows is not supported, because indexers are
  driven as shell commands assuming a POSIX shell.
- "Adding a language" in `doc/README.md`, separating the half that is
  language-agnostic today (reading an index) from the half that needs knowledge
  written down somewhere (driving an indexer).
- Producer registry in `internal/indexer`. A language is now one `Producer`
  implementation plus one line in the registry, where it used to be three
  coordinated edits across `discover.go` and `run.go` with no compiler check
  tying them together. Registry order is the detection precedence, so the
  TypeScript-before-Maven rule is expressed rather than commented.
- `runner.memo`, a per-run cache for cross-unit resolutions such as a
  classpath or, later, a virtualenv.
- `doc/README.md` cites four more pieces of prior art: clangd's remote index
  (the production precedent for the whole architecture, and the reference
  design for staleness), `scip-io` (the closest existing thing to clew's
  indexing half), GNU GLOBAL, and LSIF with `vscode-lsif-extension`.

### Changed

- Two defects were found by the acceptance suite on its first run, each asserted
  as a current failure following the precedent set for multi-module Maven, so
  that closing it flips a test rather than going unnoticed. The mechanism has
  now been exercised in both directions:
  - **A symlink anywhere in the project path destroys every Maven coordinate.**
    Recorded as issue #2 and **fixed the same day** — see *Fixed*, below.
  - **`npm install` is assumed for every TypeScript unit,** so a yarn- or
    pnpm-managed repository fails before `scip-typescript` runs at all.
    Recorded as issue #3 and **fixed the same day** — see *Fixed*, below.
- Tier 3 runs in parallel, and caches the downloads it had been repeating. Seven
  of ten tests now call `t.Parallel()`; the three that do not are barriers, since
  Go runs sequential tests first and no two concurrent tests may download the
  same artifact — a Maven local repository is not safe for concurrent writes of
  one artifact across processes. `acceptance.yml` also caches the npm, yarn and
  bun download caches, which nothing had covered: immer's yarn install was 43
  seconds of a 115-second suite, re-fetched from the registry on every run.
- Staleness is described honestly in `doc/README.md`: "nearly free" is
  qualified with the drift that follows editing, the clangd hybrid clew is
  aimed at, and the fact that `staleness_check` today only reports index age.
- `README.md` no longer claims support for "any language that has a SCIP
  indexer". The architecture is still language-agnostic; the wording now says
  which indexers are actually wired up, matching the dependency table.
- `README.md` restructured: the logo leads the header, the nav bar sits directly
  under the title, and a new "What is clew.nvim?" section answers that question
  in one paragraph.
- "Project shapes" folded into "Features", which is now one flat section with
  links to the tools, formats and Neovim features it names.
- "Why a precomputed index?", "Related Projects" and "Credits" moved out of
  `README.md` into a new `doc/README.md`, linked from a short "Background"
  section.

### Removed

- `NOTES.md`. Its durable content -- the empirically verified invariants, the
  open questions, the known gaps -- moved to `AGENTS.md`. Its status table and
  roadmap had gone stale, and its build-failure section was actively misleading:
  it recorded a sandbox restriction and a network block as ruled out, when the
  cause was a firewall rule denying the Go binary specifically, which is exactly
  why `curl` had succeeded.

### Fixed

- **A TypeScript unit is installed with its own package manager** (issue #3).
  `npm install` ran unconditionally, so a yarn-, pnpm- or bun-managed repository
  was resolved by a manager it does not use — and where npm's resolver refuses
  the project's graph outright, as it does for immer's, the unit produced no
  index at all rather than a degraded one. The lockfile now decides:
  `pnpm-lock.yaml`, `yarn.lock` (classic or berry, distinguished by the file's
  own banner), `bun.lock`/`bun.lockb`, `package-lock.json`, and npm's last
  because it is the one most often left behind by an accidental `npm install`.
  A project whose manager is missing from `$PATH` now fails naming both the
  lockfile and the binary, rather than reaching for npm and indexing against a
  dependency tree nobody uses. Every install is frozen, because "no build file
  is ever modified" covers a lockfile: the sole exception is `npm ci`, which
  refuses a lockfile that has drifted from `package.json`, and there
  `installDependencies` falls back to `npm install` and logs that it may rewrite
  it. `TestAcceptance_SingleRepository_TypeScript` now indexes immer through
  yarn and asserts `yarn.lock` is byte-identical afterwards; `planInstall` is
  covered hermetically.
- The Angular fixture had been indexed with the wrong package manager all along.
  `angular-realworld` ships a `bun.lock` and no `package-lock.json`, so
  `TestAcceptance_SingleRepository_Angular` was green while installing a tree
  npm resolved from scratch — the exact failure acceptance testing exists to
  catch, passing because the assertions never looked at how dependencies got
  there. It now installs with `bun`, and `acceptance.yml` installs `yarn` and
  `bun` alongside Node.
- **A symlink in the project path no longer destroys every Maven coordinate**
  (issue #2). `scip-java` bounds its search for the unit's `pom.xml` by a
  *realpath'd* sourceroot while clew passed the path the user gave, so for any
  project behind a symlink — a symlinked workspace, everything under `/tmp` on
  macOS — no pom was found and every symbol degraded to `scip-java maven . . `.
  All 158 spring-petclinic symbols were affected. The degraded form is
  internally consistent, so navigation inside the unit kept working and nothing
  surfaced until units merged and collapsed into one anonymous package.
  `indexer.resolveRoot` now resolves the root once, before discovery walks it,
  which fixes every producer rather than Maven alone: `filepath.WalkDir` does
  not follow symlinks, so a real root guarantees a real `Unit.Dir`. The same
  change makes `--root` pointed *directly* at a symlink work, where it
  previously discovered nothing at all for the same reason.
  `TestAcceptance_SingleRepository_MavenViaSymlink` now asserts that coordinates
  survive; `TestDiscover_SymlinkInTheRootPathIsResolved` and
  `TestDiscover_RootItselfIsASymlink` cover the mechanism hermetically.
- The project builds. The SCIP Go bindings moved with the format's transfer to
  the `scip-code` org and are now their own module, so the pinned
  `github.com/sourcegraph/scip v0.6.0` predated the typed occurrence ranges
  `internal/index/query.go` reads: it did not compile. Repinned to
  `github.com/scip-code/scip/bindings/go/scip v0.9.0`.
- `:ClewIndex` ignored a configured `cmd`. `binary.server_cmd` honoured it while
  `binary.index_cmd` resolved the binary independently, so overriding `cmd`
  either broke indexing outright ("clew binary not found" with a working server)
  or, worse, silently indexed with a different clew build than the one serving
  the index. A new `bin` option names the binary used for indexing and defaults
  to `cmd[1]`; `cmd` keeps its conventional meaning as the complete LSP server
  argv. `:ClewStatus` and `:checkhealth clew` report which source was used, so a
  wrapper `cmd` whose first element is not clew is visible rather than silent.
- `README.md` documented `:ClewUnits` and `:ClewLog` as though they existed.
  `lua/clew/commands.lua` registers three commands: `ClewIndex`, `ClewStatus`
  and `ClewRestart`. Both are now marked 🚧 with the `clew units` shell
  equivalent noted, and `:ClewStatus` describes what it actually prints rather
  than discovered units and symbol counts it has never reported.
- Configuration defaults in `README.md` had drifted from `lua/clew/config.lua`:
  `javascriptreact` was missing from `filetypes`, `node_modules/**` from
  `root.exclude`, and `root.markers` listed `.clew/index.scip` ahead of
  `.clew/config.toml`.
- `go.sum` is committed, so the repository builds from a clean checkout.
- Data race indexing more than one Maven unit. Units index concurrently and
  share one `runner`, whose `cachedJavacCP` and `cachedCLICP` fields were read
  and written from every goroutine without synchronisation. The same bug had
  two file-level counterparts: parallel `mvn dependency:build-classpath` runs
  writing one `classpath.txt`, and units overwriting the single
  `scip-java.args` argfile while another handed it to `java`. All three now go
  through `runner.memo`.

## 2026-07-24

### Added

- `clew` command-line tool with three subcommands: `index` (discover units,
  index each, merge into one index), `units` (list discoverable build roots and
  the indexer chosen for each), and `lsp` (serve the index on stdio).
- SCIP index merge that folds per-unit indexes into one: single normalised
  metadata, per-unit `relative_path` prefixing, document-scoped `local` symbols,
  and `external_symbols` deduplication.
- SCIP query layer handling all three occurrence range encodings
  (`single_line_range`, `multi_line_range`, and the deprecated packed `range`).
- Unit discovery by build root (`pom.xml`, `build.gradle`, `package.json` +
  `tsconfig`/`angular.json`), so single repositories, git superprojects and
  monorepos are all handled by one code path.
- Maven/Java indexing that runs without executing the project build and without
  modifying `pom.xml`, including the `javacopts.txt` step required for Maven
  coordinate stamping.
- TypeScript/JavaScript indexing via `scip-typescript`.
- stdio language server backed by a loaded SCIP index, serving
  `textDocument/definition`, `textDocument/references`,
  `textDocument/documentSymbol` and `workspace/symbol`, with no third-party LSP
  library.
- Neovim plugin (`lua/clew`, `plugin/clew.lua`) that registers the server via
  `vim.lsp.config`/`vim.lsp.enable`, with `:ClewIndex`, `:ClewStatus`,
  `:ClewRestart` and `:checkhealth clew`.
- Outermost-root detection, so a git superproject yields one server rather than
  one per submodule, with `.clew/config.toml` as an explicit override.
- Documentation: `README.md`, `doc/clew.txt` vim help, and `NOTES.md` recording
  what is verified versus unbuilt.
- Project logo and MIT `LICENSE`.
