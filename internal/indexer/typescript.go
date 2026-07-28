package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ScipTypeScriptPackage is the npm package clew invokes via npx.
const ScipTypeScriptPackage = "@sourcegraph/scip-typescript@latest"

// yarnNodeModulesLinker is the environment override that makes a yarn 2+ install
// materialise a node_modules instead of a Plug'n'Play tree. See yarnPlan.
const yarnNodeModulesLinker = "YARN_NODE_LINKER=node-modules"

// typeScriptProducer drives scip-typescript over a Node project.
type typeScriptProducer struct{}

func (typeScriptProducer) Kind() Kind { return KindTypeScript }

// Detect requires a tsconfig or angular.json alongside package.json. A bare
// package.json is not enough: plenty of repositories carry one purely for tooling
// (a linter, a git hook) with no TypeScript to index.
func (typeScriptProducer) Detect(dir string) (string, bool) {
	if !exists(filepath.Join(dir, "package.json")) {
		return "", false
	}
	if exists(filepath.Join(dir, "tsconfig.json")) || exists(filepath.Join(dir, "angular.json")) {
		return "package.json", true
	}
	return "", false
}

// Index indexes a TypeScript/JavaScript unit.
//
// Caveats worth knowing, both verified against a real Angular 21 app:
//
//   - Angular templates are NOT indexed. scip-typescript reads .ts and ignores
//     .html entirely. Component class members ARE symbolized correctly, so the
//     definitions exist -- what is missing is references *from* templates. A
//     dedicated scip-angular producer would close this gap without any change to
//     the merge or the server.
//
//   - Document.language is left EMPTY by scip-typescript (scip-java sets "java").
//     Nothing downstream may branch on it.
func (typeScriptProducer) Index(ctx context.Context, r *runner, u Unit) (string, error) {
	if _, err := os.Stat(filepath.Join(u.Dir, "node_modules")); os.IsNotExist(err) {
		if err := installDependencies(ctx, r, u); err != nil {
			return "", err
		}
		// node_modules is both the question and the answer here: it is what
		// decides whether to install, so an install that does not create one
		// repeats on every single invocation AND leaves scip-typescript resolving
		// nothing. Yarn Plug'n'Play did exactly that (issue #8), silently, and
		// planInstall now overrides the linker to stop it. Anything else that
		// manages to install without a node_modules -- a linker mode nobody has
		// hit yet -- gets said out loud rather than shipping a plausible-looking
		// index with every external symbol missing.
		if _, err := os.Stat(filepath.Join(u.Dir, "node_modules")); os.IsNotExist(err) {
			r.logf("  %s: WARNING the install created no node_modules. scip-typescript resolves "+
				"imports through ordinary Node resolution, so every symbol this unit imports from a "+
				"dependency will be MISSING from the index, and nothing downstream can tell.", u.Prefix)
		}
	}

	out := filepath.Join(u.Dir, ".clew-unit.scip")
	if err := r.run(ctx, u.Dir, "npx", "--yes", ScipTypeScriptPackage,
		"index", "--output", out,
	); err != nil {
		return "", fmt.Errorf("scip-typescript: %w", err)
	}
	return out, nil
}

// installPlan is how one unit's node_modules gets materialised: which package
// manager to run, why it was chosen, and what to do if it fails.
type installPlan struct {
	manager  string // the binary that must be on $PATH
	lockfile string // the file that selected it; empty when none was found
	args     []string
	env      []string // extra environment, `KEY=value`, added to clew's own

	// note explains an env override to whoever reads the log. Empty for a plan
	// that only runs the obvious command.
	note string

	// fallback is a second command to try when args fails. Only the npm plan
	// carries one, and it is the single place clew may write to a build file.
	// See planInstall.
	fallback []string
}

// String is the command as a reader would type it, environment included, so the
// log line names everything that shaped the install.
func (p installPlan) String() string {
	parts := append(append([]string{}, p.env...), p.manager)
	return strings.Join(append(parts, p.args...), " ")
}

