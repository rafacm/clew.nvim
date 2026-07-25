// Package acceptance holds clew's tier 3 suite: real projects, downloaded at
// pinned commits, indexed with the real indexers, asserted on.
//
// Every file with test content is behind `//go:build acceptance`, so
// `go test ./...` never reaches the network and never needs a JDK, Maven or
// Node. Run the suite deliberately:
//
//	make test-acceptance
//
// This file carries no build tag purely so the package always has one Go file
// to parse; without it, tooling reports the package as having no buildable
// sources.
//
// See doc/adr/0001-testing-strategy.md for the tier boundaries, the pinned
// commit table, and why fixtures are downloaded rather than committed.
package acceptance
