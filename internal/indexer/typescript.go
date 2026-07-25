package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ScipTypeScriptPackage is the npm package clew invokes via npx.
const ScipTypeScriptPackage = "@sourcegraph/scip-typescript@latest"

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

	// fallback is a second command to try when args fails. Only the npm plan
	// carries one, and it is the single place clew may write to a build file.
	// See planInstall.
	fallback []string
}

func (p installPlan) String() string {
	return p.manager + " " + strings.Join(p.args, " ")
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
		return installPlan{
			manager:  "yarn",
			lockfile: "yarn.lock",
			args:     []string{"install", yarnFreezeFlag(filepath.Join(dir, "yarn.lock"))},
		}
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

// yarnFreezeFlag returns the flag that makes `yarn install` refuse to touch the
// lockfile, which yarn renamed between major versions: classic takes
// --frozen-lockfile, berry (2+) takes --immutable and rejects the old spelling.
// The lockfile itself is the version marker -- classic writes a `yarn lockfile
// v1` banner, berry does not -- so this needs no yarn on $PATH to decide.
func yarnFreezeFlag(lockfile string) string {
	head := make([]byte, 512)
	f, err := os.Open(lockfile)
	if err != nil {
		// Unreadable but present: berry is the safer guess, since classic is the
		// version being retired.
		return "--immutable"
	}
	defer f.Close()
	n, _ := f.Read(head)
	if strings.Contains(string(head[:n]), "yarn lockfile v1") {
		return "--frozen-lockfile"
	}
	return "--immutable"
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

	err := r.run(ctx, u.Dir, plan.manager, plan.args...)
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
	if err := r.run(ctx, u.Dir, plan.manager, plan.fallback...); err != nil {
		return fmt.Errorf("npm %s: %w", strings.Join(plan.fallback, " "), err)
	}
	return nil
}