// planInstall picks the package manager a unit is actually developed with.
//
// Running the wrong one is not a slower path, it is a broken one. npm resolves a
// dependency tree the project has never used, and where the project's own graph
// is one npm's resolver refuses outright -- a peer dependency it will not
// reconcile -- the unit produces no index at all rather than a degraded one.
// immer is exactly that repository, which is why it is a fixture.
//
// Every command is FROZEN: the lockfile decides the tree and is not rewritten,
// because "no build file is ever modified" applies to a lockfile as much as to a
// pom. The npm plan is the one exception and it is deliberate. `npm ci` refuses a
// lockfile that has drifted from package.json, and enough real repositories have
// drifted that a hard failure there would cost more indexes than the invariant
// is worth; installDependencies falls back to `npm install` and says so.
//
// PRECEDENCE matters only when a repository carries more than one lockfile, which
// is ambiguous by construction -- nothing in the tree records which one is real.
// npm's comes last because it is the one most often left behind by accident: a
// single `npm install` in a yarn or pnpm repository writes a package-lock.json
// that nobody intended and nobody removed. The choice is logged either way.
//
// A `packageManager` field in package.json is corepack's authoritative answer to
// this question and is NOT consulted yet; the lockfiles agree with it in every
// case seen so far.
func planInstall(dir string) installPlan {
	has := func(name string) bool { return exists(filepath.Join(dir, name)) }

	switch {
	case has("pnpm-lock.yaml"):
		return installPlan{
			manager:  "pnpm",
			lockfile: "pnpm-lock.yaml",
			args:     []string{"install", "--frozen-lockfile"},
		}
	case has("yarn.lock"):
		return yarnPlan(filepath.Join(dir, "yarn.lock"))
	// bun 1.2 replaced the binary bun.lockb with the textual bun.lock. Both are
	// still in the wild, and both mean bun.
	case has("bun.lock"):
		return installPlan{
			manager:  "bun",
			lockfile: "bun.lock",
			args:     []string{"install", "--frozen-lockfile"},
		}
	case has("bun.lockb"):
		return installPlan{
			manager:  "bun",
			lockfile: "bun.lockb",
			args:     []string{"install", "--frozen-lockfile"},
		}
	}

	npmFlags := []string{"--silent", "--no-audit", "--no-fund"}
	for _, lock := range []string{"package-lock.json", "npm-shrinkwrap.json"} {
		if has(lock) {
			return installPlan{
				manager:  "npm",
				lockfile: lock,
				args:     append([]string{"ci"}, npmFlags...),
				fallback: append([]string{"install"}, npmFlags...),
			}
		}
	}
	// No lockfile at all: there is nothing to freeze, and npm is the manager
	// every Node installation already has.
	return installPlan{
		manager: "npm",
		args:    append([]string{"install"}, npmFlags...),
	}
}

// yarnPlan builds the install plan for a yarn unit, which needs the yarn MAJOR
// version for two separate reasons.
//
// The freeze flag was renamed: classic (1.x) takes --frozen-lockfile, berry (2+)
// takes --immutable and rejects the old spelling.
//
// The linker is the other, and it is the reason a berry unit needs an override at
// all. Berry's DEFAULT install mode is Plug'n'Play: no node_modules, dependencies
// left zipped under .yarn/cache, resolution through a generated .pnp.cjs.
// scip-typescript resolves imports through ordinary Node resolution, so against a
// PnP tree it finds no dependency and SAYS NOTHING -- exit 0, an index that looks
// complete and has lost every external symbol (issue #8). Setting the linker for
// clew's install alone materialises a node_modules and produces an index
// identical to the same project's node-modules control, byte-for-byte in symbols.
//
// It is an environment variable rather than a config edit precisely so that
// `.yarnrc.yml` and `yarn.lock` are untouched: "no build file is ever modified"
// holds. What it does leave behind is a node_modules in a repository that
// deliberately opted out of one, which is the honest cost of this fix. The
// alternative -- running scip-typescript under yarn's PnP loader -- needs
// PnP-aware TypeScript resolution inside a tool clew does not own, and upstream
// has declined it: sourcegraph/scip-typescript#259 was closed as NOT PLANNED on
// 2026-01-03, and microsoft/TypeScript#35206, which would have made it moot, was
// closed unmerged after six years. Do not remove this override on the assumption
// that upstream is about to fix it; see ADR 3 for the three things that would.
//
// The override is unconditional for berry rather than gated on reading
// `nodeLinker` out of `.yarnrc.yml`, and that is deliberate on two counts.
// Absence of the key is not absence of PnP -- it IS the PnP default -- so the
// detection would have to assume PnP for the unset case anyway. And for the two
// modes where PnP is not in effect the override changes nothing that matters: a
// `nodeLinker: node-modules` project was already getting exactly this tree, and a
// `nodeLinker: pnpm` one gets the same packages with a flatter layout. In every
// case the versions come from the lockfile, which is frozen.
func yarnPlan(lockfile string) installPlan {
	plan := installPlan{manager: "yarn", lockfile: filepath.Base(lockfile)}
	if yarnIsClassic(lockfile) {
		plan.args = []string{"install", "--frozen-lockfile"}
		return plan
	}
	plan.args = []string{"install", "--immutable"}
	plan.env = []string{yarnNodeModulesLinker}
	plan.note = "yarn 2+ installs Plug'n'Play by default, which scip-typescript cannot resolve; " +
		"the linker is overridden for this install only, so .yarnrc.yml and yarn.lock are untouched"
	return plan
}

