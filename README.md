<div align="center">

# clew.nvim 🧶

**clew** *(n.)* a ball of thread; the origin of the word *clue*. [Ariadne's](https://en.wikipedia.org/wiki/Ariadne#Minos_and_Theseus) thread for your codebase.

Go-to-definition and find-references served from a **precomputed [SCIP](https://scip-code.org) index**, with **no language server running inside your editor**.

Works with **any language that has a [SCIP indexer](https://scip-code.org)**, across **single repositories**, **git superprojects** (submodules) and **monorepos** alike.

[Features](#features) • [Why](#why-a-precomputed-index) • [Getting Started](#getting-started) • [Usage](#usage) • [Commands](#commands) • [Integration](#integration-with-neovim) • [Related Projects](#related-projects) • [Credits](#credits)

<img src="assets/clew-nvim.png" alt="clew.nvim logo" width="50%">

</div>

> [!WARNING]
> **Early development.** This README describes the intended v1. Sections marked 🚧 are not implemented yet. The indexing pipeline it is built on has been validated end-to-end (Java + Angular superproject, cross-file go-to-definition working); the plugin around it is new.

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
| **Git superproject** | One unit per submodule, merged into one index. Navigation crosses submodule boundaries. |
| **Monorepo** | One unit per build root, merged the same way. Submodules are not required. |

> [!NOTE]
> **On terminology:** *superproject* is [git's own term](https://git-scm.com/docs/gitsubmodules) for a repository that contains submodules, so this README prefers it over the informal "umbrella project". The distinction matters less than it sounds: to clew, a superproject and a monorepo are both just several units under one root.

## Why a precomputed index?

The usual framing is *"tree-sitter vs. LSP"*, as if semantic analysis were what makes Java tooling heavy. That framing is wrong, and it sends you off building a worse ctags.

The real axis is **resident semantic server vs. precomputed static index.**

`jdtls` is painful because it runs Eclipse JDT Core inside a workspace runtime, resident in your editor: slow to start, memory-hungry, duplicated across projects. None of that is inherent to semantic analysis. It is inherent to *keeping the analyzer alive next to you.*

Move the identical work out-of-band and the cost profile changes completely:

|                        | Resident server (jdtls, jls) | Precomputed index (clew) |
| ---------------------- | ---------------------------- | ------------------------ |
| Editor-resident memory | 0.5 to 2 GB **per project root** | one small Go process |
| Startup                | seconds, every session       | instant; the index already exists |
| 50 submodules          | up to 50 JVMs                | **one** index, one process |
| Cross-submodule nav    | ❌ servers don't share state | ✅ symbols federate |
| Correctness            | ✅ compiler-grade            | ✅ compiler-grade |
| Freshness              | live                         | as of last index |

That last row is the whole trade. For **navigation**, staleness is nearly free. It is the same deal you already accept with `ctags`/`gutentags`, except the index is built by a real compiler instead of a regex.

### Why SCIP specifically

[SCIP](https://scip-code.org) is a language-agnostic code-intelligence index format, governed in the neutral [`scip-code`](https://github.com/scip-code) org and consumed by Sourcegraph, Mozilla Searchfox, rust-analyzer and Glean.

Two properties make it the right substrate:

1. **Symbols are globally-scoped strings.** For example `scip-java maven maven/org.example/svc 1.2.0 com/example/Foo#`, encoding manager, package, version and descriptor. There are no index-local integer IDs. That means two *separately produced* indexes can be merged and cross-resolved by plain string matching, so federating across submodules is a string join.
2. **It is a format, not a tool.** Adding a language means writing a producer. The editor side never changes.

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

## Related Projects

Prior art, and honestly why each one did not do what we needed.

- **[eclipse-jdtls](https://github.com/eclipse-jdtls/eclipse.jdt.ls)** and [`nvim-jdtls`](https://github.com/mfussenegger/nvim-jdtls): the reference Java LSP. Correct and full-featured, but it wraps Eclipse JDT Core inside a workspace runtime, so it is slow to start, memory-hungry, and runs one instance per project root. On a superproject with dozens of submodules that is untenable.
- **[idelice/jls](https://github.com/idelice/jls)** (a fork of [georgewfraser/java-language-server](https://github.com/georgewfraser/java-language-server)): a genuinely lighter Java LSP built on the **javac API** rather than JDT, tuned for Neovim, with Lombok and JAR navigation. **If you have a single Java project, use this instead of clew.** It did not fit here for one reason: Neovim starts one instance per project root, at `-Xmx2g` default each, and separate instances cannot navigate *between* submodules.
- **[uber/scip-lsp](https://github.com/uber/scip-lsp)**: the closest existing thing, a language-agnostic LSP server backed by SCIP. It is TCP-only, Bazel-built, ships no release binaries, and carries Uber-internal configuration. **None of its code is used in clew**, though its stale-buffer position mapping is the intended design reference for a feature clew has not built yet.
- **[github/stack-graphs](https://github.com/github/stack-graphs)**: compiler-free cross-file name resolution on tree-sitter, and conceptually the most elegant answer. **Archived** in September 2025 after its principal author left GitHub; the most credible industrial adopter subsequently reverted to a compiler-based analyzer. The technique is sound but specialist and unmaintained.
- **[universal-ctags](https://github.com/universal-ctags/ctags)**, [`gutentags`](https://github.com/ludovicchabant/vim-gutentags) and [`ctags-lsp`](https://github.com/netmute/ctags-lsp): the original precomputed-index idea, and the workflow model clew copies. Too low-level, with no type resolution, no cross-file semantics, and ambiguous results on any codebase with common method names.
- **[jacktasia/dumb-jump](https://github.com/jacktasia/dumb-jump)** (Emacs): language-aware regex search with zero index and zero setup, covering 60+ languages including HCL. Excellent, and the right answer when you want *no* infrastructure. Too imprecise for Java specifically, where overloading and common method names defeat regex.
- **[Sourcegraph](https://sourcegraph.com)**: where SCIP came from. A full platform whose main repository is no longer publicly available, and running it to serve one developer's editor is wildly disproportionate.

### What is not covered yet

- **Angular templates.** `scip-typescript` indexes `.ts` and ignores `.html` entirely. Component *members* are symbolized correctly, but nothing in a template references them. A dedicated `scip-angular` producer would close this and is the most valuable thing to build next.
- **Infrastructure code.** No SCIP indexer exists for HCL/Terraform or Kubernetes YAML.

## Credits

clew is mostly *assembly*. The hard parts belong to other people.

- **[SCIP](https://github.com/scip-code/scip)**: the Code Intelligence Protocol, and the globally-scoped symbol design that makes cross-repository federation a string match. Originally by [Sourcegraph](https://sourcegraph.com), now stewarded by the neutral [`scip-code`](https://github.com/scip-code) organisation.
- **[scip-java](https://github.com/scip-code/scip-java)**: Java and Kotlin indexing, and the `scip-javac` compiler plugin that makes build-free indexing possible at all.
- **[scip-typescript](https://github.com/sourcegraph/scip-typescript)**: TypeScript and JavaScript indexing.
- **[uber/scip-lsp](https://github.com/uber/scip-lsp)** (MIT): the closest existing SCIP-backed language server. **None of its code is used here.** It is the intended design reference for stale-buffer position mapping, which clew does not yet implement.
- **[netmute/ctags-lsp](https://github.com/netmute/ctags-lsp)**: the architectural model, an external indexer behind a plain LSP server, so every editor's native client just works.
- **[parrot.nvim](https://github.com/frankroeder/parrot.nvim)**: the shape and tone of this README.
- **[AstroNvim](https://github.com/AstroNvim/AstroNvim)** and [astrocommunity](https://github.com/AstroNvim/astrocommunity).

## License

MIT
