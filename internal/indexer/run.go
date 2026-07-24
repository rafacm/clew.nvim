package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/rafacm/clew/internal/index"
)

// Options controls a single `clew index` invocation.
type Options struct {
	Root   string
	Output string // relative to Root
	Unit   string // empty = all units
	Jobs   int    // 0 = NumCPU
	Log    io.Writer
}

type result struct {
	unit Unit
	path string
	err  error
}

// Run discovers units, indexes each with the appropriate producer, and merges the
// results into a single index at Root/Output.
//
// The merge runs even for a single unit. Keeping one code path means the merge --
// path rewriting, metadata normalisation, external-symbol dedup -- is exercised on
// every run rather than being a rarely-taken branch that silently rots.
func Run(ctx context.Context, opts Options) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return err
	}
	if opts.Output == "" {
		opts.Output = ".clew/index.scip"
	}
	if opts.Jobs <= 0 {
		opts.Jobs = runtime.NumCPU()
	}

	r, err := newRunner(root, opts.Log)
	if err != nil {
		return err
	}

	units, err := Discover(root)
	if err != nil {
		return err
	}
	units = Filter(units, opts.Unit)
	if len(units) == 0 {
		return fmt.Errorf("no indexable units found under %s", root)
	}

	r.logf("clew: %d unit(s), %d job(s)", len(units), opts.Jobs)

	sem := make(chan struct{}, opts.Jobs)
	results := make([]result, len(units))
	var wg sync.WaitGroup

	for i, u := range units {
		wg.Add(1)
		go func(i int, u Unit) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			started := time.Now()
			var path string
			var err error
			switch u.Kind {
			case KindMaven:
				path, err = r.indexMaven(ctx, u)
			case KindTypeScript:
				path, err = r.indexTypeScript(ctx, u)
			case KindGradle:
				err = fmt.Errorf("gradle units are not implemented yet")
			default:
				err = fmt.Errorf("unknown unit kind %q", u.Kind)
			}
			if err != nil {
				r.logf("  %-32s FAILED  %v", u.Prefix, err)
			} else {
				r.logf("  %-32s %-10s %.1fs", u.Prefix, u.Kind, time.Since(started).Seconds())
			}
			results[i] = result{unit: u, path: path, err: err}
		}(i, u)
	}
	wg.Wait()

	var inputs []index.Input
	var failed int
	for _, res := range results {
		if res.err != nil {
			failed++
			continue
		}
		inputs = append(inputs, index.Input{Prefix: res.unit.Prefix, Path: res.path})
	}
	if len(inputs) == 0 {
		return fmt.Errorf("every unit failed to index")
	}
	if failed > 0 {
		// Partial indexes are useful; silently pretending otherwise is not.
		r.logf("clew: %d unit(s) failed; merging the remaining %d", failed, len(inputs))
	}

	out := filepath.Join(root, opts.Output)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	stats, err := index.Merge(index.MergeOptions{
		ProjectRoot: root,
		Inputs:      inputs,
		Output:      out,
	})
	if err != nil {
		return fmt.Errorf("merging: %w", err)
	}

	r.logf("clew: %s -- %d documents, %d occurrences, %d external symbols",
		out, stats.Documents, stats.Occurrences, stats.ExternalSymbols)
	if stats.PathCollisions > 0 {
		r.logf("clew: WARNING %d relative_path collision(s) after prefixing", stats.PathCollisions)
	}
	return nil
}
