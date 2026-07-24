// Command clew builds and serves SCIP indexes for polyglot projects,
// including git-submodule umbrellas.
//
//	clew index [--root DIR] [--output PATH] [--unit NAME]
//	clew units [--root DIR]
//	clew lsp
//	clew version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/rafacm/clew/internal/indexer"
	"github.com/rafacm/clew/internal/lsp"
)

var version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "index":
		os.Exit(cmdIndex(os.Args[2:]))
	case "units":
		os.Exit(cmdUnits(os.Args[2:]))
	case "lsp":
		os.Exit(cmdLSP(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "clew: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `clew -- Ariadne's thread for your codebase

Usage:
  clew index [flags]   Build or rebuild the project index
  clew units [flags]   List discoverable indexable units
  clew lsp             Run the language server on stdio
  clew version         Print the version

Flags for index/units:
  --root DIR      Project root (default: current directory)
  --output PATH   Index path, relative to root (default: .clew/index.scip)
  --unit NAME     Index only this unit (default: all)
  --jobs N        Parallel unit indexing (default: NumCPU)
`)
}

func cmdIndex(args []string) int {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	root := fs.String("root", ".", "project root")
	output := fs.String("output", ".clew/index.scip", "index path, relative to root")
	unit := fs.String("unit", "", "index only this unit")
	jobs := fs.Int("jobs", 0, "parallel unit indexing (0 = NumCPU)")
	_ = fs.Parse(args)

	if err := indexer.Run(context.Background(), indexer.Options{
		Root:   *root,
		Output: *output,
		Unit:   *unit,
		Jobs:   *jobs,
		Log:    os.Stderr,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "clew index: %v\n", err)
		return 1
	}
	return 0
}

func cmdUnits(args []string) int {
	fs := flag.NewFlagSet("units", flag.ExitOnError)
	root := fs.String("root", ".", "project root")
	_ = fs.Parse(args)

	units, err := indexer.Discover(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clew units: %v\n", err)
		return 1
	}
	if len(units) == 0 {
		fmt.Fprintln(os.Stderr, "no indexable units found")
		return 1
	}
	for _, u := range units {
		fmt.Printf("%-40s %-12s %s\n", u.Prefix, u.Kind, u.BuildFile)
	}
	return 0
}

func cmdLSP(args []string) int {
	fs := flag.NewFlagSet("lsp", flag.ExitOnError)
	_ = fs.Parse(args)

	if err := lsp.Serve(context.Background(), os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "clew lsp: %v\n", err)
		return 1
	}
	return 0
}
