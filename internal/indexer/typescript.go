package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		r.logf("  %s: node_modules missing, running npm install", u.Prefix)
		if err := r.run(ctx, u.Dir, "npm", "install", "--silent", "--no-audit", "--no-fund"); err != nil {
			return "", fmt.Errorf("npm install: %w", err)
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
