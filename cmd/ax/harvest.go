package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-reader/harvest"
	"github.com/tamnd/arxiv-reader/metadata"
)

// corpusRoot is the directory ax reads and writes.
//
// ARXIV_CORPUS, or the working directory when that is unset. One environment
// variable and no config file, because the only thing a config file would hold
// is this path and a path that can be in two places is a path that will be.
func corpusRoot() string {
	if root := os.Getenv("ARXIV_CORPUS"); root != "" {
		return root
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func runHarvest(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax harvest <kaggle|hf|oai|status|report>")
	}
	switch args[0] {
	case "status":
		return harvestStatus(args[1:])
	case "report":
		return harvestReport(args[1:])
	case "oai":
		return harvestOAI(args[1:])
	case "kaggle":
		return harvestSnapshot(metadata.SourceKaggle, args[1:])
	case "hf":
		return harvestHF(args[1:])
	default:
		return fmt.Errorf("unknown harvest subcommand %q", args[0])
	}
}

// harvestSnapshot reads the Cornell metadata snapshot into the metadata plane.
//
// This is the bootstrap: 3.17 million records in an afternoon rather than a
// week of polite paging at OAI-PMH. The file is not downloaded here, because
// Kaggle wants a login and a login is not something a build tool should be
// holding. Fetch it with the kaggle client, or from a Hugging Face mirror, and
// point this at the file.
func harvestSnapshot(source metadata.Source, args []string) error {
	fs := flag.NewFlagSet("ax harvest "+string(source), flag.ContinueOnError)
	dry := fs.Bool("n", false, "read and report, but write nothing")
	quiet := fs.Bool("q", false, "no progress")
	batch := fs.Int("batch", 200000, "records to hold before writing a round of months")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if err := onlyTheFile(rest); err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: ax harvest %s [-n] [-q] <arxiv-metadata-oai-snapshot.json>", source)
	}
	return readSnapshotFile(source, rest[0], *dry, *quiet, *batch)
}

// readSnapshotFile is the file half of a snapshot harvest, shared by kaggle and
// by hf because the bytes are the same and only the source differs.
func readSnapshotFile(source metadata.Source, path string, dry, quiet bool, batch int) error {
	if batch < 1 {
		return errors.New("-batch has to be at least 1")
	}

	// Archives are not unpacked here on purpose. The file arrives as a zip from
	// Kaggle and as parquet from most Hugging Face mirrors, and guessing at
	// container formats is how a tool ends up with four of them and a bug in
	// each. Unpack it first.
	if strings.HasSuffix(path, ".gz") || strings.HasSuffix(path, ".zip") {
		return fmt.Errorf("%s is still packed, and this reads the JSON inside it: unpack it first", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := harvest.Snapshot{
		Source: source,
		Now:    time.Now,
	}
	if !quiet {
		reader.Log = func(read, skipped int) {
			fmt.Fprintf(os.Stderr, "%d records read\n", read)
		}
	}

	b := newBatcher(metadata.Plane{Root: corpusRoot()}, batch, dry)
	read, err := reader.Read(f, b.add)
	// Flushed first, for the reason given in harvestHF: a bad line three
	// million records into a five gigabyte file should not throw away the three
	// million good ones in front of it.
	if ferr := b.flush(); ferr != nil {
		return ferr
	}
	if err != nil {
		if !dry && read.Read > 0 {
			fmt.Fprintf(os.Stderr, "%s, written\n", b.stats)
		}
		return err
	}

	if dry {
		fmt.Printf("%s, nothing written\n", read)
		return nil
	}
	fmt.Printf("%s from %s\n", b.stats, read)
	if read.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "%d lines had an id this tool cannot place\n", read.Skipped)
	}
	return nil
}

