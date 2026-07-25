# Working on clew

clew is two things in one repository: a Go binary (`cmd/clew`, `internal/`) that
builds and serves SCIP indexes, and a Neovim plugin (`lua/clew`, `plugin/`) that
drives it. The plugin is built on top of the binary and never the other way round —
`clew index` must keep working from a shell, in CI, and from other editors.

## Commands

```sh
make build   # go build -o bin/clew ./cmd/clew
make test    # go test ./...
make vet     # go vet ./...
make docs    # regenerate doc/tags so :help clew works
```

`go.sum` is committed and the module graph is deliberately small: the SCIP
bindings and protobuf. Think before adding a dependency.

## Invariants

These were established empirically, most of them the expensive way. Each one fails
*silently* if broken — that is why they are written down.

- **`javacopts.txt` is mandatory** for Maven units. Without it every symbol the
  unit defines degrades to `scip-java maven . . …` instead of carrying real
  coordinates. Navigation *inside* the unit still works, so the damage is invisible
  until indexes merge and every unit collapses into the same anonymous package. The
  documented `dependencies.txt` route does not produce coordinates. See
  `internal/indexer/java.go`.
- **Cross-unit resolution is a string join on symbol names**, which embed the
  Maven coordinate *including version*. Anything that changes how coordinates are
  stamped breaks navigation between units and nothing else.
- **`Document.language` is empty** from `scip-typescript` (`scip-java` sets
  `"java"`). Never branch on it.
- **Three occurrence range encodings exist** — `single_line_range`,
  `multi_line_range`, and the deprecated packed `range`, which comes back empty
  from modern writers. A consumer reading only `GetRange()` panics on real data.
  Handled in `internal/index/query.go:OccurrenceRange`.
- **`scip-javac` is not zero-dependency** despite its documentation. It needs
  `scip-shared`, `scip-java-bindings` and `protobuf-java`, and fails at plugin init
  with `NoClassDefFoundError` without them.
- **Every `Kind` needs a registered `Producer`.** Discovery stamps a `Kind` and
  dispatch looks it up; adding one without the other produces a unit that discovers
  cleanly and fails at index time.
- **Units index concurrently and share one `runner`.** Anything cached across units
  goes through `runner.memo`, never a struct field. Three races were fixed this way
  and the pattern exists to stop a fourth.
- **No build file is ever modified.** Maven runs only `dependency:build-classpath`;
  `pom.xml` is byte-identical before and after indexing.
- **Angular templates are not indexed.** `scip-typescript` reads `.ts` and ignores
  `.html`. Component members are symbolized, but nothing references them from a
  template.

## Conventions

**Decisions live in `doc/adr/`**, in the Nygard format. They record what was
decided and why, including alternatives that were rejected. Read them before
proposing a different design; do not re-litigate a decision without saying you are
reopening it.

**Three documentation surfaces must stay in sync**, and they have drifted before:

| File | Audience |
| --- | --- |
| `README.md` | The pitch, and the GitHub landing page |
| `doc/README.md` | Architecture rationale, prior art, decision index |
| `doc/clew.txt` | Vim help — the only one `:help clew` can read |

`doc/clew.txt` is not redundant with the markdown. Neovim's help system indexes
`doc/*.txt` only, so it is what makes `:help clew` and `<C-]>` tag-jumping work
offline. When configuration defaults, commands or requirements change, check all
three plus `lua/clew/config.lua`. A previous drift documented two commands that did
not exist.

**`CHANGELOG.md`** follows Keep a Changelog, headed by date rather than version,
newest first. Record decisions as decisions — if an ADR lands but nothing is
implemented, say so.

**Platforms:** macOS, Linux, and Windows via WSL. Producers are shell commands
assuming a POSIX shell, so native Windows is out.

## Testing

The strategy is [ADR 1](doc/adr/0001-testing-strategy.md): three tiers, hermetic by
default. Unit and producer-contract tests need no network and no toolchain;
acceptance tests download real projects at pinned commits and sit behind a build
tag, so `go test ./...` never reaches the network. Lua tests use `plenary.nvim`.
Tests are named for the project layout they exercise.

Not implemented yet. This is the current focus.

## Known gaps

- **Stale-buffer position mapping.** The index reports positions as of the last
  index, so `gd` drifts after edits. `staleness_check` reports index age; it does
  not fix positions. This is the product risk, not a rough edge.
- **Multi-module Maven fails.** `indexMaven` reads only `<unit>/src/main/java`
  while `Discover` refuses to descend, on the stated grounds that scip-java handles
  multi-module builds — which `indexMaven` does not use.
- **Gradle** is detected and reports unimplemented.
- **`SymbolInformation.kind`** is hardcoded to 12 in `documentSymbol`.
- **No SCIP indexer exists** for HCL/Terraform or Kubernetes YAML.

## Open questions

- **Repo layout.** The Go binary and Lua plugin share one repository, following
  `blink.cmp`'s precedent (`build = "make"`). Splitting into `clew` + `clew.nvim`
  remains an option if distributing the build step proves annoying.
- **Binary distribution.** GoReleaser plus GitHub releases, and/or a
  `mason-registry` entry, so users get a binary without a Go toolchain.

## Local-only references

Not in the repository, and unavailable to anyone else:

- `~/Develop/scip-sandbox` — the original umbrella (spring-petclinic +
  angular-realworld) where merging and cross-file navigation were first confirmed.
