package main

import (
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tamnd/arxiv-reader/site"
)

func runSite(args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return errors.New("usage: ax site build -out DIR [-url URL]")
	}
	fs := flag.NewFlagSet("ax site build", flag.ContinueOnError)
	out := fs.String("out", "", "the directory to write the site to, which must be empty or not exist")
	url := fs.String("url", "https://arxiv.tamnd.com", "the address of the site")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("ax site build needs -out")
	}
	root := corpusRoot()
	commit := ""
	if b, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(b))
	}
	st, err := site.Build(site.Options{Corpus: root, Out: *out, URL: *url, Commit: commit})
	if err != nil {
		return err
	}
	fmt.Printf("papers    %d\nwith text %d\nitems     %d\nfiles     %d\nbytes     %d\n", st.Papers, st.WithText, st.Items, st.Files, st.Bytes)
	if st.NoLicence > 0 {
		fmt.Printf("papers with no licence on record, shown as structure only: %d\n", st.NoLicence)
	}
	return nil
}
