# Working on clew

clew is two things in one repository: a Go binary (`cmd/clew`, `internal/`) that
builds and serves SCIP indexes, and a Neovim plugin (`lua/clew`, `plugin/`) that
drives it. The plugin is built on top of the binary and never the other way round —
`clew index` must keep working from a shell, in CI, and from other editors.

## Commands

```sh
make build            # go build -o bin/clew ./cmd/clew
make test             # tiers 1 and 2: hermetic, no network, no toolchain
make test-go          # go test -race ./...
make test-lua         # plenary, headless nvim
make test-acceptance  # tier 3: real projects, real indexers, network required
make vet              # go vet ./...
make docs             # regenerate doc/tags so :help clew works
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
- **The project root is realpath'd before discovery walks it**, in
  `indexer.resolveRoot`. `scip-java` bounds its search for a unit's `pom.xml` by a
  *realpath'd* sourceroot, so a symlink surviving into `Unit.Dir` — a symlinked
  workspace, anything under `/tmp` on macOS — makes the search escape that bound
  and degrades every symbol exactly as a missing `javacopts.txt` does. Resolving
  the root is enough for every unit because `filepath.WalkDir` does not follow
  symlinks; it is also the only reason `--root` may name one at all. Asserted by
  `TestDiscover_SymlinkInTheRootPathIsResolved` and
  `TestAcceptance_SingleRepository_MavenViaSymlink`.
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
- **A unit is installed with its own package manager, never npm by default.** The
  lockfile decides — `pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, `package-lock.json`
  — and a project whose manager is missing from `$PATH` fails rather than falling
  back. Reaching for npm regardless either fails outright (immer's peer graph) or,
  worse, succeeds against a dependency tree the project has never used;
  angular-realworld is bun-managed and was indexed by npm for months without
  anything noticing. See `internal/indexer/typescript.go:planInstall`.
- **No build file is ever modified**, and a lockfile is a build file. Maven runs
  only `dependency:build-classpath`, `pom.xml` is byte-identical before and after
  indexing, and every JS install is frozen. The single exception is deliberate:
  `npm ci` refuses a `package-lock.json` that has drifted from `package.json`, so
  `installDependencies` falls back to `npm install` — which may rewrite it — and
  logs that it is doing so.
- **Angular templates are not indexed.** `scip-typescript` reads `.ts` and ignores
  `.html`. Component members are symbolized, but nothing references them from a
  template.
- **`acceptance.yml`'s `paths:` filter is a hand-maintained copy of the project
  layout.** Adding a top-level Go package without adding it there drops that
  package out of tier 3 coverage. Nothing fails: the job simply never runs, and
  a green pull request means less than it did. Update the filter in the same
  commit that adds the directory. See *Testing*, below.

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

Tiers 1 and 3 are implemented. **Tier 2 is not**, because a fakeable producer is
a producer that can be *declared*, and that is ADR 2.

- **Tier 1** lives beside the code it tests: `internal/indexer`,
  `internal/index`, `internal/lsp`, and `tests/*_spec.lua`. Indexes are built
  programmatically with the Go bindings, so no `.scip` blob is committed.
- **Tier 3** lives in `internal/acceptance`, behind `//go:build acceptance`.
  Fixtures are downloaded per the pinned-commit table in ADR 1 and cached under
  `$XDG_CACHE_HOME/clew-test/<sha>/`; working copies are fresh per run.

Two things to know before writing an acceptance test:

- **Assert on properties, not bytes.** SCIP output moves with indexer versions
  and `ScipTypeScriptPackage` is pinned to `@latest`, so any golden file rots.
- **Realpath your fixture root.** `tempRoot` hands back a `t.TempDir()` with its
  own symlinks resolved, so that a test about paths measures clew rather than
  macOS's `/var` symlink — and so that a test *about* symlinks measures only the
  one it created.

### CI

Linux and macOS for both workflows.

| Workflow | Runs on | Covers |
| --- | --- | --- |
| `test.yml` | every push and pull request | Tier 1, `gofmt`, `go vet` (also under the `acceptance` tag), stale `doc/tags` |
| `acceptance.yml` | daily at 04:00 UTC, plus pull requests touching indexing code | Tier 3 |

Two things about `acceptance.yml` are load-bearing and easy to undo by accident.

**It must not become a required check.** It depends on GitHub, Maven Central and
the npm registry, and `ScipTypeScriptPackage` is pinned to `@latest`, so it can
go red with no bad commit anywhere. A required check that fails for reasons the
author cannot fix trains people to merge past red CI, and then it protects
nothing. Advisory is the design, not an oversight — this is a branch-protection
setting on GitHub, so nothing in the repository can enforce it.

**Its `paths:` filter mirrors the project layout by hand,** and a directory
missing from it silently loses tier 3 coverage. Listed today: `internal/**`,
`cmd/**`, `go.mod`, `go.sum`, `Makefile`, and the workflow itself. Deliberately
absent: `lua/**`, `plugin/**`, `doc/**` and `tests/**`, none of which can change
what an indexer produces.

**The schedule is not merely a slower per-change run.** It is the only thing that
catches *upstream* drift: `@latest` moving underneath clew, or a pinned fixture
being renamed or made private. No pull-request trigger can see that, so the
schedule stays even though pull requests now run the same suite.

**Tier 3 runs in parallel, and three tests are deliberately serial.** No two
concurrent tests may download the same artifact: a Maven local repository is not
safe for concurrent writes of one artifact across processes, and a package
manager's cache is no better. Go runs sequential tests before resuming parallel
ones, so `SingleRepository_Maven`, `SingleRepository_MavenLarge` and
`SingleRepository_Angular` act as barriers that warm `~/.m2` and bun's cache for
the batch that follows. Adding a fixture means checking it against that rule —
the full argument is at the top of `internal/acceptance/acceptance_test.go`. A
flake here is worse than a slow job, because this check is advisory and a check
people learn to ignore protects nothing.

**Every download the suite repeats is cached in CI**, and the list is not
self-maintaining: fixtures by commit SHA, `~/.m2`, and the npm, yarn and bun
download caches. The last of these was missing until immer's yarn install — 43
seconds, the slowest test in the suite — made the omission obvious.

**The package managers' cache directories are pinned in `acceptance.yml`'s `env:`,
not left to their defaults.** `XDG_CACHE_HOME` is already overridden there so the
fixture cache has one path on both runners, and yarn classic honours it too: on
Linux its cache followed into the workspace, outside the cached path, and was
discarded on every run. Nothing failed — the install simply never got faster,
which is only visible if someone is timing it. `YARN_CACHE_FOLDER`,
`BUN_INSTALL_CACHE_DIR` and `npm_config_cache` now name the paths the cache step
stores, so the two cannot drift apart.

**The suite needs whatever package manager its fixtures declare.** clew installs a
TypeScript unit with the manager named by its lockfile, so immer needs `yarn` and
angular-realworld needs `bun`; `acceptance.yml` installs both with
`npm install --global`. A fixture that switches lockfiles upstream changes which
binaries tier 3 requires, and both tests fail with that message rather than a
resolver error.

**`CLEW_TEST_REQUIRE_TOOLS` turns a missing toolchain into a failure,** and
`acceptance.yml` sets it. Locally, `requireTools` skips — a laptop without a JDK
is not a clew regression. In CI the same skip would mean a broken setup step
reports a green tick having verified nothing, so there it must be fatal.

Also note `acceptance.yml` deliberately does **not** use `setup-java`'s
`cache: maven`. That option keys on `hashFiles('**/pom.xml')` and fails the job
outright when nothing matches, and clew has no `pom.xml` of its own — every pom
the suite touches belongs to a fixture and is downloaded at test time. `~/.m2`
is cached explicitly instead.

WSL is claimed in `README.md` and is not verified by CI.

## Known gaps

- **The lockfile is the only package-manager signal clew reads.** `packageManager`
  in `package.json` — corepack's authoritative answer — is not consulted, so clew
  runs whichever `yarn` is on `$PATH` no matter which major the project pins, and
  a repository carrying two lockfiles is resolved by the precedence in
  `planInstall` rather than by what it declares. Every fixture agrees with its
  lockfile so far. This one at least fails *loudly*: yarn classic refuses a
  project pinning yarn 4 and names corepack in the error. See issue #8.
- **Yarn PnP indexes silently, without its dependencies** (issue #8). `nodeLinker:
  pnp` installs no `node_modules`, so `scip-typescript` resolves no import that
  needs one — and reports nothing, leaving an index that looks complete and has
  lost every external symbol. The install also repeats on every invocation, since
  the thing clew checks for is `node_modules` itself. Confirmed against yarn 4.5.0
  with a `nodeLinker: node-modules` control built from the same lockfile: the
  dependency's symbol is in one index and absent from the other.
  `YARN_NODE_LINKER=node-modules` on the install closes it without touching the
  project's config, and is not applied yet.
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
