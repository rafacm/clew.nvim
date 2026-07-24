package indexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind identifies which indexer handles a unit.
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
	abs, err := filepath.Abs(root)
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

// classify decides whether dir is a unit root, and of what kind.
func classify(root, dir string) (Unit, bool) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return Unit{}, false
	}
	if rel == "." {
		rel = ""
	}
	rel = filepath.ToSlash(rel)

	mk := func(kind Kind, file string) (Unit, bool) {
		return Unit{Prefix: rel, Dir: dir, BuildFile: file, Kind: kind}, true
	}

	// TypeScript is checked first: an Angular or Node project may sit beside a
	// pom.xml in the same repo, and package.json is the more specific signal.
	if exists(filepath.Join(dir, "package.json")) &&
		(exists(filepath.Join(dir, "tsconfig.json")) || exists(filepath.Join(dir, "angular.json"))) {
		return mk(KindTypeScript, "package.json")
	}
	if exists(filepath.Join(dir, "pom.xml")) {
		return mk(KindMaven, "pom.xml")
	}
	if exists(filepath.Join(dir, "build.gradle")) || exists(filepath.Join(dir, "build.gradle.kts")) {
		return mk(KindGradle, "build.gradle")
	}
	return Unit{}, false
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
