# arxiv-reader

The toolchain that builds [tamnd/arxiv](https://github.com/tamnd/arxiv): harvest arXiv's metadata, extract a paper into tagged Markdown, translate it, connect it to the rest of the corpus, and publish the result as web, EPUB, TeX and PDF.

The binary is called `ax`.
It is not called `arxiv` because [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli) already installs a binary by that name and this tool depends on it.
`ax` is also the URI space the corpus uses, which is `ax://paper/2106.09685`.

## Status

M4, which is the source path: the papers arXiv never rendered, which is everything announced before December 2023.
M3 before it was one paper end to end down the render path, and the seed paper is Mamba.
M2 before that was the licence census: counting what the corpus is permitted to do with what it holds.
M1 before that was the metadata plane: the harvest, the writer, the rules that check it and the report that sets it against arXiv's own numbers.
M0 before that was the module, the licence gate and the command set.
Every other command names the milestone it arrives in and exits non-zero rather than pretending to succeed.
The plan is in the issues, one per milestone.

```
$ ax harvest hf -rows -limit 6000
$ ax audit --plane meta
6000 records over 126 months, nothing found
$ ax harvest report
6,000 records of 3,163,381 records announced, 0.2%
126 months held, 423 months announced
$ ax licence census
Of 6,000 papers counted, 2,863 may be translated and 3,185 may be republished in English, both as upper bounds.
  cc0                    83    1.4%  open
  cc-by               2,464   41.1%  open
  cc-by-sa              153    2.5%  share-alike
  cc-by-nc-sa           163    2.7%  share-alike
  cc-by-nc-nd           322    5.4%  verbatim
  arxiv-1.0           2,815   46.9%  record
```

Those two numbers are upper bounds and the report says so in its first sentence.
Every surface that serves arXiv metadata in bulk carries one licence per paper rather than one per version, and the one it carries is the latest version's.
Here is what that looks like on one paper.

```
$ ax licence resolve -all 2203.09431
2203.09431v1           arxiv-1.0    record
2203.09431v2           arxiv-1.0    record
2203.09431v3           arxiv-1.0    record
2203.09431v4           arxiv-1.0    record
2203.09431v5           cc-by-sa     share-alike
```

The mirror says cc-by-sa, once, for the paper.
Four of its five versions were never offered under it, and translating any of those four on the strength of the mirror would be republishing something arXiv was given no right to let us republish.
A sample of forty multi-version papers taken in September 2026 found five whose v1 licence differs from their latest, and four of those five ran in this direction.
Roughly two fifths of arXiv has more than one version, so the overstatement is on the order of five percent of the corpus.

Resolving it properly is one page per version, which is over five million pages at the fifteen seconds arXiv asks for on the website, so `ax licence resolve` runs per paper when a paper is selected rather than over the whole archive.
`-write` puts what it read back on the record, with the abs page named as the authority, which is the only authority the content plane will accept.

The third thing M2 does is set this corpus against somebody else's reading of the same question.
The Common Pile publishes 75,747 arXiv papers in full, which means somebody there read arXiv's licence field and decided what they were allowed to republish.

```
$ ax licence crosscheck -limit 1200
Of 21 papers read and also held here, all agree, and 1,179 are absent from the metadata plane.
  rows read                       1,200
  claims                          1,200
  held here                          21
  agreed                             21
  disagreed                           0
  of those, costs something           0
  absent here                     1,179
```

That agreement is worth less than it looks, and the report says so before it says what it found.
Their identifiers carry no version either, so both sides are reading one paper level licence and calling it the paper's, and two readings of the same upstream field can be wrong the same way twice.
The disagreements are the whole of the signal.
The one that costs something is where their reading permits republishing the English and ours does not, because they have already published it.
The 1,179 absent are a gap in the harvest and not a licence question: that run was against a plane of 9,242 records, and a paper they hold that this corpus has never heard of says the harvest is short rather than that anybody read a licence wrong.

All of that is counting, and `ax fetch` is the first command that acts on the count.

```
$ ax fetch render -all 2312.00752
fetching 2312.00752v1
fetch: arXiv has no HTML rendering of 2312.00752v1 (404 Not Found), which is normal before December 2023 and for submissions with no TeX source, so this paper is on the source path
fetching 2312.00752v2
2312.00752v2           fetched  cc-by           668209  work/html/2312/2312.00752v2.html
1 fetched, 0 cached, 1 with no rendering, 1 sources in manifests/sources.yaml
```

The licence gate runs before the request and not after it.
A version nobody has read a licence for is refused, a version whose licence came from Kaggle or Hugging Face or OAI-PMH is refused because those state one licence for the whole paper, and a version under arXiv's default licence is refused because none of its content may be republished.
All three refusals happen before anything is asked for, which is the point: a refusal that arrives after fifteen seconds and a download has already spent the thing it was meant to save.

What gets committed is the manifest and not the bytes.

```
$ ax fetch verify
2312.00752v2  render  ok  db9c91f06ddc
1 sources, 1 ok, 0 missing, 0 changed
```

A rendering is a few hundred kilobytes and a corpus that committed one per paper would be a mirror of arXiv rather than a reading of it, so `work/` is gitignored and `manifests/sources.yaml` records the URL, the hash, the size, the hour and the licence that was in force.
That makes a fetch idempotent and hash checked, in that order.
A file already on disk whose hash matches is returned without a request, which is what makes re-running an interrupted batch cheap.
A file whose hash does not match is a finding and never an overwrite, because a source that moved under a paper the corpus has already extracted is exactly the event the manifest exists to catch.

The rendering only exists for papers announced from December 2023 onward and arXiv does not backfill, so most of the archive has no rendering at all and the fallback is the submission itself.
`ax fetch source` reads that surface through the same gate, the same pace and the same manifest, and what comes back is one gzip stream.

```
$ ax fetch source 2006.10256v1 1710.05832v1
fetching 2006.10256v1
2006.10256v1           fetched  cc-by          1385395  work/source/2006/2006.10256v1.gz
                       paper.tex of 5 files
fetching 1710.05832v1
1710.05832v1           fetched  cc-by          1599186  work/source/1710/1710.05832v1.gz
  source: this submission is a PDF of 1599186 bytes and not TeX, which arXiv accepts and which leaves no source to read, so the paper is on the native path
2 fetched, 0 cached, 0 arXiv does not serve, 2 sources in manifests/sources.yaml
```

Those two are the NumPy paper and the second LIGO detection, and between them they are what a submission turns out to be.
One is a tar of five files with the document in `paper.tex`, and the other is a PDF the authors produced themselves, which arXiv accepts and which leaves nothing to read as source.
Nothing announces which of the two arrived, so it is worked out from the bytes, and it is worked out as soon as they land rather than at extraction time, because somebody fetching a hundred papers wants to know which of them have no source before the extraction starts rather than after.

Which file the document starts from is the other question a tarball asks, and it is four passes in the order of how much they are worth believing.
A `00README` naming a top level file is the first, because the submitter saying so outright is the only answer that is not a guess.
Then the files that hold both a preamble and a body, which answers most submissions on its own.
Then the include graph, which removes the chapter files of a paper whose sections each compile alone.
Then the names people give a main file, which is a convention and is treated as one.
A submission that gets through all four with two candidates left is usually two papers in one upload, and the answer to that is somebody naming the file rather than this tool picking.

Choosing the sample papers for this path meant reading licences first, and what that reading found is worth writing down.
Of eight famous machine learning preprints checked in September 2026, being Attention Is All You Need, ResNet, GANs, VAEs, GPT-3, ViT, BERT and DDPM, all eight are under arXiv's own nonexclusive licence, so this corpus may hold their metadata, their structure and their tags and none of their text.
The famous papers that are CC-BY are elsewhere, and the NumPy paper and the two LIGO detections are three of them.

Once a rendering is on disk, `ax extract` reads it.

```
$ ax extract render -n 2312.00752v2
2312.00752v2  arXiv:2312.00752v2 [cs.LG] 31 May 2024
  licence     CC BY 4.0
  title       Mamba: Linear-Time Sequence Modeling with Selective State Spaces
  authors     Albert Gu, Tri Dao
  abstract    1 block
  headings    111
  blocks      2 algorithm, 26 equation, 32 figure, 23 list, 2 listing, 201 paragraph, 1 proof, 8 table, 3 theorem
  unparsed    18
  faults      none
```

The render path costs one HTTP GET and no model call, and it gives back more than text.
Every piece of mathematics in arXiv's rendering carries an `alttext` attribute holding the LaTeX the author typed, and the MathML beside it is only what a browser draws, so the corpus keeps the LaTeX and throws the MathML away and regenerates it at build time with KaTeX.
Figures keep their captions, their numbers and their files, including the SVGs that arrive as an `object` rather than an `img`.
Tables keep their cells, their headings, their alignment and their spans.
Theorems keep whatever the author called them, so a paper full of claims and remarks reads as one rather than as a column of the word theorem.
The bibliography keeps the anchor every citation in the paper points at, so a link in the text and an entry in the reference list stay connected.
`-outline` prints the whole document, heading by heading and block by block.

Reading and writing are separate commands because the reject rule sits between them.

```
$ ax extract render -n 2501.00001v3
  faults      1, 1 of them inside the body of the paper
2501.00001v3: extract: the rendering has 1 conversion error inside the body of the paper, so a piece of the paper is missing
  at S1: \nothingmacro
```

LaTeXML leaves an error marker in place of anything it could not read.
One in a bibliography entry is a reference that prints badly, and about a quarter of arXiv's conversions carry at least one of those, so rejecting on the first would throw away most of the corpus to fix a typo in a citation.
One inside a section body is a sentence of the paper that is gone, and that is a rejection however many there are.
A rejected paper is not a failed paper: it falls through to the source path, where LaTeXML runs here with this project's flags rather than arXiv's, and a good share of the errors do not happen twice.

Drop the `-n` and the same command writes the paper into the content plane, one file per top level section.

```
$ ax extract render 2312.00752v2
  written  00_front.md
  written  01_introduction.md
  ...
12 files in content/en/2312/2312.00752: 12 written, 0 unchanged, 0 removed, 0 left alone
```

Every file opens with front matter holding what the metadata plane knows about the paper and what the emitter counted in the file beside it, and then the section itself.
A section that is a whole file has its title in `section_title` rather than as a heading, because a page with its title in its metadata and again in its body has two titles.

```
section: 3
section_title: Selective State Space Models
local_id: s3
objects: 28
figures:
  - fig-2
  - fig-3
statements:
  - thm-1
content_sha256: d85d21a42207b36648adfa35e74e3f3a678c870a57057c75068e85697a4b14fd
edited: false
```

The body is Markdown with an attribute block on the line each object starts, so `**Theorem 1** {#thm-1 .statement env=theorem}` is a theorem a link can point at.
The identifiers are local to the paper and readable, which is `thm-1` and `fig-2` and `s3-1`, and the permanent four character tags that survive a re-extraction are assigned later by `ax tags assign`.
arXiv's rendering names the same objects `S3.F2` and `S3.SS1`, which mean nothing outside the one HTML file they came from, so every cross reference is rewritten once the whole paper has been walked.
A forward reference in section 1 to a figure in section 4 cannot be resolved before section 4 has been named, which is why nothing is written until everything has been read.
A reference that cannot be placed is left exactly as it was, since a link that works and goes somewhere wrong is worse than one that visibly does not.

Writing is idempotent and a correction outranks the converter.

```
$ ax extract render 2312.00752v2
12 files in content/en/2312/2312.00752: 0 written, 12 unchanged, 0 removed, 0 left alone
```

`content_sha256` is taken over the body, so a hand correction makes the file disagree with its own front matter and that is the whole mechanism.
`ax split` reports which files were corrected and prints both hashes, `ax extract render` refuses to overwrite a corrected file and says so, `ax split -accept` restamps the hash and sets `edited: true`, and `ax extract render -force` throws the correction away.
A file marked edited stays that way, because somebody decided about it and a hash that does not match is only a hint that somebody might have.

The licence gate runs again here and not only at fetch time.
Fetching and extracting are separate runs and the licence can be re-resolved between them, so the check belongs at the point something is about to be published as well as at the point it was downloaded.

`ax extract source` is the same command with three steps in front of it, and it is the path most of arXiv is on.

```
$ ax extract source -n 2006.10256v1
latexmlc (LaTeXML version 0.8.8)
converting 2006.10256v1 from paper.tex
  Error:undefined:\lstlanguagefiles The token T_CS[\lstlanguagefiles] is not defined. at listings.sty.ltxml; line 1603
  Error:imageprocessing:imageclass No image processing module found to convert types
converted 2006.10256v1 in 17.549s with errors, and whether they matter is the reject rule's question
2006.10256v1
  licence
  title       Array Programming with NumPy
  authors     Charles R. Harris, K. Jarrod Millman, Stéfan J. van der Walt, ...
  abstract    1 block
  headings    24
  blocks      3 figure, 4 listing, 93 paragraph, 1 quote
  unparsed    0
  faults      none
```

The three steps are unpacking the submission, working out which file the document starts from, and running LaTeXML over it, and everything after that is the code the render path already uses.
That is the reason this path costs a runner and no new reader: arXiv's own renderings are made by LaTeXML, so a paper converted here comes back in the same markup, with the same classes and the same `alttext` holding the LaTeX the author typed.
It does mean LaTeXML has to be installed, which is `brew install latexml` or `apt install latexml`, and a machine without it gets that line rather than a stack trace.
The flags are chosen rather than copied from arXiv: HTML5 out, the TeX kept beside every formula, TikZ and picture environments drawn as SVG, and no stylesheet copied next to the document.

The two error lines in that run are worth reading, because they are what conversion errors actually look like.
The first is a macro from a package LaTeXML models but does not model all of, and it costs the paper one code listing header.
The second is not about the paper at all: LaTeXML converts raster graphics through Image::Magick, and without it the PNGs are copied next to the document rather than converted, which is a limitation of this machine and not of the submission.
Neither is a hole in the body of the paper, which is why the reject rule passes this one, and the rule is the same rule the render path uses.

The status line LaTeXML prints is its own scale and not the process exit code, and the two disagree on purpose: a conversion with errors in it exits zero because it still wrote a document, with a marker where the thing it could not read was.
So a conversion that failed is one that wrote nothing, and everything else is a result somebody has to judge.
A conversion gets five minutes, because a submission that defeats LaTeXML tends to defeat it slowly rather than fail, and the NumPy paper takes under twenty seconds of that budget.
A paper that runs out of it is stopped and is on the native path.

The conversion is kept under `work/converted/` and reused, and `-again` throws it away and converts afresh.
It goes beside the unpacked submission rather than inside it because LaTeXML copies every picture a paper uses next to the document it writes, and a conversion written into the submission would leave this project's output mixed in with the author's files with nothing saying which was which.
The submission itself is unpacked from the cached bytes every time, since those are hashed in the manifest and the unpacked files are not.
A submission that turns out to be a PDF is reported and stepped over rather than failed on, the same way a version arXiv never rendered is on the fetch side, because a batch of a hundred papers should not stop at the first author who compiled their own paper.

The front matter records which of the two surfaces the file came from, in `path: render` or `path: source`.
A rendering is what arXiv made of a submission and a conversion is what this project made of the same submission, and they are not always the same document, so a reader comparing two papers has nothing else to go on.
Figures on this path are still undecided: they come out of the conversion as local files next to the document, and putting them through the figure gate is the next piece of work.

Figures have their own gate, because a `cc-by` article licenses what the authors own and not the plot they reprinted from somebody else's paper with permission.
There is no metadata for that and there never will be, so `ax figures` reads the signals that are actually in the source.

```
$ ax figures -n 2501.00001v3
2501.00001v3  2 figures with a picture in it
  Figure 1    owned      2501.00001v3/nothing.svg
  Figure 3    suspected  the caption says "reproduced" within 60 characters of a citation
1 figure withheld by rule F09, and the caption, the number and the tag are published anyway
```

Reproduced, adapted, reprinted and courtesy are ordinary English and they turn up in captions constantly, in "our adapted architecture" and "reproduced across five seeds".
Next to a citation the same word is an author saying where the picture came from, so the proximity is the rule and not a refinement of it.
A figure the corpus only suspects is treated exactly like one it has confirmed, because the cost of being wrong one way is a missing picture and the cost of being wrong the other way is republishing somebody's work under a licence they never granted.
A withheld figure is not a hole: the caption is published, the figure keeps its number and its tag, the prose that references it still resolves, and the page says the image is withheld and links to it on arXiv.

`ax figures check` runs the same gate over files somebody already has.

```
$ ax figures check figures/testdata/page.png
figures/testdata/page.png  png               1275 by 1650  17 KB
  page                     100.0% of a page  at 8.50 by 11.00 inches, from pHYs
  verdict                  withhold          F06: it covers 100 per cent of a page at the 8.50 by 11.00 inches the file states, and the cap is 75 per cent
```

Rule F06 is what separates a corpus from a mirror of copyrighted PDFs: a picture that covers a whole page is a page, and a page of somebody else's paper is not a figure whatever the caption under it says.
The physical size is read out of the file, which is the `pHYs` chunk of a PNG, the JFIF density of a JPEG and the `width` and `height` of an SVG when they carry a unit.
A file that states no resolution is not judged by this rule and the report says so, because a resolution cannot be guessed from a pixel count: at 150 dots per inch every high resolution figure measures as a page and at 300 every page scan measures as a figure, so the two wrong answers are wrong in opposite directions and neither beats admitting the file said nothing.

The headers are read by hand rather than by decoding the picture.
A figure is judged on its size and its shape, decoding the whole raster to find that out costs about as much as everything else in this pipeline put together, and there are three million papers.

Without `-n` the same command fetches each picture at the fifteen second pace, runs the whole gate over the bytes and commits the ones that pass.

```
$ ax figures 2501.00001v3
2501.00001v3: 1 committed, 1 withheld, 0 already decided, 2 figures in manifests/figures/2501.yaml
```

What it decided is written to `manifests/figures/<shard>.yaml`, one entry per picture, and a withheld figure gets an entry too.
That is the point of the file: without one, nothing can tell a picture that was refused from one that was never fetched, and the download is paid for again on every run.
The entry records the hash, what the header measured, where the physical size was read from, which rule refused it and what the caption said.

This command does not touch the content plane.
`ax extract render` reads the manifest and writes the image lines, because if `ax figures` edited the Markdown then the next extraction would put the arXiv paths back and the two commands would take turns undoing each other.
So a committed figure becomes an image pointing at `/figures/...`, a withheld one becomes a line saying what is missing and where to see it, and a picture nothing has decided about yet is written as it stands, which is the state of every figure until `ax figures` has run.

Running it twice costs nothing and writes nothing.
A picture already in the manifest is skipped without a request, and `-recheck` is how to make it decide again.
A recheck that finds the same bytes keeps the time they were first read, so a recheck that changes no decision leaves the file byte for byte as it was, which is what makes this manifest worth committing.

`-from <dir>` reads the pictures out of a directory instead of off the website.
That is for somebody who already has the source tarball unpacked, and it is what CI uses, because CI does not talk to arXiv and a committing path nothing exercises is a committing path nobody has run.

`ax tables` keeps every table twice, once as Markdown and once as the markup it was reconstructed from.

```
$ ax tables -n 2312.00752v2
2312.00752v2  15 tables
  t01         Table 4             9 rows by 4 columns
  t02         Table 1             26 rows by 11 columns
  ...
  t11         Table 4             5 rows by 8 columns
  t12         Table (unnumbered)  3 rows by 8 columns
$ ax tables 2312.00752v2
15 tables in tables/2312/2312.00752: 30 written, 0 unchanged, 0 removed
```

Markdown is what a reader sees and what a translator works on, and it is lossy on purpose.
It cannot express a cell that spans several rows, a header centred across several columns, or a rule drawn under part of a row, and academic tables use all three constantly.
The markup is what the loss is measured against.

The files are numbered by position and not by the number the paper prints, because the printed number is not an identifier.
Mamba prints Table 4 twice, once in the paper and once in an appendix, and prints one table with no number at all, which is what `t12` above is.

```
$ cat tables/2501/2501.00001/t01.md
| Model | Accuracy |  |
| :--- | :---: | :---: |
| Nothing | 0.0 | 0.1 |
$ cat tables/2501/2501.00001/t01.tex
% Table 1 of 2501.00001, rebuilt from arXiv's own rendering.
% Not the author's source. The spans, the alignment and the rules are LaTeXML's reading of it.
\begin{tabular}{lcc}
\textbf{Model} & \multicolumn{2}{c}{\textbf{Accuracy}} \\
\hline
Nothing & 0.0 & 0.1 \\
\end{tabular}
```

The Markdown loses the span and writes an empty cell where the second column of the header would be.
The markup says what the header actually covers, and it says in its first line that it is a reconstruction, because a file called `t01.tex` that does not say so is a file somebody will eventually quote as the author's source.
The source path replaces it with the author's own bytes when a paper goes down that route.

`ax tables check` is rules F11 and F12, which are that both files are there and that they agree on the row count, the column count and every numeric cell.

```
$ ax tables check 2501.00001
2501.00001  2 tables in tables/2501/2501.00001
  t01       fails    F12: number 2 is 0.1 in the Markdown and 0.2 in the markup
  t02       F11 F12  both files agree
```

The numbers are the part that matters.
A table is where a paper's measured results live, and a second representation that quietly reformats one of them is worse than no second representation at all.
The two files are read into cells and compared cell by cell rather than compared as text, because they lay the same table out differently by design, and mathematics is compared exactly as it stands on both sides.

The Markdown written here is the same text the extractor puts inside the section the table belongs to, from the same emitter, so the two cannot say different things about the same table.

`ax refs build` reads a paper's bibliography into fields.

```
$ ax refs build -n 2312.00752v2 2404.19756v5
2312.00752v2   116 references
  a DOI        2 of 116
  a title      116 of 116
  a year       116 of 116
  an arXiv id  26 of 116
  an author    116 of 116
2404.19756v5   118 references
  a title      115 of 118
  a year       118 of 118
  an arXiv id  22 of 118
  an author    118 of 118
  no title  bib.bib44  Aojun Lu, Tao Feng, Hangjie Yuan, Xiaotian Song, and Yanan Sun. Revisiting...
  no title  bib.bib46  Sergei Gukov, James Halverson, Ciprian Manolescu, and Fabian Ruehle. Searching...
  no title  bib.bib108  L. H. Kauffman, N. E. Russkikh, and I. A. Taimanov. Rectangular knot diagrams...
```

The parsing is by pattern and never by a model.
A bibliography is the most regular prose in a paper, because a style file laid it out and the same style file laid out every other entry in the same list.
A model asked to read a hundred entries per paper across three million papers would cost more than the rest of this pipeline put together, and would be wrong in prose, which is the hardest kind of wrong to find later.

Those two papers are the two styles the patterns have to survive.
Mamba is an author-year style that puts the title in quotation marks, and KAN is a numeric style that does not quote it and relies on the order of the blocks instead.
The three KAN entries with no title are entries printed as an author line and then everything else, where half of the second block is the title and half of it is the year.
Guessing where the split falls would invent a field, so nothing guesses, and the entry is published with the line the paper printed and no title.

```
$ cat manifests/refs/2312/2312.00752.yaml
paper: "2312.00752"
version: 2
entries:
    - id: bib.bibx1
      label: Arjovsky et al. (2016)
      authors:
        - Martin Arjovsky
        - Amar Shah
        - Yoshua Bengio
      title: Unitary Evolution Recurrent Neural Networks
      venue: In The International Conference on Machine Learning (ICML), 2016, pp. 1120–1128
      year: 2016
      text: Martin Arjovsky, Amar Shah and Yoshua Bengio “Unitary Evolution Recurrent Neural Networks” In *The International Conference on Machine Learning (ICML)*, 2016, pp. 1120–1128
```

`text` is the line the paper printed and it is what gets published.
The fields are a reading of a bibliography style and any of them can be empty, so an empty field is a field the style did not make available rather than an error.
That entry prints 1120 and 1128 after the year it actually states, which is why a year is a four digit number in the range a paper can cite and not any four digits.

The manifest is keyed by version as well as by paper, because a paper adds references between versions constantly and a bibliography read from v1 does not describe v3.
The entries keep the order the paper prints them in and are not sorted, because that order is the numbering in a numeric style.
There is one file per paper rather than one per month, unlike figures: a bibliography runs to a hundred entries and a month runs to twenty thousand papers, so a month of references would be a two million line file that every extraction in that month rewrites.

A bibliography entry is a line of the paper, so the licence gate runs here too, at the point the manifest is about to be written.

`ax refs resolve` matches those entries against the metadata plane.

```
$ ax refs resolve -n 2501.00001
2501.00001v3  6 references
  resolved    3 of 6
  via arxiv   1
  via doi     1
  via title   1
  near miss  1.00  bib.bibx6  1901.00004  no author of the entry is an author of the record
```

That run is the test fixture and not a real paper, because the metadata plane is the only thing a bibliography can resolve against and nobody has filled it yet.
What a real bibliography resolves at is a number this README will carry once the plane holds the snapshot, and quoting one before then would be quoting a guess.

The ladder stops at the first step that hits.
An arXiv id printed in the entry, then a DOI, then the title with the year within one and at least one author in common, then nothing.
Nothing is a fine outcome: a reference to a textbook resolves to nothing, and the entry is still published as a bibliography line, it just does not become an edge.

The title step is the one that needs guards, and it has two.
The similarity has to clear 0.92, which is high on purpose, because two different papers by the same group in the same year agree on far more than half of their characters and a loose threshold here does not produce a few wrong edges, it produces a systematically wrong graph in exactly the neighbourhoods somebody would want to read.
Then the year has to be within one, because a paper is cited by its preprint year as often as by its proceedings year, and one author surname has to appear on both sides.

An edge built on an identifier is recorded as certain and an edge built on a title is recorded as medium, because the third step is a reading of prose and the first two are not.
Two papers that match one entry equally well resolve to neither, because a coin toss between them is an edge that is wrong half the time and says nothing about which half.

The near misses are the half of the report worth reading.
A resolution rate on its own cannot tell a threshold that is doing its job from one that is set wrong, and the entries that cleared the title and then failed on the year or the author are where that shows.

Every paper named in one run is resolved by a single pass over the plane.
The plane holds three million records and a bibliography holds a hundred entries, so the entries are what goes into a map and the plane is streamed past them, which keeps the memory proportional to the bibliography and makes a run over fifty papers cost the same pass as a run over one.
Resolution is cleared and redone from scratch each time, because the plane grows and an entry that resolved to nothing last month is a question worth asking again.

`ax refs cites` reads every citation out of a paper's sections, with the locator each of them carries.

```
$ ax refs cites -n 2311.05762
2311.05762v2       75 citations
  with a locator   24 of 75
  via after-comma  23
  via before-of    1
  conjecture 2.2.2  bib.bib29  ...f the desired $2K^{C}$. It was Ruzsa \[[28](#bib.bib28)\], \[[29](#bib.bib29), Conjecture...
  page 8  bib.bib11  ...[33](#bib.bib33)\], and see also the comments on page 8 of \[[11](#bib.bib11)\]) showed that...
  theorem 1.11  bib.bib11  ...18)\]. ## Applications {#su1-1 .section} It was shown in \[[11](#bib.bib11), Theorem 1.11\]...
```

A citation says that one paper mentioned another and a locator says which result of it was used, and the difference between those two is the whole of 08-graph.md section 5.
The first is a citation edge, which a paper level citation graph already has.
The second is a `uses` edge, which is the hard one, the novel one and the one worth the most, because it says that this theorem was proved using that theorem.

That paper is "On a conjecture of Marton" by Gowers, Green, Manners and Tao, which is the proof of the polynomial Freiman-Ruzsa conjecture.
It is here because a mathematics paper is where locators live.
Mamba and KAN between them make 412 citations and not one of them names a result, which is not a failure of the patterns, it is how machine learning cites.

The patterns are in `manifests/graph.yaml` and the binary carries a default set.
A corpus file replaces it rather than adding to it, because the person who notices that a field writes locators in a shape nothing matches is the person reading that field's papers, not whoever is editing this repository that week.

```
$ ax refs locators
patterns  after-comma after-parens after-bare before-of before-comma before-credit
kinds     30
  algorithm
  appendix
  assumption
  ...
```

08-graph.md estimated about thirty patterns.
What is actually there is a vocabulary of thirty kind words crossed with six shapes, which covers more than thirty flat patterns would and is easier to be right about: the thirty flat patterns are these six shapes written out thirty times, and a flat list of near identical regular expressions is where a typo hides for a year.

The shapes split down the middle.
Three of them read the text after the citation, which is where a numeric style prints the locator, so "[12, Lemma 3.4]".
Three read the text before it, which is where the sentence puts it, so "by Theorem 2 of [7]".
The first pattern that matches wins, and the order in the file is the order of certainty: a locator inside the citation's own brackets cannot be anything else, and a locator read out of the surrounding sentence can.

Every citation is recorded and not only the ones that carry a locator, because a citation is an edge either way and where in the paper it sits is half of what it says.
The prose around a citation is kept only where there is a locator to check it against, since a paper makes a thousand citations and a handful of them name a result.

One record per place in the paper that cites something, which is why a work cited nine times is nine lines in `manifests/cites/` and one line in `manifests/refs/` beside it.
Nothing in the citations file says what the entry resolved to.
That is in the bibliography, it belongs in exactly one place, and the graph joins the two rather than reading a copy out of one and having to wonder which is older.

`ax tags assign` gives every object in a paper a permanent name.

```
$ ax tags assign -n 2311.05762
2311.05762  116 objects
  new       116
  kept      0
  run       X22B to XQEF
  X22B  s1
  86R6  prob-1-1
  UI64  thm-1-2
```

A tag is four characters of the Stacks Project's alphabet, it is assigned once, and it never changes.
Every link in the reading app, every edge in the graph and every translated file points at one, which is what lets all three survive the paper being extracted again next year by a better tool.
Numbering is not stable over time, LaTeX labels are human readable and therefore get edited, and a reference has to survive both.

Four characters is 1,679,616 names, which is nowhere near enough for a corpus and is absurdly loose for a paper.
That is the trick: a tag is unique inside one paper and the arXiv id carries the other half of the name, so the canonical reference is `2311.05762#UI64` and a bare tag is not a reference at all.
Bourbaki and papers used four hex digits, which is 65,536, and that was enough for one treatise and for a hundred papers.
The answer here is not a longer tag, it is a tag with a smaller job.

Tags are handed out from a shuffled space, so nothing about one says where its object sits.
The order is the same on every run, which is what makes re-running an assignment write nothing, and it is different for every paper, so a person looking at two papers side by side sees nothing in common between them.
The alternative is worse than it looks: the first person to notice that tags ascend in reading order writes code that sorts by them, and that works until somebody inserts a section.

The register is the record and the content files are the copy, and both are written.

```
# tags/2311/2311.05762.tags
X22B,s1
86R6,prob-1-1
UI64,thm-1-2
```

```markdown
**Conjecture 1.1** {#prob-1-1 .problem tag=86R6 env=conjecture}
```

The anchor is the local identifier and not the tag, so a URL is readable and a tag is stable.
One register per paper and not one for the corpus: a single file would be five million lines, two people extracting two unrelated papers would conflict in git on every run, and a takedown would be a rewrite rather than a deletion.
`tags/2311/2311.05762.runs` sits beside it and says where one assignment stopped and the next began, which is what tells a correct edit apart from a tag somebody pasted in the wrong place.

An object that is in the register and no longer in the paper stops the run.
Matching it to whatever replaced it is the four pass matcher in 03-tags.md section 6, which is the author's own `\label`, then the kind and number, then a hash of the normalised prose, then a sequence alignment, and it arrives with `ax tags diff` in M4.
Guessing in the meantime is exactly the failure this whole mechanism exists to prevent.
A reference that breaks is visible and a reference silently pointed at the wrong theorem is not.

`ax audit -plane content` is where all of that gets checked rather than trusted.

```
$ ax audit -plane content -q
rule  state    checked  findings
S01   pass     26
S04   pass     2
S07   pass     26
S10   pass     26
S12   pass     26
T01   pass     26
T02   pass     26
T03   pass     26
T04   pass     2
T05   pass     26
T06   pass     2
T07   pass     2
T08   pass     24
T09   pass     26
T10   pass     26
T11   pass     26
T12   pass     26
T13   pass     26
M01   pass     26
M02   pass     2
M03   pass     26
M05   pass     26
M07   pass     26
M08   pass     26
M09   pass     26
M10   pass     26
M11   pass     26
M12   pass     26
M13   pass     26
M14   pass     2
F01   pass     40
F02   pass     14
F03   pass     14
F05   pass     14
F06   pass     14
F07   pass     2
F08   pass     2
F09   pass     41
F10   pass     2
F11   pass     2
F12   pass     21
R01   fail     3        1
R02   pass     26
R03   pass     2
R04   not run  0
R05   not run  0
R06   pass     234
G01   pass     2
G02   pass     26
G03   pass     2
G04   pass     2
G05   pass     26
G06   pass     26
X01   pass     26

26 files over 2 papers, 1 finding, and the build fails

R01 every arXiv id written down in this corpus names a paper the metadata plane has
  manifests/refs/2404/2404.19756.yaml: 2404.19756: bib.bib18 names arXiv:2312.14276, and the metadata plane has no paper with that identifier
ax: a hard rule found something
```

Group S is the licence gate read back off the files.
Its other half runs over the metadata plane and asks what a record's licence was read from, and these five ask the same question of a file that got written: was this allowed, and is what it says about itself true.
`S01` is the gate itself, and a paper the corpus may only hold a record of has no content file at all, which is a thing to check rather than a thing to trust, because the check costs nothing and the failure is republishing somebody's paper without their permission.
It reads the access line and the licence the article carries against each other as well, since the tool derives the one from the other and a file where the two disagree was written by somebody.
`S10` is where the licence came from, and the answer has to be the abs page, because every bulk surface states one licence for a whole paper and only the abs page states the licence of a version.
`S12` is the version trap, which is the reason the licence is carried per version everywhere in this project.
A paper relicensed at v3 still has a v1 under the old terms, so a corpus that reads the licence off the paper and publishes the version it extracted has published one version under another version's permission, and when the finding sees that shape it says so.
`S04` is a paper with content and no record behind it, and it is the floor the other four stand on: every question here is asked of the record, so a paper it reports is a paper the rest of the group steps over rather than four findings about one missing line.
`S07` is the version the file names, which the plane has to hold, because a licence checked against a version that does not exist has not been checked.

Group T is the one that says a content file is a content file: it parses, its fields are known and typed, its recorded hash matches the body under it, its sections run from 0 with no gaps, its headings skip no level, and it has a front matter file with an abstract in it.
The three that catch what a bad extraction actually leaves behind are the last ones.
`T10` is the page furniture: a running head, a page number on a line of its own, and arXiv's own identifier, which it prints down the left margin of every PDF it serves and which comes back off that page as a column of one character per line.
`T11` is HTML a converter gave up on, which matters most on the render path, because a table left as `<table>` markup passes every other group in the audit and is unreadable.
`T13` is a word the page broke in half at a hyphen, which is soft, because sometimes a hyphen is a hyphen.

Group M reads the mathematics, and all twelve of the rules here work off one splitter.
That is the whole design of the group: a rule that decides for itself where a formula starts will disagree with the next rule that does, and two rules disagreeing about where a span begins is how a corpus gets a finding nobody can reproduce.
`M01` is a span nothing closes, which never damages only the formula, because everything after it is set as mathematics too.
`M03` is the opposite leak, a `\frac` left sitting in the prose, and it is the one failure that reads as ordinary text to every other group.
`M07` is a bracket opened in a sentence and closed inside a formula, which is the shape a span that is off by one delimiter always has.
`M11` holds the corpus to one delimiter, and it only reads `\[` when the line is the delimiter or opens and closes on itself, because `\[` in the middle of a sentence is Markdown escaping a bracket and this corpus writes hundreds of those in citation labels and table cells.

`M14` is the rule that decides whether the extraction was usable at all.
The other eleven read the spans and ask whether they are right, which leaves the worst outcome unexamined: a paper whose formulas were dissolved into prose has no spans to read, so all eleven report that they had nothing to look at and the audit comes back green over a destroyed paper.
It counts the characters that appear in mathematics and never in English, and a paper carrying three or more of them with not one math span is a paper something flattened.

Group F reads the pictures and the tables, and the shape of it is that a figure is three things: a decision in the manifest, bytes on disk and a line in a body.
Every way a corpus goes wrong here is two of those three disagreeing.
`F01` is the one that goes to disk, from both sides, and it is also the rule that says `ax figures` has never been run: the extractor leaves arXiv's own path in the body until something decides about the picture, so a body still pointing at `2404.19756v5/kan_mlp.png` is a picture nobody has established the permission for.
That is not a hypothetical. It is what this rule found on KAN the first time it ran, where the one figure still served off arXiv was the teaser above the abstract, because `ax figures` walked the sections and never the abstract.
`F08` and `F09` are the two that are about permission rather than about quality, and both are deliberately blunt.
`F09` treats suspected exactly as confirmed, because the cost of being wrong one way is a missing picture and the cost of being wrong the other way is republishing somebody else's copyrighted work under a licence they never granted.
`F02`, `F03`, `F05` and `F06` read what `ax figures` measured rather than measuring again, since the command is what measures and a second opinion in the audit is a second answer to defend.
`F07` is the rule that loads the manifest at all, the way `G01` loads the register, so a manifest that does not parse is one finding here rather than six across the group, and the caption is checked by the loader and not again by the rule.

`F11` and `F12` are the tables, and they run through the same package `ax tables check` runs through, because a rule and the command it checks that disagree about what agreeing means is worse than having neither.
`F11` counts the tables in the sections against the pairs on disk in one direction only: more tables in the bodies than on disk is a paper `ax tables` has not been run over, and more on disk than in the bodies is a table the paper cut between versions, which `ax tables` sweeps itself.
`F12` is the measurement, and it is the reason a table is written twice at all.

Group R reads the bibliographies and the edges built out of them.
`R03` is the rule that loads the manifest at all, the way `F07` loads the figure manifest and `G01` loads the register, and what an entry has to keep is `refs.Load`'s definition rather than a second copy of it written here.
`R02` is a citation with no entry behind it, which on a paper nobody has run `ax refs` over is every citation that paper has, and that is the answer wanted: a bibliography is part of the content and not an extra.
`R01` reads every arXiv identifier written down anywhere, in a body, in an entry and in a resolution, against the metadata plane.
It judges an identifier only when the plane holds that month, because a month nobody has harvested yet is a fact about the harvest and not about the paper, and the plane's own coverage is group S's question.
`R04` re-runs the matcher over the record the edge points at, which is the only way to say that an edge written months ago would still be written today, and it needs no network to do it because the answer is in the plane.
`R05` is a bibliography entry that resolves to the paper it sits in, which is what a title match against a paper's own title looks like from the outside.
`R06` is a citation dated after the paper making it, with two years of slack, because a preprint cited in one year and published two years later is a style printing the publication year and not a defect.

The one finding in the run above is `R01` and it is true of that corpus.
Those two papers live in a scratch corpus whose metadata plane holds two records, one of them in the month KAN cites into, so the rule judges the identifier and reports it.
`R04` and `R05` say not run for the same reason: nothing in either bibliography resolved against a two record plane, so there is no edge for either of them to read.
That is the checked column doing its job rather than a green tick over an empty question.

`T12` is declared with group T and implemented with group R, because a link to `#bib.bib28` is a leak on one reading and a citation `ax refs` has not got to yet on another, and telling those two apart means having the bibliography.
It found a defect in the render path the first time it ran, on KAN, where two links into numbered equations pointed at anchors nothing in the corpus had.
LaTeXML numbers a display by putting it in a `tbody` of its own and hanging the identifier there, so the paper links to `S4.E8` while the table wrapping it is `S4.EGx18` and nothing at all links to that, and the rewrite had been reading the identifier off the table.

Group G reads the register against the bodies the tags in it were written into.
The register is the record and the content files are the copy, which is the shape of the group: `G01`, `G03` and `G04` read the register on its own, `G06` reads the copy on its own, and `G05` is the two disagreeing, which is what a rewrite that stopped half way leaves behind and is the state that makes a tag resolve to the wrong object rather than to nothing.
`G02` is the rule that pays for scoping tags to a paper: a bare `03QK` is not a reference here, because `03QK` exists in thousands of papers and means something different in each, and the reference is `2106.09685#03QK`.
`G06` scans a body with the same function `ax tags assign` scans it with, so the rule and the command cannot disagree about what a taggable object is, and a paper nobody has tagged fails it on every object it has, which is the answer wanted: content is committed tagged.

`X01` is the object model, and it holds every object to one of the sixteen kinds in 06-objects.md, because the kind is what every later group dispatches on.
A block the extractor gave an anchor and no class is reported by it too.
That block still gets a tag, on purpose, because an anchor nothing can be written against is worse than a tag on something that turns out to be a footnote, and it is still a hole in the class mapping.

The checked column is the point of the whole thing.
A rule with no findings and nothing checked has not passed, it has not run, and the two are different states in the report and not the same green tick.
`S04`, `T04`, `T06`, `T07`, `M02`, `M14`, `F07`, `F08`, `F10`, `F11`, `R03`, `G01`, `G03` and `G04` are about a paper rather than a file, which is why they say 2 where the rest say 26.
The F rules that count figures say 40 and 14 and 41 because those are pictures and not files: 41 decisions in the two manifests, 14 of them committed, and 40 image lines across the bodies.
`T08` says 24 because an abstract is as long as its author made it and is not a section that came out too short.
`R06` says 234 because that is the number of bibliography entries in the two papers that printed a year, which is what the rule reads.

Mathematics and code are masked out of a body before the T rules read it, delimiters and all, with every line kept exactly where it was so a finding still points at a line somebody can open.
Without it `$a<b>c$` is an HTML tag, a listing that shows a table is raw markup, and a shell session with a number on a line is a page number.
Where the mathematics and the code are is the M group's splitter answering, and not a second reading of the same body, so the two groups cannot end up with different opinions about which lines are a fence.

Seven rules of group S are not here, and five of those seven are the ones that read the metadata plane rather than a file, which is where they run.
`S02` and `S06` are the two about translated files, one saying a no-derivatives paper has none and the other holding a translation to the licence the propagation table gives it, and nothing is translated yet, so both arrive with M8.
`S03` and `S11` are git and the takedown manifest, neither of which this tool has to read yet.
`S08` and `S09` measure a body against the pages it was read off, and a page count is something only the native path has, so the two of them arrive with it in M4.

Three rules of group R are not here.
`R07` is the acquire selection report, which is a paper three or more papers in the content plane cite and which is not in the plane itself, and nothing writes that report yet.
`R08` reads a rendered reference section and nothing renders one yet.
`R09` compares a paper's resolution rate against its category's median, which needs the same baselines `M06` needs, so the two arrive together in M6.
`M04` and `M06` are not here: `M04` is every span parsing under KaTeX and the reader that carries KaTeX is M6, and `M06` compares a paper's displays per page with its category's median, which needs baselines computed over a corpus with more than two papers in it.
`G07` and `G08` are not here either: `G07` is about tombstones and nothing writes one until `ax tags diff`, and `G08` reads git history for a tag that used to be in a register and is not any more, which is the same milestone.
`F04` is not here for the same reason as `G08`: it says nothing under `figures/` is untracked, which is a question for git, and this tool is run over a directory that is a checkout on one machine and an unpacked archive on the next.
Of group X only `X01` runs, because the other eight read a result, a concept or an artefact record and none of those three things exist yet.
Every one of them is named in the source with what it needs, because a rule registered before it can run is a rule everybody believes is working.

The metadata plane is filled from three surfaces, which are the Cornell snapshot on Kaggle, a Hugging Face mirror of it, and arXiv's own OAI-PMH for anything newer than the snapshot.
Whichever it was read from, the record says so, and the audit is what holds that to be true.

```
$ ax licence explain cc-by-nc-nd
licence      cc-by-nc-nd
deed         https://creativecommons.org/licenses/by-nc-nd/4.0/
access       verbatim
metadata     yes, always, arXiv publishes it under CC0
full text    yes
figures      yes
translation  no
our files    cc-by-nc-nd
```

That command is the whole project in eight lines.
arXiv's default licence is a nonexclusive distribution licence that grants arXiv the right to distribute and grants nobody else the right to republish, and the no-derivatives licence forbids translation because a translation is a derivative work.
So the first thing this tool does with any paper is work out which of four access classes it is in, and three of those four classes forbid something the project would otherwise do.

## Install

```sh
go install github.com/tamnd/arxiv-reader/cmd/ax@latest
```

## The corpus

`ax` reads and writes one corpus directory, found through `ARXIV_CORPUS` or the current directory when that is unset.

The corpus has two planes and they have different rules.

The **metadata plane** covers every paper on arXiv, which is about 3.17 million of them.
It is JSONL, one file per month, and it needs no licence gate because arXiv publishes its metadata under CC0.

The **content plane** is the papers whose licence permits republishing, extracted into Markdown with the mathematics, figures, tables and bibliographies kept intact.
It is a small fraction of the first plane and it always will be.

## The four extraction paths

| Path | Input | Model |
| --- | --- | --- |
| render | arXiv's own LaTeXML HTML5, at `/html/<id>` | none |
| source | the submitted TeX, compiled with LaTeXML here | none |
| native | the PDF's text layer | none |
| vision | page images, read by a vision model | yes |

Three of the four use no model at all, which matters more than it sounds.
About 90 per cent of arXiv has TeX source and about 97 per cent of recent submissions have an HTML rendering, so the paths that cost nothing cover almost everything and the vision path is the fallback for scanned submissions.

## Related

- [tamnd/arxiv](https://github.com/tamnd/arxiv), the corpus this builds
- [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli), the twelve arXiv surfaces, the id parser and the `ax://` URI space
- [tamnd/llm](https://github.com/tamnd/llm), the model transport, routing, queue and ledger

## Licence

Apache 2.0.
The corpus it builds is licensed separately and per paper, which is the point of the licence gate.
