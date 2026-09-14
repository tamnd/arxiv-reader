# arxiv-reader

The toolchain that builds [tamnd/arxiv](https://github.com/tamnd/arxiv): harvest arXiv's metadata, extract a paper into tagged Markdown, translate it, connect it to the rest of the corpus, and publish the result as web, EPUB, TeX and PDF.

The binary is called `ax`.
It is not called `arxiv` because [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli) already installs a binary by that name and this tool depends on it.
`ax` is also the URI space the corpus uses, which is `ax://paper/2106.09685`.

## Status

M3, which is one paper end to end down the render path, and the seed paper is Mamba.
M2 before it was the licence census: counting what the corpus is permitted to do with what it holds.
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