// harvestHF reads the same records off Hugging Face.
//
// The fallback, and it has two ways in because the mirrors are not consistent.
// Given a JSON lines file it reads the file, which is the Kaggle path with the
// source changed. Given -rows it walks the datasets server instead, which is
// the only surface in this whole tool that needs neither a login nor a five
// gigabyte download, and the only one that will serve a dataset kept as
// parquet.
func harvestHF(args []string) error {
	fs := flag.NewFlagSet("ax harvest hf", flag.ContinueOnError)
	useRows := fs.Bool("rows", false, "read the datasets server instead of a file")
	dataset := fs.String("dataset", harvest.Mirror, "the mirror to read, as owner/name")
	offset := fs.Int("offset", 0, "row to start at, which is a resume point and not a date")
	limit := fs.Int("limit", 0, "stop after about this many rows, rounded up to the end of a page")
	all := fs.Int("all", 0, "read the whole split, which is this many rows and hours of requests")
	dry := fs.Bool("n", false, "read and report, but write nothing")
	quiet := fs.Bool("q", false, "no progress")
	batch := fs.Int("batch", 200000, "records to hold before writing a round of months")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := onlyTheFile(fs.Args()); err != nil {
		return err
	}
	if !*useRows {
		// A flag that belongs to the other way in is a mistake worth naming.
		// Ignoring it would run a harvest that quietly did something other than
		// what was asked for, which on a command that writes to the corpus is
		// the wrong kind of forgiving.
		for _, name := range []string{"dataset", "offset", "limit", "all"} {
			if set(fs, name) {
				return fmt.Errorf("-%s only means something with -rows, and this is reading a file", name)
			}
		}
		// No -rows and no file is the one case with nothing to do, and the
		// message has to name both ways in or the second one is undiscoverable.
		if len(fs.Args()) != 1 {
			return errors.New("usage: ax harvest hf [-n] [-q] <arxiv-metadata-oai-snapshot.json>, or ax harvest hf -rows -limit N to read the datasets server")
		}
		return readSnapshotFile(metadata.SourceHF, fs.Args()[0], *dry, *quiet, *batch)
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("-rows reads the datasets server, so it takes no file: drop %s or drop -rows", fs.Args()[0])
	}
	if *batch < 1 {
		return errors.New("-batch has to be at least 1")
	}
	if *offset < 0 {
		return errors.New("-offset has to be at least 0")
	}
	// The split is about 3.17 million rows and the page is a hundred, so a full
	// walk is over thirty thousand requests and a day of them. That should
	// be a number somebody typed rather than what happens when a flag is
	// forgotten, which is what -all is: it asks for the count out loud.
	if *limit == 0 && *all == 0 {
		return errors.New("a rows harvest with no -limit walks the whole split, which is over thirty thousand requests and most of a day, so say -all <rows> if that is what you meant")
	}
	if *limit == 0 {
		*limit = *all
	}

	client := &harvest.Rows{
		Dataset: *dataset,
		Now:     time.Now,
		// Hugging Face does not block anonymous readers the way arXiv does, but
		// naming the project costs nothing and means somebody who wants this to
		// stop has a person to write to.
		UserAgent: "arxiv-reader/" + Version + " (+https://github.com/tamnd/arxiv-reader; tamnd87@gmail.com)",
	}
	if !*quiet {
		client.Log = func(page, records, total int) {
			fmt.Fprintf(os.Stderr, "page %d, %d of %d rows\n", page, records, total)
		}
	}

	b := newBatcher(metadata.Plane{Root: corpusRoot()}, *batch, *dry)
	read, err := client.List(context.Background(), harvest.RowsQuery{Offset: *offset, Limit: *limit}, b.add)
	// Flushed before the error is returned, and that ordering is the point.
	// A walk of this length will be stopped by the far end sooner or later, and
	// throwing away four thousand rows that arrived fine because the four
	// thousand and first did not is how an interrupted harvest becomes an
	// afternoon of repeating work. What was read is written and the message
	// says where to pick it up.
	if ferr := b.flush(); ferr != nil {
		return ferr
	}
	if err != nil {
		if !*dry && read.Read > 0 {
			fmt.Fprintf(os.Stderr, "%s, written: resume with -offset %d\n", b.stats, *offset+read.Read+read.Skipped)
		}
		return err
	}

	if *dry {
		fmt.Printf("%s, nothing written\n", read)
		return nil
	}
	fmt.Printf("%s from %s\n", b.stats, read)
	if read.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "%d rows were truncated or had an id this tool cannot place\n", read.Skipped)
	}
	return nil
}

// onlyTheFile catches a flag written after the file name.
//
// Go's flag package stops looking for flags at the first argument that is not
// one, so "ax harvest kaggle big.json -q" leaves -q as a second file name and
// the harvest runs loud. That is a small thing to get wrong and a confusing one
// to be told about as a usage line, so it gets its own sentence.
func onlyTheFile(rest []string) error {
	for _, arg := range rest[min(1, len(rest)):] {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("%s came after the file name, and flags have to come before it", arg)
		}
	}
	return nil
}

