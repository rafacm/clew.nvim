# Development notes

State of the scaffold, what is verified, and what is not. Research background:
`~/Develop/2026-07-24-treesitter-code-navigation-research.md` (§12 and §13 are the
hands-on validation this project is built on).

## Status

| Component | State |
| --- | --- |
| `README.md` | Written |
| `doc/clew.txt` | Written |
| Lua plugin (`lua/clew/`, `plugin/`) | **Loads clean; root detection verified** |
| `cmd/clew` CLI | Scaffolded, parses, not built |
| `internal/indexer` | Scaffolded, parses, not built |
| `internal/index` (merge + query) | Scaffolded, parses, not built |
| `internal/lsp` | Scaffolded, parses, not built |
| Tests | **None yet** |

## ⚠️ The Go build has never run

`go build ./...` could not be executed on the machine this was scaffolded on:
**every outbound connection from the Go toolchain fails with
`dial tcp …:443: connect: bad file descriptor`.**

Ruled out during diagnosis:
- not a sandbox restriction (bypassing it changed nothing)
- not a network block — `curl https://proxy.golang.org/...` returns HTTP 200 in 0.17s
- not fd exhaustion — `ulimit -n` is already 1048576

`github.com/sourcegraph/scip@v0.6.0` and `google.golang.org/protobuf@v1.36.8` are
present in the local module cache, but scip's own module graph requires
`cockroachdb/errors` → `grpc`, which cannot be fetched.

**First thing to do on a network-capable machine:**

```sh
cd ~/Develop/clew.nvim
go mod tidy
make
make vet
```

Expect compile errors — this code has been read, formatted and type-checked by eye,
never by the compiler.

## Verified

Everything below was confirmed empirically before this repo existed, against a real
umbrella (`~/Develop/scip-sandbox`: spring-petclinic + angular-realworld):

- **No build-file modification is needed.** `pom.xml` SHA is byte-identical before
  and after indexing. Maven runs only `dependency:build-classpath`.
- **`javacopts.txt` is mandatory.** Without it every own-symbol degrades to
  `scip-java maven . . …`. The documented `dependencies.txt` route did *not* work.
- **`scip-javac` is not zero-dependency** despite the docs — needs `scip-shared`,
  `scip-java-bindings`, `protobuf-java`.
- **Merging works.** 30 Java + 50 TS documents merged, 0 relative_path collisions,
  cross-file go-to-definition correct in both languages from the merged index.
- **Angular templates are not indexed.** 11 `.html` files on disk, 0 in the index.
  Component *members* are symbolized correctly.
- **`Document.language` is empty** from scip-typescript. Never branch on it.
- **Three range encodings** — `single_line_range`, `multi_line_range`, and the
  deprecated packed `range` which comes back empty. Handled in
  `internal/index/query.go:OccurrenceRange`.
- **Root detection** resolves `scip-sandbox/java/petclinic/src/main/java` to
  `scip-sandbox`, not the submodule. Verified via `nvim -l`.

## Next

1. `go mod tidy && make` on a network-capable machine; fix whatever the compiler says.
2. Tests, starting with the two that encode expensive lessons:
   - `TestMavenSymbolsCarryCoordinates` — assert a real `maven/<g>/<a> <version>`
     coordinate, so a missing `javacopts.txt` fails loudly instead of silently.
   - `TestOccurrenceRangeAllEncodings` — all three range shapes.
3. End-to-end: point `clew index` at `~/Develop/scip-sandbox` and diff the result
   against the known-good `umbrella.scip` produced by `build-index.sh`.
4. Wire `SymbolInformation.kind` into `documentSymbol` (currently hardcoded to 12).
5. Stale-buffer position mapping — the genuinely hard part. `uber/scip-lsp`'s
   `doc-sync/position_mapper.go` (MIT) is the reference.
6. Gradle unit support (`internal/indexer/run.go` currently errors on it).
7. Only then: astrocommunity PR, ~20 lines following the `nvim-java` entry.

## Open questions

- **Repo layout.** Go binary and Lua plugin currently share one repo, following
  `blink.cmp`'s precedent (`build = "make"`). Splitting into `clew` + `clew.nvim`
  is still an option if the build step proves annoying to distribute.
- **Binary distribution.** GoReleaser + GitHub releases, and/or a `mason-registry`
  entry so both vanilla and AstroNvim users get it without a toolchain.
- **`LICENSE` says "Rafael Cordones"** — inferred from the git email, not confirmed.
  The GitHub handle (`rafacm`) is confirmed.
