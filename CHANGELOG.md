# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Sections are headed by date (`YYYY-MM-DD`) rather than version number, newest first.

## 2026-07-25

### Added

- `doc/adr/`, holding architectural decision records in the Nygard format, and
  ADR 1: producer definitions are declared in clew's own TOML configuration
  rather than in Go or in the Neovim plugin's Lua, so a new language needs no
  clew release and `clew index` keeps working in CI and from other editors.
  Not implemented yet; the record is the decision, not the feature.
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
