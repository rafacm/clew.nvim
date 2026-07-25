# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Sections are headed by date (`YYYY-MM-DD`) rather than version number, newest first.

## 2026-07-25

### Added

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
