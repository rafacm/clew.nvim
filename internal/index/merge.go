// Package index merges per-unit SCIP indexes into one project index.
package index

import (
	"fmt"
	"os"

	"github.com/sourcegraph/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// Input is one unit's index and the path prefix it should be rewritten under.
type Input struct {
	Prefix string // e.g. "java/svc-a"; "" for a single-unit project
	Path   string
}

type MergeOptions struct {
	ProjectRoot string
	Inputs      []Input
	Output      string
}

type Stats struct {
	Documents       int
	Occurrences     int
	ExternalSymbols int
	PathCollisions  int
}

// Merge folds several SCIP indexes into one.
//
// Four rules, each of which silently corrupts the result if skipped. They are
// listed in the order they bite:
//
//  1. Exactly one Metadata, emitted first, carrying the umbrella project_root.
//     Protobuf singular-message merge is last-wins, so concatenating indexes
//     without normalising metadata leaves you with the final unit's project_root.
//
//  2. Every Document.relative_path is prefixed with its unit path. Document has no
//     module or root field -- relative_path is the ONLY disambiguator -- and every
//     Java unit on earth contains `src/main/java/...`.
//
//  3. `local N` symbols are document-scoped, not global. They must never be treated
//     as join keys; `local 0` recurs in every document of every unit.
//
//  4. external_symbols are deduplicated by symbol string.
//
// Cross-unit resolution then works by plain string matching, because SCIP symbols
// carry manager, package, version and descriptor and contain no index-local IDs.
func Merge(opts MergeOptions) (Stats, error) {
	var stats Stats

	out := &scip.Index{
		Metadata: &scip.Metadata{
			Version:              scip.ProtocolVersion_UnspecifiedProtocolVersion,
			ProjectRoot:          "file://" + opts.ProjectRoot,
			TextDocumentEncoding: scip.TextEncoding_UTF8,
			ToolInfo:             &scip.ToolInfo{Name: "clew", Version: "0.0.0-dev"},
		},
	}

	seenPaths := make(map[string]struct{})
	externals := make(map[string]*scip.SymbolInformation)

	for _, in := range opts.Inputs {
		raw, err := os.ReadFile(in.Path)
		if err != nil {
			return stats, fmt.Errorf("reading %s: %w", in.Path, err)
		}
		var idx scip.Index
		if err := proto.Unmarshal(raw, &idx); err != nil {
			return stats, fmt.Errorf("parsing %s: %w", in.Path, err)
		}

		for _, doc := range idx.Documents {
			doc.RelativePath = joinPrefix(in.Prefix, doc.RelativePath)
			if _, dup := seenPaths[doc.RelativePath]; dup {
				stats.PathCollisions++
			}
			seenPaths[doc.RelativePath] = struct{}{}

			out.Documents = append(out.Documents, doc)
			stats.Documents++
			stats.Occurrences += len(doc.Occurrences)
		}

		for _, sym := range idx.ExternalSymbols {
			if _, ok := externals[sym.Symbol]; !ok {
				externals[sym.Symbol] = sym
			}
		}
	}

	for _, sym := range externals {
		out.ExternalSymbols = append(out.ExternalSymbols, sym)
	}
	stats.ExternalSymbols = len(out.ExternalSymbols)

	blob, err := proto.Marshal(out)
	if err != nil {
		return stats, err
	}
	if err := os.WriteFile(opts.Output, blob, 0o644); err != nil {
		return stats, err
	}
	return stats, nil
}

func joinPrefix(prefix, path string) string {
	if prefix == "" {
		return path
	}
	return prefix + "/" + path
}
