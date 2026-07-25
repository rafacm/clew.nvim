# clew.nvim Detailed Documentation

← Back to the [main README](../README.md).

Longer-form material that does not belong in the main README: the reasoning behind the architecture, the prior art it was weighed against, and the projects it is assembled from.

## Contents

- [Why a precomputed index?](#why-a-precomputed-index)
  - [Why SCIP specifically](#why-scip-specifically)
- [Related Projects](#related-projects)
  - [What is not covered yet](#what-is-not-covered-yet)
- [Credits](#credits)

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
- **[parrot.nvim](https://github.com/frankroeder/parrot.nvim)**: the shape and tone of the clew.nvim README and an inspiration for the logo.
- **[AstroNvim](https://github.com/AstroNvim/AstroNvim)** and [astrocommunity](https://github.com/AstroNvim/astrocommunity).
