package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/mdrec"
	"github.com/tamnd/arxiv-reader/scope"
	"github.com/tamnd/arxiv-reader/witness"
)

// runWitness is ax witness: the outside indexes of AX8.5 as census witnesses.
func runWitness(args []string) error {
	if len(args) == 0 || args[0] != "import" {
		return errors.New("usage: ax witness import -index theoremsearch|theoremgraph")
	}
	return witnessImport(args[1:])
}

// witnessImport reads an outside index and writes the witness records of the
// papers in scope into analysis/witness/<index>/. The remote files are read
// as a stream, or with range requests for a parquet file, and are not written
// to the disk.
func witnessImport(args []string) error {
	fs := flag.NewFlagSet("ax witness import", flag.ContinueOnError)
	index := fs.String("index", "", "the index: "+strings.Join(witness.Indexes, " or "))
	list := fs.String("scope", scope.Math, "keep the papers of this scope list")
	from := fs.String("from", "", "read the files of the index from this directory, and not from Hugging Face")
	revision := fs.String("revision", "", "the revision of the dataset, and the current one when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	src, ok := witness.Sources[*index]
	if !ok {
		return fmt.Errorf("%q is not an index, want %s", *index, strings.Join(witness.Indexes, " or "))
	}
	root := corpusRoot()
	in, err := scope.Read(scope.Path(root, *list))
	if err != nil {
		return fmt.Errorf("%w, so run ax select scope first", err)
	}
	keep := func(paper string) (string, bool) {
		id, err := axid.Parse(paper)
		if err != nil || !in[id.Canonical] {
			return "", false
		}
		return corpus.Shard(id), true
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	hub := &witness.Hub{UserAgent: userAgent()}
	imp := witness.Import{Index: *index, Dataset: src.Dataset, Revision: *revision, On: time.Now().UTC().Format("2006-01-02"), By: "ax/" + Version}
	if *from == "" && imp.Revision == "" {
		if imp.Revision, imp.Licence, err = hub.Revision(ctx, src.Dataset); err != nil {
			return err
		}
	}
	if *from != "" {
		imp.Revision = "local"
	}
	open := func(file string) (io.ReadCloser, error) {
		if *from != "" {
			return os.Open(filepath.Join(*from, file))
		}
		return hub.Open(ctx, hub.URL(src.Dataset, imp.Revision, file))
	}
	fmt.Fprintf(os.Stderr, "reading %s at %s\n", src.Dataset, imp.Revision)
	var res *witness.Result
	switch *index {
	case witness.TheoremSearch:
		var r io.ReaderAt
		var size int64
		if *from != "" {
			f, err := os.Open(filepath.Join(*from, src.Files[0]))
			if err != nil {
				return err
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				return err
			}
			r, size = f, st.Size()
		} else {
			rf, err := hub.ReaderAt(ctx, hub.URL(src.Dataset, imp.Revision, src.Files[0]))
			if err != nil {
				return err
			}
			r, size = rf, rf.Size
		}
		if res, err = witness.ReadTheoremSearch(r, size, keep); err != nil {
			return err
		}
	case witness.TheoremGraph:
		papers, err := open(src.Files[0])
		if err != nil {
			return err
		}
		defer papers.Close()
		statements, err := open(src.Files[1])
		if err != nil {
			return err
		}
		defer statements.Close()
		if res, err = witness.ReadTheoremGraph(papers, statements, keep); err != nil {
			return err
		}
	}
	dir := filepath.Join(root, "analysis", "witness", *index)
	old, err := mdrec.Glob(filepath.Join(dir, "*"+mdrec.Ext))
	if err != nil {
		return err
	}
	for shard, recs := range res.ByShard {
		if err := witness.Write(witness.Path(root, *index, shard), recs); err != nil {
			return err
		}
	}
	// A month that the new import does not have is a month that the old one
	// had, and its file goes, by its own name.
	for _, f := range old {
		if _, ok := res.ByShard[strings.TrimSuffix(filepath.Base(f), mdrec.Ext)]; !ok {
			for _, f := range []string{f, strings.TrimSuffix(f, mdrec.Ext) + mdrec.Old} {
				if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	imp.Read, imp.Kept, imp.Papers = res.Read, res.Kept, res.Papers()
	b, err := json.MarshalIndent(imp, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "import.json"), append(b, '\n')); err != nil {
		return err
	}
	fmt.Printf("%s: %d results read, %d kept for %d papers in scope over %d months\n", *index, res.Read, res.Kept, imp.Papers, len(res.ByShard))
	return nil
}
