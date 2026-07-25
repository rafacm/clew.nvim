package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// Tier 1. runner.memo is the mechanism that keeps concurrently-indexed units
// from racing on shared scratch space under .clew/tools. Three races were fixed
// by routing through it; these tests exist to stop a fourth.

func TestNewRunner_CreatesToolsDir(t *testing.T) {
	root := t.TempDir()
	r, err := newRunner(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".clew", "tools"); r.toolsDir != want {
		t.Errorf("toolsDir = %q, want %q", r.toolsDir, want)
	}
	if fi, err := os.Stat(r.toolsDir); err != nil || !fi.IsDir() {
		t.Errorf("toolsDir was not created: %v", err)
	}
}

func TestMemo_RunsOncePerKey(t *testing.T) {
	r, err := newRunner(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	fn := func() (string, error) {
		calls.Add(1)
		return "resolved", nil
	}

	for range 3 {
		got, err := r.memo("k", fn)
		if err != nil {
			t.Fatal(err)
		}
		if got != "resolved" {
			t.Fatalf("memo = %q, want %q", got, "resolved")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("fn ran %d times, want 1", n)
	}
}

func TestMemo_KeysAreIndependent(t *testing.T) {
	r, err := newRunner(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := r.memo("a", func() (string, error) { return "A", nil })
	b, _ := r.memo("b", func() (string, error) { return "B", nil })
	if a != "A" || b != "B" {
		t.Errorf("memo returned (%q, %q), want (A, B)", a, b)
	}
}

// A transient mvn failure must not poison the rest of the run: the next unit to
// ask for the same artifact retries rather than inheriting the error.
func TestMemo_DoesNotCacheFailures(t *testing.T) {
	r, err := newRunner(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var calls int
	fn := func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("transient")
		}
		return "resolved", nil
	}

	if _, err := r.memo("k", fn); err == nil {
		t.Fatal("first memo returned no error, want the transient failure")
	}
	got, err := r.memo("k", fn)
	if err != nil {
		t.Fatalf("retry returned %v, want it to succeed", err)
	}
	if got != "resolved" {
		t.Errorf("memo = %q, want %q", got, "resolved")
	}
}

// Units are indexed concurrently and share one runner. Two units resolving the
// same classpath in parallel is not merely wasteful: they race on the same cache
// file and the same argfile. memo must serialise first-time resolution.
//
// Run under -race, this is the regression test for that class of bug.
func TestMemo_SerialisesConcurrentCallers(t *testing.T) {
	r, err := newRunner(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	var calls atomic.Int32
	var inFlight atomic.Int32
	var overlapped atomic.Bool

	// A shared, unsynchronised value the memoised function writes: under -race
	// this reports a data race the moment memo lets two callers in at once.
	shared := ""

	var wg sync.WaitGroup
	results := make([]string, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := r.memo("classpath", func() (string, error) {
				if inFlight.Add(1) > 1 {
					overlapped.Store(true)
				}
				defer inFlight.Add(-1)
				calls.Add(1)
				shared = "resolved"
				return shared, nil
			})
			if err != nil {
				t.Errorf("memo: %v", err)
			}
			results[i] = v
		}(i)
	}
	wg.Wait()

	if overlapped.Load() {
		t.Error("memo ran the resolver concurrently; it must serialise first-time resolution")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("resolver ran %d times, want 1", n)
	}
	for i, got := range results {
		if got != "resolved" {
			t.Errorf("goroutine %d got %q, want %q", i, got, "resolved")
		}
	}
}

func TestRunner_LogfToleratesANilWriter(t *testing.T) {
	r, err := newRunner(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.logf("this must not panic: %d", 1) // Options.Log is optional.
}
