# arxiv-reader

The toolchain that builds [tamnd/arxiv](https://github.com/tamnd/arxiv): harvest arXiv's metadata, extract a paper into tagged Markdown, translate it, connect it to the rest of the corpus, and publish the result as web, EPUB, TeX and PDF.

The binary is called `ax`.
It is not called `arxiv` because [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli) already installs a binary by that name and this tool depends on it.
`ax` is also the URI space the corpus uses, which is `ax://paper/2106.09685`.

## Status

M2, which is the licence census: counting what the corpus is permitted to do with what it holds.
M1 before it was the metadata plane: the harvest, the writer, the rules that check it and the report that sets it against arXiv's own numbers.
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
