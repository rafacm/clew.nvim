package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// runner owns process execution and the scratch directory shared across units.
//
// It is deliberately language-neutral. Anything specific to one indexer's
// toolchain -- a resolved classpath, a virtualenv, a downloaded binary -- belongs
// to that Producer's file and reaches across units through memo, never through a
// field here. Two languages ago this struct had grown Java-shaped fields; that is
// the shape to keep it out of.
type runner struct {
	root string
	log  io.Writer

	// toolsDir is scratch space under .clew/tools that any producer may use.
	toolsDir string

	mu    sync.Mutex
	memos map[string]string
}

func newRunner(root string, log io.Writer) (*runner, error) {
	tools := filepath.Join(root, ".clew", "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return nil, err
	}
	return &runner{root: root, log: log, toolsDir: tools, memos: map[string]string{}}, nil
}

func (r *runner) logf(format string, args ...any) {
	if r.log == nil {
		return
	}
	fmt.Fprintf(r.log, format+"\n", args...)
}

// memo runs fn once per key per `clew index` invocation and caches its result.
//
// Units are indexed concurrently and share one runner, so every cross-unit
// resolution must come through here. Two Maven units resolving the same classpath
// in parallel is not merely wasteful: they race on the same cache file and the
// same argfile under toolsDir. Holding the lock across fn serialises first-time
// resolution, which is the point -- afterwards it is a map lookup.
//
// Failures are not cached, so a transient `mvn` failure does not poison the rest
// of the run. Retries are still serialised, so they cannot race either.
func (r *runner) memo(key string, fn func() (string, error)) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.memos[key]; ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return "", err
	}
	r.memos[key] = v
	return v, nil
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
