<div align="center">

<img src="assets/clew-nvim.png" alt="clew.nvim logo" width="50%">

# clew.nvim 🧶

**clew** *(n.)* a ball of thread; the origin of the word *clue*.<br>
[Ariadne's](https://en.wikipedia.org/wiki/Ariadne#Minos_and_Theseus) thread for your codebase.

[What is it?](#what-is-clewnvim) • [Features](#features) • [Getting Started](#getting-started) • [Usage](#usage) • [Commands](#commands) • [Integration](#integration-with-neovim) • [Background](#background) • [Changelog](#changelog)

</div>

> [!WARNING]
> **Early development.** This README describes the intended v1. Sections marked 🚧 are not implemented yet. The indexing pipeline it is built on has been validated end-to-end (Java + Angular superproject, cross-file go-to-definition working); the plugin around it is new.

## What is clew.nvim?

A Neovim plugin that gives you go-to-definition and find-references served from a **precomputed [SCIP](https://scip-code.org) index**, with **no language server running inside your editor**. It works with **any language that has a [SCIP indexer](https://scip-code.org)**, across **single repositories**, **git superprojects** (submodules) and **monorepos** alike, and the answers are compiler-grade rather than heuristic, because the index was built by a real compiler.

## Features

`clew.nvim` inverts the usual trade-off. Instead of running a semantic engine *inside* your editor, it runs the semantic engine **out-of-band**, on save, on build, or in CI, and leaves your editor querying a static index.

You keep compiler-grade correctness. You give up nothing but freshness.

- **No resident language server.** No JVM in your editor session. Indexing happens when *you* ask, not continuously in the background.
- **One index for the whole project**, including **git superprojects**. Fifty submodules produce one index and one server process, not fifty.
- **Navigate *between* submodules.** Separate language servers each know only their own repo; a merged SCIP index resolves across them, because SCIP symbols are globally-scoped strings.
- **Language-agnostic by construction.** Anything with a SCIP indexer lands in the same index with the same query path. Adding a language means adding an *indexer*, never touching the editor side.
- **Compiler-grade, not heuristic.** Real overload resolution, real generics, real classpath awareness, including versioned symbols for third-party jars and the JDK itself.
- **Native Neovim integration.** `clew` speaks LSP over stdio, so `gd`, `gr`, `<C-]>`, Telescope, fzf-lua and aerial.nvim all work with no extra glue.
- **No build modification required.** Your `pom.xml` is never touched. Maven is invoked only for dependency *resolution*, never for a build.

### Project shapes

clew does not distinguish between project layouts. It discovers **units**, meaning build roots such as a `pom.xml`, a `build.gradle` or a `package.json`, and merges whatever it finds into one index. A single-project repository is simply the case where there is one unit.

| Shape | What clew does |
| --- | --- |
| **Single repository** | One unit, one index. No configuration needed. |
| **[Git superproject](https://git-scm.com/docs/gitsubmodules)** | One unit per submodule, merged into one index. Navigation crosses submodule boundaries. |
| **Monorepo** | One unit per build root, merged the same way. Submodules are not required. |

## Getting Started

### Dependencies

- [`neovim 0.11+`](https://github.com/neovim/neovim/releases), for `vim.lsp.config` and `vim.lsp.enable`
- The `clew` binary (see [Installation](#installation))

Per language, only for the languages you actually index:

| Language | Needs | Provided by |
| --- | --- | --- |
| Java / Kotlin | JDK 17+, Maven or Gradle | [`scip-java`](https://github.com/scip-code/scip-java) |
| TypeScript / JavaScript | Node.js 18+ | [`scip-typescript`](https://github.com/sourcegraph/scip-typescript) |

Other languages work as soon as clew learns to drive their indexer; see [the SCIP indexer list](https://scip-code.org) for what exists.

Optional, for nicer pickers:

- [`telescope.nvim`](https://github.com/nvim-telescope/telescope.nvim) or [`fzf-lua`](https://github.com/ibhagwan/fzf-lua)
- [`aerial.nvim`](https://github.com/stevearc/aerial.nvim)

### Installation

<details>
  <summary>lazy.nvim</summary>

```lua
{
  "rafacm/clew.nvim",
  build = "make",          -- builds the `clew` binary
  opts = {},
}
```

</details>

<details>
  <summary>Packer</summary>

```lua
require("packer").startup(function()
  use({
    "rafacm/clew.nvim",
    run = "make",
    config = function() require("clew").setup() end,
  })
end)
```

</details>

<details>
  <summary>Neovim native package</summary>

```sh
git clone --depth=1 https://github.com/rafacm/clew.nvim.git \
  "${XDG_DATA_HOME:-$HOME/.local/share}"/nvim/site/pack/clew/start/clew.nvim
cd "${XDG_DATA_HOME:-$HOME/.local/share}"/nvim/site/pack/clew/start/clew.nvim && make
```

</details>

### Setup

The defaults are intended to work with no configuration:

```lua
require("clew").setup()
```

Everything below is optional:

```lua
require("clew").setup({
  -- Where the index lives, relative to the project root.
  index_path = ".clew/index.scip",

  -- Filetypes clew attaches to.
  filetypes = { "java", "kotlin", "typescript", "typescriptreact", "javascript" },

  -- Root detection. clew always prefers the OUTERMOST match so a superproject
  -- yields ONE server, not one per submodule.
  root = {
    markers = { ".clew/index.scip", ".clew/config.toml", ".gitmodules", ".git" },
    -- Units to index. Empty = auto-discover build roots (pom.xml, build.gradle,
    -- package.json + angular.json, ...) beneath the project root.
    include = {},
    exclude = { "vendor/**", "third_party/**" },
  },

  -- Rebuild the index automatically. "never" | "save" | "manual"
  auto_index = "never",

  -- Warn when the index is older than the working tree.
  staleness_check = true,
})
```

## Usage

The workflow is deliberately explicit: **you decide when the index is built.**

1. Open any file inside your project.
2. Run `:ClewIndex` once. clew discovers indexable units, runs the right indexer for each, and merges the results into a single index at `.clew/index.scip`.
3. Navigate with the standard Neovim LSP mappings: `gd`, `gr`, `<C-]>`, `:Telescope lsp_references`.
4. Re-run `:ClewIndex` after a significant change. Individual units can be refreshed with `:ClewIndex <unit>`, which is much faster than a full rebuild.

Because the index is a plain file, it can equally be produced in CI and then committed or cached. The editor does not care who built it.

> [!NOTE]
> clew reports positions from the index, so after heavy edits results drift until you re-index. With `staleness_check` enabled, clew tells you when that has happened rather than letting you chase a phantom line number. 🚧

## Commands

| Command | Description |
| --- | --- |
| `:ClewIndex` | Build or rebuild the index for the whole project |
| `:ClewIndex {unit}` | Rebuild a single unit, for example one submodule |
| `:ClewStatus` | Show project root, discovered units, index age, symbol/document counts |
| `:ClewUnits` | List discovered indexable units and the indexer chosen for each |
| `:ClewRestart` | Restart the language server and reload the index |
| `:ClewLog` | Open the clew server log |
| `:checkhealth clew` | Verify binary, toolchains, root detection and index validity |

## Integration with Neovim

`clew` is an ordinary LSP server speaking stdio, so it plugs into everything Neovim already has. There is no clew-specific API to learn.

| What you want | How | Notes |
| --- | --- | --- |
| Go to definition | `vim.lsp.buf.definition()` / `gd` | Cross-file and cross-submodule |
| Find references | `vim.lsp.buf.references()` / `gr` | Whole-project, from the merged index |
| Tag jump | `<C-]>`, `:tag`, `<C-w>]` | Free via `vim.lsp.tagfunc`; the tag stack works normally |
| Document symbols | `vim.lsp.buf.document_symbol()` | Powers [aerial.nvim](https://github.com/stevearc/aerial.nvim) |
| Workspace symbols | `vim.lsp.buf.workspace_symbol()` | Fuzzy jump across every unit at once |
| Fuzzy pickers | `:Telescope lsp_references`, `fzf-lua lsp_definitions` | No extra configuration |
| Hover | `vim.lsp.buf.hover()` / `K` | Signature and documentation where the index carries it 🚧 |

**Deliberately not provided:** completion, diagnostics, formatting and rename. A static index is the wrong tool for those, and pretending otherwise would produce confidently wrong results. Keep a real language server attached if you need them. clew coexists with one happily, and is useful precisely when you would rather not run one.

## Background

See [`doc/README.md`](doc/README.md):

- [**Why a precomputed index?**](doc/README.md#why-a-precomputed-index), on resident semantic servers versus static indexes, and why SCIP is the right substrate.
- [**Related Projects**](doc/README.md#related-projects), the prior art clew was weighed against, and what is not covered yet.
- [**Credits**](doc/README.md#credits), the projects clew is assembled from.

## Changelog

Notable changes are recorded in [CHANGELOG.md](CHANGELOG.md), following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format with dated sections.

## License

Released under the [MIT License](LICENSE). Copyright (c) 2026 Rafael Cordones.
