package indexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind identifies which indexer handles a unit. Every Kind is owned by exactly
// one Producer; see producer.go.
type Kind string

const (
	KindMaven      Kind = "maven"
	KindGradle     Kind = "gradle"
	KindTypeScript Kind = "typescript"
)

// A Unit is one independently indexable build root.
//
// The umbrella/normal-project distinction deliberately does not exist here: a
// normal project is simply a project with a single unit. Units are discovered by
// build file, not by git boundary, so a monorepo with several Maven modules and a
// frontend -- no submodules involved -- is handled by exactly the same code path.
type Unit struct {
	// Prefix is the unit's path relative to the project root. It becomes the
	// prefix of every Document.relative_path in the merged index, which is what
	// keeps `src/main/java/...` from colliding across units.
	Prefix string
	// Dir is the absolute path to the unit.
	Dir string
	// BuildFile is the file that identified this unit.
	BuildFile string
	Kind      Kind
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "target": true, "build": true,
	"vendor": true, "third_party": true, ".gradle": true, "dist": true,
	".clew": true, "out": true,
}

// Discover walks root and returns every indexable unit.
//
// A directory that yields a unit is not descended into further: scip-java already
// handles multi-module Maven and Gradle builds as a single unit, so recursing
// would produce overlapping indexes.
func Discover(root string) ([]Unit, error) {
	abs, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}

	var units []Unit
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable directories are skipped, not fatal
		}
		if !d.IsDir() {
			return nil
		}
		if path != abs && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		if u, ok := classify(abs, path); ok {
			units = append(units, u)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(units, func(i, j int) bool { return units[i].Prefix < units[j].Prefix })
	return units, nil
}

// classify decides whether dir is a unit root, and of what kind, by asking each
// registered producer in precedence order. It holds no knowledge of any specific
// language: that lives in the producers.
func classify(root, dir string) (Unit, bool) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return Unit{}, false
	}
	if rel == "." {
		rel = ""
	}
	rel = filepath.ToSlash(rel)

	for _, p := range producers {
		if file, ok := p.Detect(dir); ok {
			return Unit{Prefix: rel, Dir: dir, BuildFile: file, Kind: p.Kind()}, true
		}
	}
	return Unit{}, false
}

// resolveRoot makes path absolute and resolves every symlink in it.
//
// This is the single place a user-supplied path enters, and resolving it here is
// what keeps symlinks out of every Unit.Dir: filepath.WalkDir does not follow a
// symlinked directory, so once the root is real every path below it is real too.
//
// It is load-bearing rather than tidy. scip-java bounds its search for a unit's
// pom.xml by a REALPATH'D sourceroot; handed the spelling the user typed, the
// search escapes that bound whenever the two disagree -- a symlinked workspace,
// anything under /tmp on macOS -- no pom is found, and every symbol the unit
// defines degrades to the anonymous `scip-java maven . . ` coordinate. That form
// is internally consistent, so nothing fails until units merge. See
// internal/indexer/java.go.
//
// It also decides whether `--root` may name a symlink at all: WalkDir does not
// follow one, so an unresolved root that *is* a symlink discovers nothing.
//
// A path that cannot be resolved -- almost always because it does not exist --
// is returned absolute but unresolved rather than reported. Discover treats a
// missing root as zero units and Run turns that into a message naming the path,
// which is a better error than EvalSymlinks' own.
func resolveRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, nil
	}
	return resolved, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Filter narrows units by name. An empty name returns everything.
func Filter(units []Unit, name string) []Unit {
	if name == "" {
		return units
	}
	name = strings.TrimSuffix(filepath.ToSlash(name), "/")
	var out []Unit
	for _, u := range units {
		if u.Prefix == name || strings.HasSuffix(u.Prefix, "/"+name) {
			out = append(out, u)
		}
	}
	return out
}
