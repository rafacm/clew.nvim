package indexer

import "context"

// A Producer drives one external SCIP indexer. It recognises the build roots it
// owns and turns one of them into a `.scip` file.
//
// This interface is the only place a language enters clew. Everything downstream
// of Index -- the merge, the query layer, the language server -- is
// language-neutral and must stay that way, so adding a language means adding a
// Producer and one line in the registry below, and touching nothing else.
type Producer interface {
	// Kind is the stable identifier reported by `clew units`.
	Kind() Kind

	// Detect reports whether dir is a root this producer owns, and the build file
	// that says so. It examines dir only: the directory walk, the skip list and
	// the don't-descend-into-a-unit rule all belong to Discover.
	Detect(dir string) (buildFile string, ok bool)

	// Index produces a SCIP index for u and returns the path to it.
	//
	// Called concurrently, once per unit. Producers are stateless values, so
	// anything worth caching across units goes through runner.memo rather than a
	// field.
	Index(ctx context.Context, r *runner, u Unit) (string, error)
}

// producers is consulted in order and the first match wins, so this ordering is
// the detection precedence.
//
// TypeScript precedes the JVM producers deliberately: an Angular or Node app may
// sit in the same directory as a pom.xml, and package.json plus a tsconfig is the
// more specific signal.
var producers = []Producer{
	typeScriptProducer{},
	mavenProducer{},
	gradleProducer{},
}

// producerFor returns the producer that owns kind.
func producerFor(kind Kind) (Producer, bool) {
	for _, p := range producers {
		if p.Kind() == kind {
			return p, true
		}
	}
	return nil, false
}
