package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScipVersion is the scip-java / scip-javac release clew drives.
const ScipVersion = "0.13.1"

// mavenProducer drives scip-javac over a Maven unit.
type mavenProducer struct{}

func (mavenProducer) Kind() Kind { return KindMaven }

func (mavenProducer) Detect(dir string) (string, bool) {
	if exists(filepath.Join(dir, "pom.xml")) {
		return "pom.xml", true
	}
	return "", false
}

// Index indexes a Maven unit WITHOUT running the project's build and WITHOUT
// modifying its pom.xml.
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
// javacopts.txt does. scip-java's aggregator reaches it through
// ClasspathEntry.fromTargetroot, which reads targetroot/javacopts.txt when it
// exists and falls back to dependency discovery when it does not.
//
// Asserted by TestAcceptance_SingleRepository_Maven/SymbolsCarryCoordinates.
//
// The same degradation has a second cause, and it is why u.Dir must already be
// realpath'd when it arrives here: the aggregator recovers the coordinate by
// walking up from the `-d` directory to find a pom.xml, bounded by a REALPATH'D
// sourceroot. Any spelling of u.Dir that still contains a symlink fails that
// bound. indexer.resolveRoot is what keeps that from happening; see
// TestAcceptance_SingleRepository_MavenViaSymlink.
func (mavenProducer) Index(ctx context.Context, r *runner, u Unit) (string, error) {
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

// gradleProducer recognises Gradle units so `clew units` reports them honestly,
// and fails with a clear message rather than a silent omission when asked to
// index one.
type gradleProducer struct{}

func (gradleProducer) Kind() Kind { return KindGradle }

func (gradleProducer) Detect(dir string) (string, bool) {
	if exists(filepath.Join(dir, "build.gradle")) || exists(filepath.Join(dir, "build.gradle.kts")) {
		return "build.gradle", true
	}
	return "", false
}

func (gradleProducer) Index(context.Context, *runner, Unit) (string, error) {
	return "", fmt.Errorf("gradle units are not implemented yet")
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
	return r.memo("scip-javac.classpath", func() (string, error) {
		return resolveScipArtifact(ctx, r, "scip-javac")
	})
}

// runScipJava invokes the scip-java CLI. The resolved classpath routinely exceeds
// the shell's argument limit, so it goes through an @argfile.
func (r *runner) runScipJava(ctx context.Context, dir string, args ...string) error {
	argfile, err := r.scipJavaArgfile(ctx)
	if err != nil {
		return err
	}
	full := append([]string{"@" + argfile, "org.scip_code.scip_java.ScipJava"}, args...)
	return r.run(ctx, dir, "java", full...)
}

// scipJavaArgfile resolves the scip-java CLI and writes its @argfile, once per
// run. Resolution and the write are memoised together on purpose: the argfile has
// one fixed path under toolsDir, so units writing it concurrently would race on
// the file another unit is handing to java.
func (r *runner) scipJavaArgfile(ctx context.Context) (string, error) {
	return r.memo("scip-java.argfile", func() (string, error) {
		cp, err := resolveScipArtifact(ctx, r, "scip-java")
		if err != nil {
			return "", err
		}
		path := filepath.Join(r.toolsDir, "scip-java.args")
		if err := os.WriteFile(path, []byte("-cp\n"+cp+"\n"), 0o644); err != nil {
			return "", err
		}
		return path, nil
	})
}

// resolveScipArtifact returns the full transitive classpath for an org.scip-code
// artifact, using a throwaway POM so Maven does the resolution. The classpath is
// cached on disk under toolsDir, so it survives across runs as well.
//
// Callers must reach this through runner.memo: it writes to a fixed path per
// artifact and cannot be run concurrently for the same one.
func resolveScipArtifact(ctx context.Context, r *runner, artifact string) (string, error) {
	dir := filepath.Join(r.toolsDir, artifact)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cpFile := filepath.Join(dir, "classpath.txt")

	if b, err := os.ReadFile(cpFile); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b)), nil
	}

	pom := fmt.Sprintf(`<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>dev.clew</groupId><artifactId>resolve-%s</artifactId><version>1</version>
  <dependencies><dependency>
    <groupId>org.scip-code</groupId><artifactId>%s</artifactId><version>%s</version>
  </dependency></dependencies>
</project>`, artifact, artifact, ScipVersion)

	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(pom), 0o644); err != nil {
		return "", err
	}
	if err := r.run(ctx, dir, "mvn", "-B", "-q",
		"dependency:build-classpath", "-Dmdep.outputFile="+cpFile,
	); err != nil {
		return "", fmt.Errorf("resolving %s: %w", artifact, err)
	}
	b, err := os.ReadFile(cpFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
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
