# 3. Yarn Plug'n'Play units are installed with the node-modules linker

- **Status:** Accepted
- **Date:** 2026-07-27

## Context

`scip-typescript` needs a unit's dependencies on disk before it can resolve an
import: it drives the TypeScript compiler API, which resolves through ordinary
Node module resolution, which means `node_modules`. clew therefore installs a
unit that has none, with the package manager the unit's lockfile names.

Yarn 2+ ("berry") breaks that assumption on purpose. Its default install mode is
**Plug'n'Play**: no `node_modules` at all, dependencies left zipped under
`.yarn/cache`, and resolution routed through a generated `.pnp.cjs` that only
processes yarn itself has bootstrapped can read. `nodeLinker` being absent from
`.yarnrc.yml` is not the absence of PnP — it *is* PnP, because that is the
default.

Against such a tree `scip-typescript` resolves nothing, and reports nothing.
Measured on a two-file fixture depending on `date-fns@4.1.0`, indexed by clew
with yarn 4.5.0, against the identical project built from the identical lockfile
with `nodeLinker: node-modules` as the control:

| | PnP | control |
| --- | --- | --- |
| exit status | 0 | 0 |
| occurrences in `src/index.ts` | 18 | 19 |
| the project's own `tomorrow()` | present | present |
| TypeScript's built-in `Date#` | present | present |
| `npm date-fns 4.1.0 …/addDays().` | **absent** | present |

The project's own symbols resolve, and so do TypeScript's bundled lib types,
because neither needs `node_modules`. The one occurrence that does is simply not
in the index. `gd` on that import does nothing, and nothing anywhere says why.

This is the failure mode this repository is organised around: it is not that
indexing fails, it is that indexing *succeeds* and the result is quietly
incomplete. A partial index reported as a whole one is worse than no index,
because navigation appears to work right up to the symbol that came from a
dependency.

There is a second, smaller defect with the same root. `node_modules` missing is
what makes clew decide to install, so under PnP that condition is permanently
true and a full install runs on every single invocation, forever.

## Decision

**Every yarn berry unit is installed with `YARN_NODE_LINKER=node-modules` set on
clew's install alone.** The lockfile flavour already distinguishes berry from
classic (`yarn lockfile v1` is classic's banner), and `--immutable` already
freezes the resolution, so this is one environment variable on a command clew was
already running. `internal/indexer/typescript.go:yarnPlan`.

It is an environment variable rather than a configuration edit specifically so
that the project's `.yarnrc.yml` and `yarn.lock` are untouched, which the
repository's "no build file is ever modified" invariant requires. Both are
asserted byte-identical before and after indexing by
`TestAcceptance_SingleRepository_YarnPnP`.

**The override is unconditional for berry rather than gated on reading
`nodeLinker`.** Absence of the key means PnP, so detection would have to assume
PnP for the unset case anyway — leaving it to decide only between the two modes
where the override changes nothing that matters. A `nodeLinker: node-modules`
project was already getting exactly this tree; a `nodeLinker: pnpm` one gets the
same packages in a flatter layout. In every case the versions come from the
frozen lockfile.

**An install that produces no `node_modules` is now reported.** Whatever else may
one day install without one, it will say so rather than shipping a
plausible-looking index with every external symbol missing.

## Alternatives rejected

**Run `scip-typescript` under yarn's own PnP loader.** This is the honest fix:
nothing is materialised, the project is left exactly as its authors configured
it, and clew resolves dependencies the way the project itself does. It is not
available today. `scip-typescript` drives the TypeScript compiler API directly,
and making that resolve through PnP means yarn's TypeScript SDK and the
`pnpapi` hooks inside a tool clew does not own — an upstream piece of work, not a
flag clew can pass.

**This is the alternative that retires the decision above.** If `scip-typescript`
gains PnP-aware resolution, or yarn's SDK becomes drivable from outside a
`yarn exec` context, the override should go: it exists only because the honest
route is closed.

**Write `nodeLinker: node-modules` into the project's `.yarnrc.yml`.** Achieves
the same install and violates the invariant outright. A tool that edits the
configuration of the repository it is reading has no defensible stopping point.

**Refuse to index a PnP unit, with a clear error.** Honest, and it does convert a
silent failure into a loud one, which is most of the value. Rejected because it
leaves the user with no index at all where a complete one is one environment
variable away, and because clew's users did not choose their employer's linker.

**Detect PnP by parsing `.yarnrc.yml`.** Adds a YAML dependency to a deliberately
small module graph — or, worse, a hand-rolled parse of a format with three ways
to quote a string — to distinguish cases that get the same treatment anyway.

## Consequences

**A `node_modules` appears in a repository that deliberately has none.** This is
the real cost, and it is not hidden: clew logs the override and the reason on the
install line. Disk is the least of it — the directory is untracked, may not be in
a `.gitignore` that never needed one, and is exactly what the project's authors
opted out of.

**Yarn also removes the `.pnp.cjs` it is replacing.** A file disappears from the
user's working tree, and a "zero-install" repository commits that file alongside
`.yarn/cache`, so there it is a tracked deletion rather than a generated one.
What makes this acceptable rather than destructive is that a single
`yarn install` restores Plug'n'Play *and* removes clew's `node_modules`. clew
says so in the log at the moment it does it, and
`TestAcceptance_SingleRepository_YarnPnP/PlugNPlayIsRestoredByTheProjectsOwnInstall`
asserts the recovery, so the documented escape is a tested claim rather than a
hopeful sentence.

**Indexing a PnP project is slower the first time and normal afterwards.** The
override materialises a real dependency tree, which is the point; the second run
installs nothing, because now there is a `node_modules` for the check to find.

**`packageManager` in `package.json` is still not read, and this decision does
not change that.** clew runs whichever `yarn` is on `$PATH`, so a project pinning
yarn 4 with yarn 1 installed still fails — loudly, with yarn's own message naming
corepack. The tier 3 fixture reaches berry through a corepack shim on `$PATH`
for exactly that reason. That gap is issue #8's other half and is unresolved.
