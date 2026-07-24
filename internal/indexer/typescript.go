package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ScipTypeScriptPackage is the npm package clew invokes via npx.
const ScipTypeScriptPackage = "@sourcegraph/scip-typescript@latest"

// indexTypeScript indexes a TypeScript/JavaScript unit.
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
func (r *runner) indexTypeScript(ctx context.Context, u Unit) (string, error) {
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
