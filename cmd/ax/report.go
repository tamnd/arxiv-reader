package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/report"
	"github.com/tamnd/arxiv-reader/selection"
)

// runReport writes the committed answers to the questions somebody asks about
// this corpus.
//
// One subcommand per report and no command that writes all of them, because the
// reports do not read the same planes and a corpus half way through a month can
// answer some of these questions and not others. A single command would either
// fail on the first report that had nothing to say or write a file full of
// noughts, and neither is worth having.
func runReport(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax report paths")
	}
	switch args[0] {
	case "paths":
		return reportPaths(args[1:])
	default:
		return fmt.Errorf("unknown report %q, which is paths for now, with coverage, usage, graph and licence to come", args[0])
	}
}

// reportPaths writes reports/paths.md.
//
// It reads the selection and nothing else. A paper's path is a field of its
// entry, how far it has got is another, and going to the content plane to check
// them would be answering a different question, which is whether the selection
// is true. The audit asks that one.
func reportPaths(args []string) error {
	fs := flag.NewFlagSet("ax report paths", flag.ContinueOnError)
	dry := fs.Bool("n", false, "print the report and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("ax report paths takes no arguments, and %q is one", fs.Arg(0))
	}

	root := corpusRoot()
	m, err := selection.Load(corpus.SelectedPath(root))
	if err != nil {
		return err
	}
	if len(m.Selected) == 0 {
		return fmt.Errorf("nothing is selected in %s, and a report about how the selection divides needs a selection", corpus.SelectedPath(""))
	}

	p := report.BuildPaths(m, selection.Today())
	fmt.Fprint(os.Stdout, p.Text())
	if *dry {
		fmt.Fprintln(os.Stderr, "nothing written, because this was a dry run")
		return nil
	}
	path := corpus.ReportPath(root, "paths")
	if err := report.Save(path, p.Markdown()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s\n", corpus.ReportPath("", "paths"))
	return nil
}