// yarnIsClassic reports whether a yarn.lock was written by yarn 1.x. The lockfile
// itself is the version marker -- classic writes a `yarn lockfile v1` banner,
// berry does not -- so this needs no yarn on $PATH to decide.
func yarnIsClassic(lockfile string) bool {
	head := make([]byte, 512)
	f, err := os.Open(lockfile)
	if err != nil {
		// Unreadable but present: berry is the safer guess, since classic is the
		// version being retired.
		return false
	}
	defer f.Close()
	n, _ := f.Read(head)
	return strings.Contains(string(head[:n]), "yarn lockfile v1")
}

// installDependencies materialises node_modules for a unit that has none.
func installDependencies(ctx context.Context, r *runner, u Unit) error {
	plan := planInstall(u.Dir)

	if _, err := exec.LookPath(plan.manager); err != nil {
		if plan.lockfile == "" {
			return fmt.Errorf("npm is not on $PATH, and this unit has no lockfile naming another package manager")
		}
		// Deliberately not falling back to npm. Installing with a manager the
		// project does not use resolves a different dependency tree, and an index
		// built against the wrong tree is wrong in ways nothing here can see.
		return fmt.Errorf("%s is not on $PATH: %s says this unit is managed with %s, "+
			"and installing with anything else resolves a dependency tree the project does not use",
			plan.manager, plan.lockfile, plan.manager)
	}

	why := plan.lockfile
	if why == "" {
		why = "no lockfile found"
	}
	r.logf("  %s: node_modules missing, running `%s` (%s)", u.Prefix, plan, why)
	if plan.note != "" {
		r.logf("  %s: %s", u.Prefix, plan.note)
	}
	// The linker override does not merely ADD a node_modules: yarn also removes
	// the .pnp.cjs it is replacing, and some Plug'n'Play repositories commit that
	// file (a "zero-install" setup commits .yarn/cache with it). Nothing is lost
	// -- it is generated, and the project's own `yarn install` puts it back and
	// takes the node_modules away again -- but a file vanishing from a working
	// tree is not something an indexer gets to do quietly.
	if exists(filepath.Join(u.Dir, ".pnp.cjs")) && slices.Contains(plan.env, yarnNodeModulesLinker) {
		r.logf("  %s: NOTE this replaces the project's generated .pnp.cjs with a node_modules tree. "+
			"Run `yarn install` to restore Plug'n'Play, which also removes the node_modules.", u.Prefix)
	}

	err := r.runEnv(ctx, u.Dir, plan.env, plan.manager, plan.args...)
	if err == nil {
		return nil
	}
	if plan.fallback == nil {
		return fmt.Errorf("%s: %w", plan, err)
	}

	// The one place clew may write to a build file, so it says so out loud.
	// `npm ci` fails outright when package-lock.json has drifted from
	// package.json, which is common enough that refusing to index would cost
	// more than the rewrite does.
	r.logf("  %s: `%s` failed, most likely because %s has drifted from package.json. "+
		"Falling back to `npm %s`, WHICH MAY REWRITE %s.\n%v",
		u.Prefix, plan, plan.lockfile, strings.Join(plan.fallback, " "), plan.lockfile, err)
	if err := r.runEnv(ctx, u.Dir, plan.env, plan.manager, plan.fallback...); err != nil {
		return fmt.Errorf("npm %s: %w", strings.Join(plan.fallback, " "), err)
	}
	return nil
}
