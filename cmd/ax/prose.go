package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/cues"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/source"
)

// proseText is what ax prose prints: the running text of one version, for
// one run of the prose pass of the understand role (AX7.4 step 2).
type proseText struct {
	Paper    string         `json:"paper"`
	Version  int            `json:"version"`
	Text     bool           `json:"text"` // the licence class allows text in the corpus
	Sections []cues.Section `json:"sections"`
}

// runProse prints the running text of a version, section by section, with
// the cue sentences of AX7.4 step 1 marked. It reads the e-print that the
// pipeline cached in work/source, or the file that -eprint names, and never
// fetches from arXiv. It writes nothing: the text of the paper goes only to
// the model of one run, under the licence rules of AX5.4.
func runProse(args []string) error {
	fs := flag.NewFlagSet("ax prose", flag.ContinueOnError)
	eprint := fs.String("eprint", "", "the e-print file, instead of the one in work/source of the corpus")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ax prose [-eprint file] <id>v<n>")
	}
	id, version, err := splitVersion(fs.Arg(0))
	if err != nil {
		return err
	}
	file := *eprint
	if file == "" {
		pid, err := axid.Parse(id)
		if err != nil {
			return err
		}
		file = corpus.EPrintPath(corpusRoot(), pid, version)
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("%sv%d has no cached e-print at %s, and ax prose does not fetch it: %w", id, version, file, err)
		}
	}
	b, err := source.Read(file)
	if err != nil {
		return err
	}
	lic := licences{plane: metadata.Plane{Root: corpusRoot()}}
	text, _ := lic.text(id, version)
	res, err := analysis.Analyze(b, analysis.Paper{ID: id, Version: version, Extractor: "ax/" + Version, Text: text, Sections: true}, nil)
	if err != nil {
		return err
	}
	out := proseText{Paper: id, Version: version, Text: text, Sections: res.Sections}
	if out.Sections == nil {
		out.Sections = []cues.Section{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
