package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runner owns process execution and the resolved-artifact cache shared across
// units. Resolving scip artifacts is slow the first time and free afterwards, so
// it is deliberately hoisted out of the per-unit path.
type runner struct {
	root string
	log  io.Writer

	cachedJavacCP string
	cachedCLICP   string
	toolsDir      string
}

func newRunner(root string, log io.Writer) (*runner, error) {
	tools := filepath.Join(root, ".clew", "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return nil, err
	}
	return &runner{root: root, log: log, toolsDir: tools}, nil
}

func (r *runner) logf(format string, args ...any) {
	if r.log == nil {
		return
	}
	fmt.Fprintf(r.log, format+"\n", args...)
}

func (r *runner) run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return fmt.Errorf("%s: %w\n%s", name, err, msg)
	}
	return nil
}

// resolveArtifact returns the full transitive classpath for an org.scip-code
// artifact, using a throwaway POM so Maven does the resolution.
func (r *runner) resolveArtifact(ctx context.Context, artifact string) (string, error) {
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

// runScipJava invokes the scip-java CLI. The resolved classpath routinely exceeds
// the shell's argument limit, so it goes through an @argfile.
func (r *runner) runScipJava(ctx context.Context, dir string, args ...string) error {
	if r.cachedCLICP == "" {
		cp, err := r.resolveArtifact(ctx, "scip-java")
		if err != nil {
			return err
		}
		r.cachedCLICP = cp
	}
	argfile := filepath.Join(r.toolsDir, "scip-java.args")
	if err := os.WriteFile(argfile, []byte("-cp\n"+r.cachedCLICP+"\n"), 0o644); err != nil {
		return err
	}
	full := append([]string{"@" + argfile, "org.scip_code.scip_java.ScipJava"}, args...)
	return r.run(ctx, dir, "java", full...)
}
