package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/tamnd/arxiv-reader/source"
	texfe "github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// runTex runs the TeX front end on an e-print and prints what it found.
//
// It is the tool for a person who reads one paper and wants to see the steps
// of AX3.1, the declarations of AX3.2 and the manual heads of AX3.4. It writes
// nothing to the corpus.
func runTex(args []string) error {
	fs := flag.NewFlagSet("ax tex", flag.ContinueOnError)
	tree := fs.Bool("tree", false, "print the sections and the environments too")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ax tex [-tree] <e-print file>")
	}
	b, err := source.Read(fs.Arg(0))
	if err != nil {
		return err
	}
	docs, err := texfe.Read(b)
	if err != nil {
		return err
	}
	r := theorems.NewResolver()
	for _, d := range docs {
		fmt.Printf("main      %s\n", d.Main)
		fmt.Printf("class     %s %v\n", d.Class, d.Options)
		fmt.Printf("packages  %v\n", d.Packages)
		fmt.Printf("missing   %v\n", d.Missing)
		for _, s := range d.Steps {
			fmt.Printf("step      %s\n", s)
		}
		set := r.Resolve(d)
		for _, decl := range set.List() {
			fmt.Printf("env       %-16s %-14s title=%q counter=%s numbered=%v form=%s\n", decl.Env, decl.Item, decl.Title, decl.Counter, decl.Numbered, decl.Form)
		}
		for _, h := range theorems.FindHeads(d, r.Titles, nil) {
			p := d.Pos(h.Off)
			fmt.Printf("head      %s:%d %s %q number=%q %s\n", p.File, p.Line, h.Item, h.Title, h.Number, h.Confidence())
		}
		if *tree {
			for _, n := range d.Tree.Nodes {
				p := d.Pos(n.From)
				fmt.Printf("node      %s:%d %s %s %q\n", p.File, p.Line, n.Kind, n.Name, n.Title)
			}
		}
	}
	return nil
}
