# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Sections are headed by date (`YYYY-MM-DD`) rather than version number, newest first.

## 2026-07-25

### Added

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

### Fixed

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
