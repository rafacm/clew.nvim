# 2. Producers are declared in clew's own configuration

- **Status:** Accepted
- **Date:** 2026-07-25

## Context

clew drives external SCIP indexers. Two are wired in today, `scip-java` and
`scip-typescript`, each as a Go `Producer` in `internal/indexer`. Python is next,
and after that the list is open-ended: there are indexers for Rust, Go, C#, Ruby,
C/C++ and more.

The two halves of clew do not share this problem.

**Consuming an index is already language-agnostic and needs no change ever.**
`clew lsp` derives its path from the LSP `rootUri` and loads whatever sits at
`.clew/index.scip` (`internal/lsp/server.go:112`). Nothing in the load, query or
serve path branches on language. A user can index a Rust project with any tool
they like, drop the result at that path, and `gd`/`gr` work on a language clew has
never heard of.

**Driving an indexer cannot be inferred.** Someone has to know that
`scip-typescript` is npm-distributed and wants `node_modules` present, that
`scip-python` resolves imports from an installed environment, that Maven needs a
four-step `javac` pipeline with a `javacopts.txt` that is easy to miss and
expensive to debug. That knowledge has to live somewhere, and today it lives in Go
— so every new language costs a release, which puts clew's language coverage on
clew's release cadence rather than on the user's needs.

An earlier proposal put this knowledge in the Neovim plugin's Lua configuration
and passed it to the binary on the command line. That was rejected: it would break
indexing in CI, which `README.md` explicitly offers; it would make `clew index`
and `clew units` unusable standalone; and it would make clew a Neovim tool that
happens to speak LSP, when the model it deliberately copies from `ctags-lsp` is an
external indexer behind a plain stdio server that every editor's native client can
drive. The plugin is built on top of the binary, never the other way round.

The producer registry (commit `2a7378f`) made a second implementation of the
`Producer` interface cheap, which is what makes this decision small rather than a
rewrite.

## Decision

**Producer definitions live in clew's own configuration file, read by the Go
binary.**

- **Format: TOML**, parsed with `github.com/BurntSushi/toml`.
- **Discovery:** `$XDG_CONFIG_HOME/clew/producers.toml`, falling back to
  `~/.config/clew/producers.toml`. `--config PATH` overrides both.
- **A `configProducer` implements `Producer`** and is appended to the registry at
  startup, so discovery and dispatch are unchanged.
- **Config producers are consulted before the built-in Go producers**, so a user
  can override how a directory is handled rather than only add to it. Within the
  config file, order is precedence, matching the registry's existing rule.
- **The contract with a command** is environment variables in, a SCIP index out:
  clew passes `$CLEW_UNIT_DIR`, `$CLEW_OUTPUT` and `$CLEW_PREFIX`, and expects a
  valid index written to `$CLEW_OUTPUT`.
- **Unknown keys are an error**, via `MetaData.Undecoded()`, so `detect_files`
  instead of `detect` fails loudly rather than producing a producer that silently
  never matches.

```toml
# Order is detection precedence: first match wins.

[[producer]]
kind    = "python"
detect  = ["pyproject.toml", "setup.py"]
# scip-python resolves imports from the installed environment,
# so the unit's virtualenv must be active or on PATH.
command = "npx --yes @sourcegraph/scip-python index --output $CLEW_OUTPUT"
```

**Multi-step pipelines stay in Go.** Maven is resolve-classpath → javac-with-plugin
→ write `javacopts.txt` → aggregate, with data flowing between steps; that does not
fit a command template and should not be forced into one. Configuration covers the
single-command class, which is most of what exists. Anything more complex points
`command` at a user script.

**Repo-local `.clew/producers.toml` is not honoured.** See below.

## Consequences

### What this buys

- Adding a language needs no clew release. Shipping an example per language is the
  whole delivery mechanism.
- `clew index` and `clew units` stay usable standalone, so CI indexing keeps
  working and clew stays editor-agnostic.
- The Neovim plugin stays thin: it passes `--config` or nothing, and serializes
  nothing.
- `Producer` gains a second adapter, which is what makes it a real seam rather than
  a hypothetical one.

### What it costs

- One new dependency. `BurntSushi/toml` declares zero requirements, which matters
  because the dependency graph was just reduced to a single indirect requirement.
- Two ways to add a language. The documentation has to say plainly which to reach
  for: configuration first, Go only when the pipeline has steps.
- TOML is not the Go ecosystem norm — YAML is. It was chosen anyway because every
  producer entry holds a shell command, and YAML is whitespace-significant and
  type-coercing, which makes command strings its worst case. JSON was rejected for
  having no comments, and the per-language examples need caveats sitting next to
  the line they apply to.

### Deferred, deliberately

- **Repo-local configuration needs a trust model.** Honouring
  `.clew/producers.toml` from a working tree means cloning a repository and running
  `:ClewIndex` executes commands that repository chose. That is a real hazard for a
  tool whose entire purpose is pointing at unfamiliar code. If it is added, it needs
  explicit per-project opt-in on the model of Neovim's `exrc` trust or direnv's
  `allow`. Retrofitting a trust prompt onto a feature people already depend on is
  much harder than designing it in, which is why the constraint is recorded now
  rather than discovered later.
- **Mixing an external index into the merge.** Bring-your-own currently means
  replacing `.clew/index.scip` wholesale; there is no way to have clew handle the
  Java and TypeScript units and fold in an externally-produced Python index. An
  `--extra-index PATH` joining the merge as an additional `index.Input` would close
  this, and is small, but is out of scope here.
