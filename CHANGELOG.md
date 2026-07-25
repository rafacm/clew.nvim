# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Sections are headed by date (`YYYY-MM-DD`) rather than version number, newest first.

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
