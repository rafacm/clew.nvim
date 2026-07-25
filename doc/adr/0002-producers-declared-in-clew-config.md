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

**A producer is a TOML declaration. Go producers are a documented exception, not a
parallel mechanism.**

Configuration is the only way to define a producer. The producers clew ships with
are embedded in the binary with `go:embed` as ordinary TOML, loaded into the same
registry as anything a user writes. "Built-in" therefore means *a default we
ship*, not *a second code path*.

- **Format: TOML**, parsed with `github.com/BurntSushi/toml`.
- **Discovery:** embedded defaults first, then
  `$XDG_CONFIG_HOME/clew/producers.toml`, falling back to
  `~/.config/clew/producers.toml`. `--config PATH` overrides the user file.
- **User declarations shadow embedded defaults by `kind`.** Redefining `typescript`
  replaces the shipped one rather than competing with it, so overriding needs no
  precedence rule beyond "yours wins." Within a file, order is detection
  precedence, matching the registry's existing rule.
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

### When a Go producer is justified

A producer stays in Go only if it needs one of the following. If it needs none of
them, it is a TOML declaration, and an existing Go producer that needs none should
be moved out.

1. **Cross-unit state.** Work shared across concurrently indexed units, through
   `runner.memo`. A per-unit command cannot share anything in-process.
2. **Per-step error policy.** A step whose failure is deliberately tolerated
   alongside one whose failure is fatal.
3. **Generated files.** A file the indexer requires whose exact format clew must
   produce.

**`scip-typescript` meets none of these and moves to an embedded declaration.** It
is one command plus a conditional `npm install`, which a shell command expresses
directly.

**`scip-java` meets all three, and stays:**

1. `scipJavacClasspath` resolves the compiler plugin once per run and shares it
   across every Maven unit. Per-unit commands would each resolve it, and would race
   on the same `classpath.txt` on first run — the bug fixed in `2a7378f`,
   reintroduced, with `flock` as shell's answer.
2. `javac` runs with its exit code deliberately ignored, because the SCIP plugin
   still emits shards for whatever compiled and a partial index beats none. The
   `aggregate` step that follows must succeed.
3. `writeJavacOpts` emits `javacopts.txt` in an exact format — `-version`, then
   every option and source file individually `%q`-quoted, one per line. Omitting it
   silently degrades every symbol in the unit to an anonymous package.

Extending the schema to cover those three would mean multi-step declarations with
data flowing between steps, per-step error policy, and templated file generation.
That is a build DSL, and building one is a worse project than clew.

**Repo-local `.clew/producers.toml` is not honoured.** See below.

## Consequences

### What this buys

- Adding a language needs no clew release. Shipping an example per language is the
  whole delivery mechanism.
- One mechanism rather than two. The shipped producers exercise exactly the path
  users write against, so the declaration format cannot quietly become
  second-class.
- The Go producer list shrinks over time instead of growing. After this it is
  `scip-java` alone, with `scip-gradle` joining it when written, and the three
  criteria above decide anything new.
- `clew index` and `clew units` stay usable standalone, so CI indexing keeps
  working and clew stays editor-agnostic.
- The Neovim plugin stays thin: it passes `--config` or nothing, and serializes
  nothing.
- `Producer` gains a second adapter, which is what makes it a real seam rather than
  a hypothetical one.

### What it costs

- One new dependency. `BurntSushi/toml` declares zero requirements, which matters
  because the dependency graph was just reduced to a single indirect requirement.
- Shipped producers become shell strings, so they are platform-specific in a way Go
  code was not. Windows support for an embedded default is now a property of the
  declaration rather than of the binary.
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
