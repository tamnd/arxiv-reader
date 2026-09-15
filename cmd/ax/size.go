package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/arxiv-reader/size"
)

// runSize weighs the checkout against the split trigger.
//
// The spec says the content plane splits by year once a checkout goes over
// 20 GB or a clone goes over fifteen minutes, whichever comes first. This is
// the part of that plan that can be built before the split itself: something
// that says where the corpus stands, which year would move first, and roughly
// how many more papers fit. The split needs a shard aware corpus layout and a
// second repository and neither of those is worth building against a number
// nobody has measured.
//
// There is no report file. A committed report of the checkout size changes the
// checkout size, and the clone half of the answer belongs to whoever is doing
// the cloning rather than to the corpus.
func runSize(args []string) error {
	fs := flag.NewFlagSet("ax size", flag.ContinueOnError)
	hard := fs.Bool("hard", false, "exit non-zero once either half of the trigger has been reached")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: ax size [-hard]")
	}
	root := corpusRoot()
	c, err := size.Measure(root)
	if err != nil {
		return err
	}
	fmt.Printf("corpus  %s\n\n", root)
	fmt.Print(c.Text())
	tripped, half := c.Tripped()
	if !tripped || !*hard {
		return nil
	}
	fmt.Fprintln(os.Stderr, "the plan for this is in notes/Spec/2166/02-corpus.md, which splits the content plane by year into tamnd/arxiv-20NN")
	return fmt.Errorf("%s has reached the split trigger", half)
}
