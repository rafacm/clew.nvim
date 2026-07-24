package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ScipVersion is the scip-java / scip-javac release clew drives.
const ScipVersion = "0.13.1"

// indexMaven indexes a Maven unit WITHOUT running the project's build and
// WITHOUT modifying its pom.xml.
//
// The pipeline, validated end-to-end against spring-petclinic:
//
//  1. `mvn dependency:build-classpath` -- resolution only, no compile.
//  2. `javac -Xplugin:scip` with the scip-javac plugin on the compiler classpath.
//  3. write javacopts.txt into the targetroot.   <-- see the warning below
//  4. `scip-java aggregate` to fold per-file shards into one index.
//
// Step 3 is the one that is easy to miss and expensive to debug. scip-java's
// aggregator derives Maven coordinates from javacopts.txt. Without it every
// symbol this unit defines degrades to:
//
//	scip-java maven . . org/example/Foo#
//
// instead of:
//
//	scip-java maven maven/org.example/svc 1.2.0 org/example/Foo#
//
// That degraded form is *internally consistent*, so navigation inside the unit
// keeps working and the bug is invisible locally. It only surfaces when units are
// merged, where every unit's symbols collapse into the same anonymous package.
//
// The documented `dependencies.txt` mechanism does NOT produce coordinates here;
// javacopts.txt does. See TestMavenSymbolsCarryCoordinates.
func (r *runner) indexMaven(ctx context.Context, u Unit) (string, error) {
	targetroot := filepath.Join(u.Dir, "target", "clew-targetroot")
	classes := filepath.Join(u.Dir, "target", "clew-classes")
	for _, d := range []string{targetroot, classes} {
		if err := os.RemoveAll(d); err != nil {
			return "", err
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}

	// 1. resolve the compile classpath (no build, pom untouched)
	cpFile := filepath.Join(u.Dir, "target", "clew-cp.txt")
	if err := r.run(ctx, u.Dir, "mvn", "-B", "-q",
		"dependency:build-classpath",
		"-Dmdep.outputFile="+cpFile,
		"-DincludeScope=compile",
	); err != nil {
		return "", fmt.Errorf("resolving classpath: %w", err)
	}
	projectCP, err := os.ReadFile(cpFile)
	if err != nil {
		return "", err
	}

	pluginCP, err := r.scipJavacClasspath(ctx)
	if err != nil {
		return "", err
	}
	fullCP := pluginCP + string(os.PathListSeparator) + strings.TrimSpace(string(projectCP))

	// 2. compile with the SCIP compiler plugin
	sources, err := collectSources(filepath.Join(u.Dir, "src", "main", "java"), ".java")
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", fmt.Errorf("no java sources under %s/src/main/java", u.Dir)
	}
	srcList := filepath.Join(u.Dir, "target", "clew-sources.txt")
	if err := os.WriteFile(srcList, []byte(strings.Join(sources, "\n")+"\n"), 0o644); err != nil {
		return "", err
	}

	args := []string{
		"-classpath", fullCP,
		"-d", classes,
		"-proc:none",
		fmt.Sprintf("-Xplugin:scip -sourceroot:%s -targetroot:%s", u.Dir, targetroot),
		"@" + srcList,
	}
	if err := r.run(ctx, u.Dir, "javac", args...); err != nil {
		// javac is intentionally non-fatal: the SCIP plugin still emits shards for
		// everything that compiled, which is far more useful than nothing.
		r.logf("  %s: javac reported errors; indexing what compiled", u.Prefix)
	}

	// 3. javacopts.txt -- REQUIRED for coordinate stamping. See doc comment.
	if err := writeJavacOpts(targetroot, classes, fullCP, u.Dir, sources); err != nil {
		return "", err
	}

	// 4. aggregate shards into a single index
	out := filepath.Join(u.Dir, "target", "clew-unit.scip")
	if err := r.runScipJava(ctx, u.Dir,
		"aggregate", "--targetroot="+targetroot, "--output="+out,
	); err != nil {
		return "", fmt.Errorf("aggregating: %w", err)
	}
	return out, nil
}

// writeJavacOpts emits the file scip-java's aggregator reads to recover Maven
// coordinates. Format: `-version` on the first line, then every javac option and
// source file individually double-quoted, one per line.
func writeJavacOpts(targetroot, classes, classpath, unitDir string, sources []string) error {
	var b strings.Builder
	b.WriteString("-version\n")
	q := func(s string) { fmt.Fprintf(&b, "%q\n", s) }
	q("-d")
	q(classes)
	q("-classpath")
	q(classpath)
	q("-sourcepath")
	q(filepath.Join(unitDir, "src", "main", "java") + string(os.PathListSeparator))
	for _, s := range sources {
		q(s)
	}
	return os.WriteFile(filepath.Join(targetroot, "javacopts.txt"), []byte(b.String()), 0o644)
}

// scipJavacClasspath resolves the compiler plugin and its dependencies.
//
// scip-java's docs describe scip-javac as "a zero-dependency Java library". As of
// 0.13.1 that is wrong -- it needs scip-shared, scip-java-bindings and
// protobuf-java, and fails at plugin init with NoClassDefFoundError without them.
// Resolving through a throwaway POM gets the real transitive closure.
func (r *runner) scipJavacClasspath(ctx context.Context) (string, error) {
	if r.cachedJavacCP != "" {
		return r.cachedJavacCP, nil
	}
	cp, err := r.resolveArtifact(ctx, "scip-javac")
	if err != nil {
		return "", err
	}
	r.cachedJavacCP = cp
	return cp, nil
}

func collectSources(dir, ext string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ext) {
			out = append(out, path)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

var _ = exec.Command // keep os/exec imported for runner helpers
