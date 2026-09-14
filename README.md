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
