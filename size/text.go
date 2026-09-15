package size

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-reader/internal/prose"
)

// Text is what the command prints.
//
// Four parts, and each one answers a question somebody actually has. What is in
// the repository, where the two halves of the trigger stand, what a split would
// take off first, and how much room is left at the current cost per paper.
func (c Corpus) Text() string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, p := range c.Planes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, prose.Bytes(p.Bytes), prose.Percent(share(p.Bytes, c.Checkout)), prose.Count(p.Files, "file"))
	}
	if len(c.Planes) == 0 {
		fmt.Fprintf(tw, "nothing\t\t\tno plane of this corpus has a file in it\n")
	}
	fmt.Fprintf(tw, "\t\t\t\n")
	fmt.Fprintf(tw, "checkout\t%s\t%s\tof the %s trigger\n", prose.Bytes(c.Checkout), prose.Percent(c.Share()), prose.Bytes(Trigger))
	fmt.Fprintf(tw, "objects\t%s\t\twhat a clone transfers\n", prose.Bytes(c.Objects))
	fmt.Fprintf(tw, "clone\t%s\t%s\tof the %s trigger at %s a second\n", c.Clone(), prose.Percent(c.CloneShare()), CloneTrigger, prose.Bytes(Rate))
	tw.Flush()

	fmt.Fprintln(&b)
	if tripped, half := c.Tripped(); tripped {
		fmt.Fprintf(&b, "%s has reached the trigger, so the content plane splits by year now.\n", strings.ToUpper(half[:1])+half[1:])
	} else {
		half, share := c.Nearest()
		fmt.Fprintf(&b, "%s is the nearer half at %s of the trigger, and neither half has been reached.\n", strings.ToUpper(half[:1])+half[1:], prose.Percent(share))
	}

	if len(c.Years) > 0 {
		fmt.Fprintln(&b)
		yw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, y := range c.Years {
			fmt.Fprintf(yw, "%s\t%s\t%s\n", y.Year, prose.Bytes(y.Bytes), prose.Count(y.Papers, "paper"))
		}
		yw.Flush()
		first, _ := c.First()
		fmt.Fprintf(&b, "\nSplitting %s off first would take %s out of the checkout, leaving %s.\n", first.Year, prose.Bytes(first.Bytes), prose.Bytes(c.Checkout-first.Bytes))
	}

	fmt.Fprintln(&b)
	if c.Papers == 0 {
		fmt.Fprintln(&b, "The content plane holds no papers, so there is no cost per paper to project from yet.")
		return b.String()
	}
	fmt.Fprintf(&b, "%s in the content plane at %s each, so about %s more fit before the checkout trigger.\n", prose.Count(c.Papers, "paper"), prose.Bytes(c.PerPaper()), prose.Thousands(int(c.Headroom())))
	if fixed := c.Checkout - c.Scaling; fixed > 0 {
		fmt.Fprintf(&b, "The %s each is the %s that grows with the selection and leaves out the %s of metadata and reports, which is the same size whatever is selected.\n", prose.Bytes(c.PerPaper()), prose.Bytes(c.Scaling), prose.Bytes(fixed))
	}
	fmt.Fprintln(&b, "It is the cost of the papers selected so far and not a forecast, since older papers are shorter and every language of a paper is another copy of it.")
	return b.String()
}
