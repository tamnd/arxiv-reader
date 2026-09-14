// Command ax builds and reads the arXiv corpus at github.com/tamnd/arxiv.
//
// The binary is called ax rather than arxiv because github.com/tamnd/arxiv-cli
// already installs a binary called arxiv and this tool depends on it. The name
// is also the URI space the corpus uses, which is ax://paper/2106.09685.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
)

// Version is set by the release build from the tag.
var Version = "dev"

// command is one top level verb.
//
// Milestone is the milestone the verb arrives in, from notes/Spec/2166/12-plan.
// A verb whose run is nil prints that milestone and exits non-zero, because a
// command that has not been written yet should say so rather than succeed
// quietly and leave the caller to work out why nothing happened.
type command struct {
	name      string
	summary   string
	milestone string
	run       func(args []string) error
}

var commands = []command{
	{"version", "print the version and exit", "M0", runVersion},
	{"id", "parse an arXiv reference and print what it resolves to", "M0", runID},
	{"licence", "the licence gate: census, show, explain, recheck", "M0", runLicence},
	{"harvest", "fill the metadata plane from Kaggle, Hugging Face and OAI-PMH", "M1", runHarvest},
	{"fetch", "download what a paper needs for the path it is on", "M3", runFetch},
	{"select", "choose what enters the content plane, and say why", "M5", nil},
	{"extract", "render, source, native or vision, to tagged Markdown", "M3", runExtract},
	{"figures", "crop, convert and size cap the figures", "M3", nil},
	{"tables", "keep every table twice, as Markdown and as LaTeX", "M3", nil},
	{"refs", "parse a bibliography and resolve it into the corpus", "M3", nil},
	{"split", "one file per top level section, with front matter", "M3", runSplit},
	{"tags", "assign, diff and verify the permanent identifiers", "M3", nil},
	{"objects", "the sixteen object kinds, results and artefacts", "M6", nil},
	{"graph", "build, query and verify the connected web", "M6", nil},
	{"glossary", "seed, extend and check the controlled vocabulary", "M7", nil},
	{"translate", "vi, zh and ja, and any language with a profile", "M8", nil},
	{"roundtrip", "back translate a sample and compare", "M8", nil},
	{"build", "the intermediate representation, then web, EPUB, TeX and PDF", "M6", nil},
	{"audit", "the numbered rules, hard and soft", "M1", runAudit},
	{"report", "coverage, usage, paths, graph and licence", "M5", nil},
	{"routes", "configure and probe the model fleet", "M8", nil},
	{"doctor", "probe every route and every rate limited surface", "M8", nil},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ax:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	case "-v", "--version":
		return runVersion(nil)
	}
	for _, c := range commands {
		if c.name != args[0] {
			continue
		}
		if c.run == nil {
			return fmt.Errorf("%s arrives in milestone %s and is not written yet", c.name, c.milestone)
		}
		return c.run(args[1:])
	}
	return fmt.Errorf("unknown command %q, run ax help for the command set", args[0])
}

func usage(w *os.File) {
	fmt.Fprintf(w, "ax %s builds and reads the arXiv corpus at github.com/tamnd/arxiv.\n\n", Version)
	fmt.Fprintln(w, "usage: ax <command> [arguments]")
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, c := range commands {
		state := c.milestone
		if c.run != nil {
			state = ""
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", c.name, c.summary, state)
	}
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A command with a milestone beside it has not been written yet and says so when called.")
	fmt.Fprintln(w, "The corpus is found through ARXIV_CORPUS, or the current directory when that is unset.")
}

func runVersion(_ []string) error {
	fmt.Println(Version)
	return nil
}

func runID(args []string) error {
	fs := flag.NewFlagSet("ax id", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 || rest[0] != "parse" {
		return errors.New("usage: ax id parse <reference>")
	}
	for _, ref := range rest[1:] {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		fmt.Printf("canonical  %s\n", id.Canonical)
		fmt.Printf("style      %s\n", id.Style)
		fmt.Printf("shard      %s\n", corpus.Shard(id))
		fmt.Printf("path       %s\n", corpus.PathID(id))
		fmt.Printf("uri        %s\n", id.URI())
		fmt.Printf("abs        %s\n", id.AbsURL())
	}
	return nil
}

func runLicence(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax licence explain <licence>")
	}
	switch args[0] {
	case "explain":
		if len(args) < 2 {
			return errors.New("usage: ax licence explain <licence>, or all")
		}
		if args[1] == "all" {
			for _, l := range corpus.Licences {
				if err := explain(l); err != nil {
					return err
				}
				fmt.Println()
			}
			return nil
		}
		l, err := corpus.ParseLicence(args[1])
		if err != nil {
			return fmt.Errorf("%w, one of %s", err, names())
		}
		return explain(l)
	case "census":
		return licenceCensus(args[1:])
	case "resolve":
		return licenceResolve(args[1:])
	case "crosscheck":
		return licenceCrosscheck(args[1:])
	case "show", "recheck":
		return fmt.Errorf("licence %s arrives in milestone M2 and is not written yet", args[0])
	default:
		return fmt.Errorf("unknown licence subcommand %q", args[0])
	}
}

func explain(l corpus.Licence) error {
	a := corpus.AccessFor(l)
	fmt.Printf("licence      %s\n", l)
	if url := l.URL(); url != "" {
		fmt.Printf("deed         %s\n", url)
	}
	fmt.Printf("access       %s\n", a)
	fmt.Printf("metadata     yes, always, arXiv publishes it under CC0\n")
	fmt.Printf("full text    %s\n", yesNo(a.MayPublishText()))
	fmt.Printf("figures      %s\n", yesNo(a.MayPublishText()))
	fmt.Printf("translation  %s\n", yesNo(a.MayTranslate()))
	if out, err := corpus.PublishedLicence(l); err == nil {
		fmt.Printf("our files    %s\n", out)
	} else {
		fmt.Printf("our files    nothing is published for this paper beyond its record\n")
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func names() string {
	out := make([]string, 0, len(corpus.Licences))
	for _, l := range corpus.Licences {
		out = append(out, string(l))
	}
	return strings.Join(out, ", ")
}