// set reports whether a flag was given on the command line, as opposed to
// sitting at its default.
func set(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// batcher holds records until there are enough of them to be worth writing.
//
// Batched rather than all at once, because the whole snapshot is 3.17 million
// records and holding the parsed form of it costs more memory than most
// machines have. Records accumulate until the batch is full, then every month
// they touched is written and the map is dropped.
//
// Batched rather than one month at a time, too: the file is in identifier order
// and not in month order, so a paper from 1998 turns up next to one from 2024
// and writing per record would rewrite every month file three million times.
type batcher struct {
	plane   metadata.Plane
	size    int
	dry     bool
	pending map[string][]metadata.Record
	held    int
	stats   metadata.Stats
}

func newBatcher(plane metadata.Plane, size int, dry bool) *batcher {
	return &batcher{plane: plane, size: size, dry: dry, pending: map[string][]metadata.Record{}}
}

func (b *batcher) add(r metadata.Record) error {
	shard, err := r.Shard()
	if err != nil {
		return err
	}
	b.pending[shard] = append(b.pending[shard], r)
	b.held++
	if b.held >= b.size {
		return b.flush()
	}
	return nil
}

func (b *batcher) flush() error {
	if b.dry || b.held == 0 {
		b.pending = map[string][]metadata.Record{}
		b.held = 0
		return nil
	}
	// Sorted, so that two runs of the same input write the same months in the
	// same order and a corpus can be compared against itself.
	shards := make([]string, 0, len(b.pending))
	for shard := range b.pending {
		shards = append(shards, shard)
	}
	sort.Strings(shards)
	for _, shard := range shards {
		s, err := b.plane.Merge(shard, b.pending[shard])
		if err != nil {
			return fmt.Errorf("%s: %w", shard, err)
		}
		b.stats = b.stats.Add(s)
	}
	b.pending = map[string][]metadata.Record{}
	b.held = 0
	return nil
}

// harvestOAI reads arXiv's own endpoint into the metadata plane.
//
// This is the catch up rather than the bootstrap. OAI-PMH will serve the whole
// archive, but at three seconds a page and about a thousand records a page that
// is most of a week of continuous requests for the first pass. The Kaggle
// snapshot does the same job in an afternoon, and then this command keeps the
// plane current from the day the snapshot was cut.
func harvestOAI(args []string) error {
	fs := flag.NewFlagSet("ax harvest oai", flag.ContinueOnError)
	from := fs.String("from", "", "earliest datestamp to ask for, as YYYY-MM-DD")
	until := fs.String("until", "", "latest datestamp to ask for, as YYYY-MM-DD")
	format := fs.String("format", harvest.FormatRaw, "arXivRaw for the record, arXiv for an authors pass over it")
	set := fs.String("set", "", "restrict to one OAI set, so math or cs")
	limit := fs.Int("limit", 0, "stop after about this many records, rounded up to the end of a page")
	dry := fs.Bool("n", false, "fetch and report, but write nothing")
	quiet := fs.Bool("q", false, "no per page progress")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := harvest.Query{
		Format: *format,
		From:   *from,
		Until:  *until,
		Set:    *set,
		Limit:  *limit,
	}
	if err := query.Validate(); err != nil {
		return err
	}
	// The datestamp is when arXiv last touched the record and not when the
	// paper was submitted, so an unbounded run asks for the whole archive. That
	// is a week of requests and it should be something a person typed on
	// purpose.
	if query.From == "" && query.Until == "" && query.Limit == 0 {
		return errors.New("a harvest with no -from, no -until and no -limit asks for the whole archive, which is about a week of requests, so say -from 1991-01-01 if that is what you meant")
	}

	client := &harvest.OAI{
		// arXiv blocks anonymous bulk readers and they are right to. The
		// address is here so that someone at Cornell who wants this to stop has
		// somebody to write to before they reach for a block.
		UserAgent: "arxiv-reader/" + Version + " (+https://github.com/tamnd/arxiv-reader; tamnd87@gmail.com)",
	}
	if !*quiet {
		client.Log = func(page, records int, token string) {
			more := ""
			if token != "" {
				more = ", more to come"
			}
			fmt.Fprintf(os.Stderr, "page %d, %d records%s\n", page, records, more)
		}
	}

	plane := metadata.Plane{Root: corpusRoot()}
	var records []metadata.Record
	err := client.List(context.Background(), query, func(r metadata.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		return err
	}

	// An id this tool cannot place is a record with nowhere to go. Reported
	// rather than fatal, because one bad id in a page of a thousand should not
	// throw the other nine hundred and ninety nine away.
	byShard, unplaceable := metadata.Group(records)
	shards := make([]string, 0, len(byShard))
	for shard := range byShard {
		shards = append(shards, shard)
	}
	sort.Strings(shards)

	if *dry {
		fmt.Printf("%d records over %d months, nothing written\n", len(records), len(shards))
		return reportUnplaceable(unplaceable)
	}

	var stats metadata.Stats
	for _, shard := range shards {
		recs := byShard[shard]
		// The arXiv format has no title, no abstract and no version history, so
		// a record it is the first to mention cannot be written: there is not
		// enough of it to be a record. It updates papers the plane already has
		// and leaves the rest for a raw pass, which is what makes it an authors
		// pass rather than a harvest in its own right.
		if query.Format == harvest.FormatArXiv {
			var err error
			recs, err = onlyKnown(plane, shard, recs)
			if err != nil {
				return fmt.Errorf("%s: %w", shard, err)
			}
		}
		s, err := plane.Merge(shard, recs)
		if err != nil {
			return fmt.Errorf("%s: %w", shard, err)
		}
		stats = stats.Add(s)
	}
	fmt.Printf("%s over %d months\n", stats, len(shards))
	return reportUnplaceable(unplaceable)
}

// onlyKnown drops the records for papers this month does not already hold.
func onlyKnown(plane metadata.Plane, shard string, recs []metadata.Record) ([]metadata.Record, error) {
	existing, err := plane.Read(shard)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(existing))
	for _, r := range existing {
		known[r.ID] = true
	}
	out := recs[:0]
	for _, r := range recs {
		if known[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func reportUnplaceable(recs []metadata.Record) error {
	if len(recs) == 0 {
		return nil
	}
	ids := make([]string, len(recs))
	for i, r := range recs {
		ids[i] = r.ID
	}
	// A warning and not an error. The run did what it could and the operator
	// needs to know which ids to look at by hand.
	fmt.Fprintf(os.Stderr, "%d records had an id this tool cannot place: %s\n", len(ids), strings.Join(ids, " "))
	return nil
}

// harvestStatus says what the metadata plane holds, a month at a time.
//
// The first thing worth having, because every later question about the plane is
// a question about a gap in this table. A month with no file is a month nobody
// harvested, and the whole of M1 is the work of making that list empty.
func harvestStatus(args []string) error {
	fs := flag.NewFlagSet("ax harvest status", flag.ContinueOnError)
	total := fs.Bool("total", false, "print the record count and nothing else")
	if err := fs.Parse(args); err != nil {
		return err
	}
	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}

	counts := make([]int, len(shards))
	sum := 0
	for i, shard := range shards {
		n, err := plane.Count(shard)
		if err != nil {
			return err
		}
		counts[i] = n
		sum += n
	}
	if *total {
		fmt.Println(sum)
		return nil
	}
	if len(shards) == 0 {
		fmt.Printf("the metadata plane at %s is empty\n", plane.Dir())
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', tabwriter.AlignRight)
	for i, shard := range shards {
		fmt.Fprintf(tw, "%s\t%d\t\n", shard, counts[i])
	}
	fmt.Fprintf(tw, "%d months\t%d\t\n", len(shards), sum)
	return tw.Flush()
}

// harvestReport writes reports/harvest.md, which is the corpus's count per
// month set against arXiv's own.
//
// This is the only number in the project that arXiv produced rather than this
// tool, and that is what makes it worth having. A harvest that stopped early
// cannot tell from the inside: a month it never read and a month with nothing
// in it are the same empty file. Comparing against a count from outside is the
// only way the difference shows up.
func harvestReport(args []string) error {
	fs := flag.NewFlagSet("ax harvest report", flag.ContinueOnError)
	fetch := fs.Bool("fetch", false, "read arXiv's counts live instead of the committed copy")
	save := fs.String("save", "", "write the counts read with -fetch to this file, for committing")
	out := fs.String("o", "", "where to write the report, and the default is reports/harvest.md in the corpus")
	quiet := fs.Bool("q", false, "write the file and print nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax harvest report takes no arguments, so drop %s", fs.Args()[0])
	}
	if *save != "" && !*fetch {
		return errors.New("-save writes what -fetch read, so it needs -fetch")
	}

	published, source, err := publishedCounts(*fetch, *save)
	if err != nil {
		return err
	}

	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}
	held := make(map[string]int, len(shards))
	for _, shard := range shards {
		n, err := plane.Count(shard)
		if err != nil {
			return err
		}
		held[shard] = n
	}

	coverage := harvest.Compare(held, published)
	coverage.Generated = time.Now()
	coverage.Source = source

	path := *out
	if path == "" {
		path = filepath.Join(plane.Root, "reports", "harvest.md")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(coverage.Markdown()), 0o644); err != nil {
		return err
	}
	if !*quiet {
		fmt.Print(coverage.Text())
		fmt.Fprintf(os.Stderr, "written to %s\n", path)
	}
	return nil
}

// publishedCounts is arXiv's side of the comparison, and says where it came
// from so the report can record it.
func publishedCounts(fetch bool, save string) ([]harvest.Published, string, error) {
	if !fetch {
		stats, err := harvest.PublishedStats()
		return stats, "arXiv's published monthly submissions, from the copy committed with this tool", err
	}
	agent := fmt.Sprintf("arxiv-reader/%s (+https://github.com/tamnd/arxiv-reader)", Version)
	stats, body, err := harvest.FetchStats(context.Background(), nil, agent)
	if err != nil {
		return nil, "", err
	}
	if save != "" {
		if err := os.MkdirAll(filepath.Dir(save), 0o755); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(save, []byte(body), 0o644); err != nil {
			return nil, "", err
		}
		fmt.Fprintf(os.Stderr, "%d months written to %s\n", len(stats), save)
	}
	return stats, "arXiv's published monthly submissions, read live from " + harvest.StatsURL, nil
}
