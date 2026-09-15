# arxiv-reader

The toolchain that builds [tamnd/arxiv](https://github.com/tamnd/arxiv): harvest arXiv's metadata, extract a paper into tagged Markdown, translate it, connect it to the rest of the corpus, and publish the result as web, EPUB, TeX and PDF.

The binary is called `ax`.
It is not called `arxiv` because [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli) already installs a binary by that name and this tool depends on it.
`ax` is also the URI space the corpus uses, which is `ax://paper/2106.09685`.

## Status

M5, which is the first thousand papers in English, and the parts of it that are written are the native path, the vision path and the selection: the PDF fetch, `pdftotext`, the structure recovered from the shape of a printed page, the page images a model reads when there is no text on the page to read, the file that says which papers are in the content plane and why, and which of the four paths each of them goes down.
M4 before it was the source path: the papers arXiv never rendered, which is everything announced before December 2023.
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

The third surface is the PDF, and it is the only one arXiv has for every paper it has ever taken.
A rendering exists from December 2023 onward and only where there is TeX, an e-print exists unless the submitter withheld it, and a PDF is what the site shows a reader.
That is the whole reason the native path is worth having: a paper with a PDF and nothing else is still a paper this corpus can read.

```
$ ax fetch native 1710.05832v1 2006.10256v1
fetching 1710.05832v1
1710.05832v1           fetched  cc-by          1599186  work/pdf/1710/1710.05832v1.pdf
                       holds text on 18 of its 18 pages, which is a PDF that was typeset rather than scanned
fetching 2006.10256v1
2006.10256v1           fetched  cc-by          1437715  work/pdf/2006/2006.10256v1.pdf
                       holds text on 19 of its 19 pages, which is a PDF that was typeset rather than scanned
2 fetched, 0 cached, 0 arXiv does not serve, 2 sources in manifests/sources.yaml
```

The second line under each paper is `pdftotext -layout` run over the bytes that just landed, and it is there for the same reason the main file of a tarball is reported at fetch time.
Whether a PDF was typeset or scanned cannot be seen from its size or its name, it decides which of two paths the paper is on, and somebody fetching a thousand of them wants the split before the extraction starts rather than after.
A page counts as typeset when it holds four hundred characters that are not spaces, which is ten times what arXiv's own margin stamp comes to, and a paper counts as born digital when three fifths of its pages do.
The share is not all of them on purpose, because a paper whose figures are full page images has pages holding nothing but a caption and a paper with a plate section at the back has a run of them, and neither is a scan.
Nothing is written down by that reading and nothing is routed by it: it is a sentence printed for a person, and the path a paper ends up on is recorded by `ax extract` in its front matter.

The reading takes poppler, and a machine without it still fetches.
The question is asked once for the run rather than once per paper, so what a machine with no `pdftotext` gets is one line saying how to install it, one line saying what is lost, and then the downloads.

One thing this turned up is worth writing down, because it is a cost and not a feature.

```
$ ax fetch verify
1710.05832v1  native  ok  0fcadae9d35e
1710.05832v1  source  ok  0fcadae9d35e
2006.10256v1  native  ok  c6705a2bad0a
3 sources, 3 ok, 0 missing, 0 changed
```

The LIGO paper's e-print and its PDF are the same bytes, hash for hash, because its submission was a PDF and arXiv serves that file back unchanged.
So a PDF only paper fetched on both routes costs two requests and two copies on disk for one file, and the manifest is the thing that makes it visible rather than the thing that prevents it.
Both entries are honest, each recording the surface its bytes came off, and neither is the one to delete.
What fixes it is path selection deciding that a paper whose e-print is already a PDF does not need a second download, and that arrives with the selection command rather than here.

Choosing the sample papers for this path meant reading licences first, and what that reading found is worth writing down.
Of eight famous machine learning preprints checked in September 2026, being Attention Is All You Need, ResNet, GANs, VAEs, GPT-3, ViT, BERT and DDPM, all eight are under arXiv's own nonexclusive licence, so this corpus may hold their metadata, their structure and their tags and none of their text.
The famous papers that are CC-BY are elsewhere, and the NumPy paper and the two LIGO detections are three of them.

That reading is the first half of the selection, and `ax select` is where it gets written down.

```
$ ax select add -reason cited-by-corpus -cited 7 -languages en,vi 2006.10256v1
2006.10256v1  cited-by-corpus  cited by 7 papers already in the content plane
$ ax select explain 2006.10256
2006.10256v1
  reason      cited-by-corpus
  because     cited by 7 papers already in the content plane
  means       cited by at least 3 papers already in the content plane
  added       2026-09-15
  status      selected
  languages   en, vi
```

The metadata plane covers all of arXiv and the content plane never will, so the content plane is a selection, and the selection is the most consequential judgement in the project.
It decides what gets read, translated and published, and what stays a record.
That is why it is a committed file with a reason on every row rather than whatever list somebody happened to run the extractor over.

There are six reasons and no seventh.
A paper is on the hand written seed list, or at least three papers already in the corpus cite it, or it cites at least three of them, or it is near the top of its primary category for its year by citation count, or somebody asked for it in an issue, or it is a member of a named reading list.
A paper nobody can put under one of the six is a paper somebody wants for a reason they have not written down, and there is no row for that.

The reason on its own is a word, so each one has to carry the evidence that makes it checkable.
`cited-by-corpus` with no count could be two papers, `requested` with no issue could be anybody, and `category-canon` with no rank could be the nine hundredth paper in its field.
So the closure reasons are refused under three, which is the floor and not the threshold anybody runs, `category-canon` needs both a category and a rank, `requested` needs both an issue and a name so that somebody owns the request, and `collection` needs the list it is a member of.
The same check runs on the way in and on the way out, which is what backs up the line at the top of the file saying not to edit it by hand.

```
$ ax select add -reason requested -issue 214 2006.10256v1
ax: selection: 2006.10256v1 is requested and needs both an issue and a name, so that somebody owns the request
```

The licence gate is asked here and not later.
A paper under arXiv's own licence cannot be in the content plane at all, because the corpus may publish its record and nothing else, and selecting it would be choosing a paper nothing can ever be written for.
A paper nobody has resolved a licence for is refused separately, and the two refusals are different on purpose: an empty licence means nobody has looked, an unknown licence means somebody looked and arXiv did not say, and choosing on the strength of a field nobody filled in is how a corpus republishes something it may not.

```
$ ax select add -reason seed 1706.03762v7
ax: 1706.03762v7 is arxiv-1.0, so the corpus may publish its record and nothing else, and a paper like that cannot be in the content plane
```

How far a paper has got is a rung and not a set of flags.
The stages are `selected`, `fetched`, `extracted`, `tagged`, `translated` and `published`, they are strictly ordered, and recording the rung means a paper cannot claim to be translated and not extracted.
It also means asking for everything extracted includes everything that got further, which is the question a batch run actually asks.
A status that goes backwards is refused unless `-back` says so, because a paper does not become unextracted by accident.

One entry per paper and not one per version, because the content plane holds the one version the licence gate decided may be published.
Choosing a second version replaces that decision rather than making two of them, and the date the entry was first added is kept when a row is rewritten to correct a rank.

```
$ ax select list
1710.05832v1  seed             extracted  on the hand written seed list, which is licence first and subject second
2006.10256v1  cited-by-corpus  extracted  cited by 7 papers already in the content plane

seed             1
cited-by-corpus  1
cites-corpus     0
category-canon   0
requested        0
collection       0

2 papers in the content plane: 2 extracted
```

Every reason is in that count, including the ones with nothing under them, because a reason that never fires is a fact about the selection and not an absence.
The counts are over the whole file and not over whatever `-reason`, `-status` or `-at-least` kept, since the count of what a filter kept is a number the caller already has.

`ax select drop` takes a paper out, and it is not a takedown.
A takedown is somebody exercising a right over a paper that was published, it deletes the files and leaves a tombstone, and it belongs to the licence gate.
This is for a paper that was chosen and should not have been, so it refuses a paper that has reached `extracted`: there are files for it in the corpus, and taking the row out would leave them with nothing saying why they are there.

Two halves of this are not written yet and say so rather than succeeding quietly.
`ax select seed` proposing a list off the licence census needs a person to edit what it proposes, and `ax select suggest` is the citation closure over `manifests/refs`, so it arrives with the reference work.

Once a paper is chosen, `ax path decide` works out which of the four paths it goes down and writes that next to the reason it was chosen.

The tree is three questions asked in order.
Does the submission hold TeX, and if it does, does arXiv serve a rendering of this version, and if it does not, does the PDF hold a text layer.
TeX with a rendering is the render path, TeX with no rendering is the source path, a PDF with text on it is the native path, and a PDF with nothing on it is the vision path.

```
$ ax path decide -probe
1602.03837v1: selection: the path cannot be decided until somebody has looked at whether the PDF holds a text layer, so run ax fetch native
1710.05832v1: selection: the path cannot be decided until somebody has looked at whether the PDF holds a text layer, so run ax fetch native
2207.09293v3: selection: the path cannot be decided until somebody has looked at what the submission holds, so run ax fetch source
2006.10256v1  render  the submission holds TeX and arXiv serves a rendering of this version, which costs one request and no model
2201.11903v1  render  the submission holds TeX and arXiv serves a rendering of this version, which costs one request and no model
  papers     5, 2 decided now, 3 nothing can decide yet
  probes     2
  render     2
  source     0
  native     2
  vision     0
  undecided  3
ax: 3 papers have no path yet, and the line above each one says what would settle that
```

A question nobody has answered is a hold and not a default, which is the whole design of this command.
The two gravitational wave papers there are PDF submissions, so the next question is what their text layer holds, and nothing on this machine has looked.
Guessing at that point would put a paper on the vision path, and the vision path is the one that costs money per page, so the command says which command would settle it and moves on.
Nothing else in the tree can be guessed at either: the 2022 paper has no e-print here, and a paper whose submission nobody has opened has no answer to the first question.

The rendering question is one HEAD request at the same fifteen second pace as everything else, and it only happens under `-probe`.
The spec says to read the abs page for a link to the rendering, and this asks the rendering itself, because it is the same one request and the answer is the status code rather than a link that may or may not be in the markup.
A version arXiv does not render is a 404 and a 404 is an answer, so it comes back as no and not as a failure.

```
$ ax fetch native 1602.03837v1 1710.05832v1
fetching 1602.03837v1
1602.03837v1           fetched  cc-by           935476  work/pdf/1602/1602.03837v1.pdf
                       holds text on 16 of its 16 pages, which is a PDF that was typeset rather than scanned
fetching 1710.05832v1
1710.05832v1           fetched  cc-by          1599186  work/pdf/1710/1710.05832v1.pdf
                       holds text on 18 of its 18 pages, which is a PDF that was typeset rather than scanned
2 fetched, 0 cached, 0 arXiv does not serve, 8 sources in manifests/sources.yaml
$ ax path decide
2207.09293v3: selection: the path cannot be decided until somebody has looked at what the submission holds, so run ax fetch source
1602.03837v1  native  the submission is a PDF the authors made themselves and it holds a text layer, so the text is already there to be read
1710.05832v1  native  the submission is a PDF the authors made themselves and it holds a text layer, so the text is already there to be read
```

Both of those facts are written into `manifests/sources.yaml` as `holds` and `text` at the moment they are learnt, which is why the second run needed no argument and no network.
They are learnt for nothing during a fetch, since something has to open the e-print to see what is in it and something has to read the PDF to know whether it is worth reading.
They are recorded because the bytes do not survive: `work/` is not committed, so a corpus that printed those two lines and threw them away would make the next machine download the same files again to answer the same two questions.

A paper that has a path keeps it, so running this over a month twice costs nothing and a batch that stopped halfway can be run again.
`-again` decides the papers that already have a path, which is what to do when arXiv has backfilled renderings for a year that had none.
`-n` decides everything and writes nothing.
A path with nothing next to it saying how it was decided is refused the next time the file is read, the same way a reason with no evidence is, so a row edited by hand to say `vision` stops the next command rather than quietly sending a paper to a model.

```
$ ax path list
1602.03837v1  native     the submission is a PDF the authors made themselves and it holds a text layer, so the text is already there to be read
1710.05832v1  native     the submission is a PDF the authors made themselves and it holds a text layer, so the text is already there to be read
2006.10256v1  render     the submission holds TeX and arXiv serves a rendering of this version, which costs one request and no model
2201.11903v1  render     the submission holds TeX and arXiv serves a rendering of this version, which costs one request and no model
2207.09293v3  undecided

render     2  arXiv's own LaTeXML rendering, one request and no model
source     0  the submitted TeX, compiled with LaTeXML here
native     2  the PDF's own text layer, read with pdftotext
vision     0  pictures of the pages, read by a model, which is the path that costs money
undecided  1  nobody has worked out how these get read

5 papers in the content plane: 2 on render, 2 on native
```

The decision is not final in one direction.
A paper on the render path whose rendering turns out to hold LaTeXML errors the reject rule will not accept is moved to the source path at extract time, because that is when anybody finds out.
Nothing moves the other way, since a paper with no TeX in it does not acquire any.

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
  labels      3
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

A macro LaTeXML cannot read costs the object it was in and nothing else.
In a bibliography entry that is a reference that prints badly, and in a section body it is a sentence of the paper that is gone, which is the whole of the reject rule.
There are three reasons a macro goes missing and they have different answers.

The first is a package the author wrote and shipped in the tarball.
LaTeXML skips the style files a submission carries unless it is told to read them, so on a first pass every macro defined in them is undefined.
That is what the second pass is for: `ax extract source` converts without them, and if the reject rule throws the result out it converts again with `--includestyles` and keeps whichever of the two is better.
Better means fewer conversion errors inside the body, and a pass that stopped partway through is never the better one however few errors are in the fragment it wrote.
It is a second pass and not the default because a .sty is a program, and LaTeXML reading one is LaTeXML running low level TeX it may have no answer for, so a pass that reads them can come out worse than a pass that skipped them.

The second is a package LaTeXML does model, but models by reading the real .sty out of a TeX tree.
TikZ is the one that matters, and on a machine with no TeX installed there is no file to read, so the binding does not load at all and every TikZ command in the paper is undefined.
A paper full of undefined macros has one cause and three hundred symptoms, so the command names the cause once instead.

```
$ ax extract source -n 1809.03842v1
latexmlc (LaTeXML version 0.8.8)
converting 1809.03842v1 from manual.tex
  Error:undefined:\usetikzlibrary The token T_CS[\usetikzlibrary] is not defined. at manual.tex; line 10 col 15
  Error:undefined:{tikzcd} The environment {tikzcd} is not defined. at manual.tex; line 51 col 0
  Error:undefined:\gate The token T_CS[\gate] is not defined. at manual.tex; line 52 col 25
  and more, all of which are in work/converted/1809/1809.03842v1/paper.html.log
converted 1809.03842v1 in 3.424s with a document LaTeXML gave up partway through
  LaTeXML could not read 13 packages: ltxcmds, keyval, ..., tcolorbox, tikz
  and this machine has no TeX installation, so the packages LaTeXML models by reading the real .sty, which is TikZ among others, could not load at all
1809.03842v1 was rejected: the rendering has 296 conversion errors inside the body of the paper, so a piece of the paper is missing
converting it again with the style files the submission ships, in case what it lost is something the author defined
...
1809.03842v1: reading the style files did not help, so the first conversion is the one kept
ax: 1 of 1 conversions are too broken to use, and those papers need the native path or a person
```

That is the Quantikz tutorial, which draws every one of its figures in TikZ, and it is the worst case on purpose.
Both passes lose the same thing, the paper is refused, and nothing with three hundred holes in it reaches the content plane.
The fix for this one is a TeX installation on the machine doing the conversion, which is a decision about the machine and not about the paper, so the command says which of the two kinds of machine it is running on rather than guessing.

The third is a macro LaTeXML has no answer for at all, and there the decision is the plain one.
A paper that still carries an error inside its body after both passes is refused and goes to the native path or to a person.
It is not published with holes in it and it is not published with the holes papered over, because a reader cannot see either.
Whatever LaTeXML said is kept in a `.log` beside the document, since the line explaining why a macro was ignored is in it and nowhere else.

One case the fault count cannot see is worth naming, because it looks like success.
LaTeXML stops after a hundred errors and writes what it had, so a paper that lost its packages in the preamble arrives as a title with nothing under it and no faults in it at all.
Counting faults reads that as a clean paper of no sections, so the status line is checked as well, and a conversion that stopped partway through is refused whatever its fault count says.

The `labels` line is the source path's other reason to exist, and it is what makes a tag survive a revision.

Numbering does not survive one.
An author who inserts a section renumbers every theorem after it without touching a character of them, so matching last year's Theorem 3 to this year's Theorem 4 by number matches the wrong theorem.
What does survive is the name the author gave it, because relabelling is manual and renumbering is automatic, so `\label{thm:main}` is still `thm:main` in v7 and it is the first thing `ax tags diff` asks about.

That name is not on the page.
LaTeXML resolves every `\label` into the number the paper prints while it converts, and the name reaches neither arXiv's rendering nor the HTML this project makes, which was worth checking before building anything on it.
It is in the XML LaTeXML writes before it paginates, as `labels="LABEL:thm:main"`, and the `xml:id` there is the same string as the `id` on the page.
So the source path runs LaTeXML's two programs one at a time rather than letting `latexmlc` run both, keeps the XML beside the document, and reads the names back onto the objects by anchor.
The page that comes out is the same page, since it is the same post processor doing the same work with the same flags.

The label is then written with the object, as `**Theorem 1** {#thm-1 .statement env=theorem label=thm:main}`, and a section that is a whole file carries it in `label:` in the front matter beside its `local_id`.
A paper read off the render path has none, and that is not a fault: it means the tag matcher works from kind, number and content for that paper instead, which is what the later passes are for.

The front matter records which of the two surfaces the file came from, in `path: render` or `path: source`.
A rendering is what arXiv made of a submission and a conversion is what this project made of the same submission, and they are not always the same document, so a reader comparing two papers has nothing else to go on.
Figures on this path are still undecided: they come out of the conversion as local files next to the document, and putting them through the figure gate is the next piece of work.

`ax extract native` is the third path and the first one that guesses.
The two above it read a document that says where its sections and its theorems are, and this one reads the characters that were printed on a page and works out the rest from the shape they were printed in.

```
$ ax extract native -n 2006.10256v1
pdftotext version 26.09.0
2006.10256v1  arXiv:2006.10256v1 [cs.MS] 18 Jun 2020
  abstract    1 block
  headings    15
  blocks      4 figure, 91 paragraph
  references  58
  faults      none
  pages       1-19, 19 of them typeset, read in 48ms
  characters  55877
```

That is the NumPy paper, which arXiv has no rendering of and whose submission is a PDF the authors produced themselves, so it is on this path for both of the reasons a paper lands here.
A paper also lands here when its conversion came back too broken to use.

Everything above a sentence comes from the layout and nothing comes from markup, because there is none.
A heading is a short line with a gap over it, a paragraph ends where the next one is indented, a caption is a line that opens with the word Figure and a number, and a reference is a line that opens with a bracketed number in the back half of the paper.
Each of those is a guess and each of them is wrong sometimes, which is what `path: native` in the front matter is for.

The thing that had to be measured rather than assumed is what `-layout` does to two columns.
It reads a two column page as one sequence of lines, each line holding the left column's text and then the right column's, so the naive read of a physics paper is every sentence interleaved with a different sentence.
The columns are found by counting, per character column, how many lines have text on both sides of it and how many have a character at it, and the column is quiet where the second number is under a third of the first.
The cut is a single column in the middle of the longest quiet run, not a band, and a page that cannot find its own cut uses the median of the cuts the rest of the paper found.

Two smaller things fall out of that and both were found on real papers rather than reasoned about.
A row whose gutter moved, which is what an italic first word or a wide equation does, is cut at the nearest quiet column within four of the paper's cut rather than left whole, because a reference list row left whole is two entries printed on one line.
And a heading in the right hand column has the left column's prose running alongside it, so there is no gap anywhere near it and the gap rule cannot see it, which is why a line that reads like a heading ends the paragraph above it whether or not the typesetter left a gap.

```
$ ax extract native -n 1710.05832v1
1710.05832v1
  abstract    1 block
  headings    11
  blocks      6 figure, 2 note, 166 paragraph, 1 table
  references  190
  pages       1-18, 18 of them typeset, read in 102ms
  characters  73561
1710.05832v1: the text layer holds 80 characters out of 72018 that are what a broken Type 1 font map prints instead of an operator, so the mathematics in this paper is mojibake and not just flattened, and this one is worth reading on the vision path
```

That is the GW170817 discovery paper, in two columns, with a thousand authors and a reference list of a hundred and ninety entries, and its six sections come back with the numerals the journal printed them with.

The last line is the honest record this path owes a reader, and it is the reason the table above says this path produces no mathematics.
`pdftotext` gives back the characters a PDF asks for, and a formula in a PDF is not a formula: it is glyphs positioned on a page, so a fraction arrives as a numerator, a rule of hyphens and a denominator on three lines.
Displayed mathematics is kept as the shape it was printed in rather than joined into a sentence, so a reader sees what the page said, and nothing on this path is ever published as LaTeX, because there is no LaTeX to publish and inventing some would be worse than admitting there is none.
Where the font map is broken as well the glyphs are not even the right characters, and that is a paper for the vision path, so the count of odd characters is reported rather than quietly published.

The reference list is recovered and is not written out yet.
`ax extract native` finds 58 of the NumPy paper's 73 entries and all 190 of the LIGO paper's, and `ax refs build` reads a rendering, so on this path those entries reach the paper and not `refs/`.
Teaching that command to read a PDF is the next piece of work on this path, and the reason the count is 58 and not 73 is worth writing down: `pdftotext` squeezes some reference rows down to a one space gutter, which cannot be told from a word space, so those entries come back joined to the one above them.

A PDF holding a scan is refused here rather than read.

```
$ ax extract native -n 1802.00001v1
1802.00001v1 holds no text on any of its 14 pages beyond what arXiv stamped down the margin, so it is a scan or a submission made of page images, and it is on the vision path
1 of 1 PDFs hold no text layer worth reading, so those papers are on the vision path
```

It is its own kind of error so that a batch of a hundred steps over one and carries on, which is the same arrangement the source path has for a submission that turned out to be a PDF.
`-anyway` reads it regardless, for the paper that missed the threshold by a page and that somebody has actually looked at.

The margin stamp gets one further use.
It is the only statement inside a PDF about which version of a paper the file holds, so a file whose stamp says v4 when the corpus decided it may publish v1 stops the command rather than being reported, because extracting it would mean publishing something nobody was given.

Which `pdftotext` is on the PATH changes what comes out, since poppler lays a page out differently between releases, so the version is printed on every run and `-pdftotext` names another one.
There is no poppler on the CI runner, so what runs there is a script that behaves the way `pdftotext` behaves, the same arrangement `latexml` and `latex` already have.

`ax extract vision` is the fourth path and the last one.
A paper gets here when arXiv never rendered it, its submission is not TeX this project can convert, and its PDF has no text layer worth reading, which is a scan or a PDF whose glyphs carry no characters.
Most of those are from the 1990s, and before this path existed they were simply missing.
The LIGO paper above is the milder version of the same problem: it has a text layer, and 80 characters of that layer are what a broken font map prints instead of an operator.

The reader is a program and not a client library.

```
$ ax extract vision -reader ~/bin/read-page -model claude-opus-5 1802.00001v1
```

It is run once per page with the picture as its only argument, the prompt on standard input and the model in `AX_VISION_MODEL`, and whatever it writes to standard output is the reading of that page.
That is three lines of shell for anybody who wants to point this at another service, it keeps every credential out of this repository, and it means the half of the path that costs money is a file a person can read before spending any.
It is also the reason CI exercises this path at all: what runs there is a script that writes back a page of Markdown, which is the arrangement `pdftotext`, `latexml` and `latex` already have.
There is no default reader, because a path that spends money on every page should make somebody write down what it is spending it on.

Then the ladder, which is 300, 400 and 600 dots an inch.
300 is what text wants, 400 is for a paper set in small type, and 600 is for a photocopy of a photocopy.
Above 600 the picture stops getting better and only gets bigger.
A page is drawn at 300 and read, and it is only drawn again at the next rung if the acceptance rules refused what came back, so the common case pays for one picture and one reading.

Those rules are the whole of the trust in this path, and they are deliberately not part of the audit's seven groups: the audit reads a corpus, and these decide whether text is allowed into one.
V01 is an empty reading, V02 is a model talking about the page instead of reading it, V03 is a line that came back four times or more, V04 is the prompt read back, V05 is a page identical to the page before it, V06 is more than 12000 characters, V07 is a reading that is 2 per cent replacement or unprintable characters, and V08 is an unclosed fence, display or environment.
V06's ceiling is audit rule S08's ceiling, and there is a test that says so, because a reading accepted here becomes a file the audit then reads and two different numbers would mean this path admits pages the corpus fails on.
V09 is the only one that compares the reading against the page rather than against itself: it takes twenty consecutive words of the PDF's own thin text layer and refuses the reading when none of their content words appears anywhere in it.
It took two attempts to write, because a first version that asked for any word in common fires on every page of English ever printed, on `the` and `of` alone.

A page that every rung refused is a page nothing read, and the paper is then not written at all.

```
$ ax extract vision -reader ~/bin/read-page -model claude-opus-5 1802.00001v1
page 7 at 300 dots: V01: nothing came back for this page
page 7 at 400 dots: V01: nothing came back for this page
page 7 at 600 dots: V01: nothing came back for this page
1802.00001v1: page 7 could not be read at 300,400,600, so the paper is short of those pages and is not being written
```

A corpus with a paper missing from it is honest and a corpus with a paragraph nobody printed in it is not, so there is no option that publishes the paper with a note where the page should be.
`-anyway` keeps a refused page, which is what a genuinely blank page needs, since V01 cannot tell a blank page from a reader that failed quietly, and the record keeps both the refusal and the decision to override it.

What the run did is written next to the readings, in `work/vision/<shard>/<id>v<n>/read.yaml`.

```
# What one vision run read, page by page, and what the rules said about it.
paper: "1710.05832"
version: 1
model: demo-stub
prompt_sha256: 13d548c649a3e319054ca1f230d40cda8b9f2add69f612b6649ef24043116282
painter: pdftoppm version 26.09.0
pages:
    - page: 1
      dpi: 300
      file: p001.md
      sha256: 13afb02ba44df75492f537f1882b1dab50f853ad8d380aeda4b7238130e7f234
      chars: 7599
      tried:
        - 300
      read: 2026-09-15T05:18:39Z
```

That file is what makes the path resumable, and it is saved after every page rather than at the end, because a run interrupted on page thirty of forty has to leave thirty pages behind that the next run does not pay for again.
A reading is reused when the model is the same, the hash of the prompt is the same, and the file still hashes to what the entry says.
Change the model or the prompt and the paper is read again, since a reading made under different instructions is a different reading even when it looks the same.

```
$ ax extract vision -reader /tmp/stub-reader -model demo-stub 1710.05832v1
  pages    18, 18 read now, 0 already read
  asks     18
  painter  pdftoppm version 26.09.0
  took     27s
$ ax extract vision -reader /tmp/stub-reader -model demo-stub 1710.05832v1
  pages    18, 0 read now, 18 already read
  took     0s
```

That is the GW170817 paper again, eighteen pages, and the reader there is the stub: it hands back the paper's own text layer for each page.
So the pictures, the ladder, the rules, the record and the 27 seconds are real and the structure is not, because a stub that returns flat text has no headings in it for the Markdown reader to find.
The two counts are separate on purpose: pages are pages and asks are what the model was asked, which differ as soon as one page needs a second rung, and it is the asks that were paid for.

`-recheck` puts the rules to the readings already on disk and wakes no model up.

```
$ ax extract vision -recheck 1710.05832v1
page 4 has been edited since it was read, so the record no longer speaks for it
page 4: V06: the reading holds 13328 characters, which is more than a printed page can hold
1710.05832v1: page 4 no longer passes the rules, so the paper as it stands is short of those pages
```

Everything the rules ask about is text that is already there, so a new rule is written, this is run over every paper already read, and what comes out is the list of pages the new rule objects to.
That costs nothing, which is the point: a rule that is expensive to try out is a rule nobody adds.

Two fields in the front matter exist only for this path.
`extraction_model` is the model that read the pages and `prompt_sha256` is the hash of the instructions it read them under, and both are in every file the path produced rather than in a log somewhere.
A page read by a model nobody can name under a prompt nobody kept is a page nobody can read again.
`path: vision` says the reading was made by looking, while the `route` in the sources manifest still says `native`, because this path reads the same PDF the native path reads.

The pictures are working state and they are large: eighteen pages at 300 dots an inch is 37 MB, which is about two megabytes a page and several times the PDF.
They live under `work/pages/` next to the readings, they are never published, and they are kept rather than deleted so that a page can be looked at again without drawing it again.

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

`ax figures tikz` is the one picture this project makes itself.
Every figure above started as something arXiv had already turned into pixels, and a drawing described in TikZ is a program the author wrote, so compiling it here gives a vector picture with real text in it that a screen reader can read and a translator could in principle translate.
It reads the submission rather than the rendering, which is why it is a subcommand and not a flag: they are two different files about the same paper.

```
$ ax figures tikz -n 2501.00001v3
2501.00001v3               2 drawings of its own
  ms.tex line 8            fig:one   The \emph{first} drawing.
  sections/one.tex line 3  no label  The second drawing, reproduced from \cite{smith2019}.
```

Every TeX file in the submission and not only the main one, because an author with fifty figures keeps them in fifty files and inputs them.
The preamble comes from the main document either way, and it goes into the compiled document whole rather than cut down to the lines that look like they matter, because a drawing uses the author's own macros and colours and libraries and a preamble trimmed by eye is one that compiles nine pictures out of ten and leaves the tenth undefined for a reason nobody can see.
A drawing the author commented out is not a drawing: people leave the version they did not use in the file, and compiling one would put a figure in the corpus that is not in the paper.

`-n` asks nothing of TeX, which is the point of it.
Knowing which drawings a paper has is worth having on a machine that cannot compile them, and the machine this was written on is one.

Without `-n` each drawing becomes its own document, `\documentclass[tikz,border=2pt]{standalone}` so the page is cropped to the picture, and that goes through `latex` and then `dvisvgm`.
The DVI route and not the PDF one, because dvisvgm reads a DVI's text as text and `--font-format=woff2` keeps it as text in the SVG, and a drawing whose labels are paths is a drawing no screen reader can read, which is most of the reason this path is worth having.
It runs in the submission's own directory, since a drawing reads the author's macro files and includes the author's images and both are written relative to the paper, and everything it writes is named for the hash of the document and swept afterwards, so the author's files are left as they were found.
Shell escape is off and says so on the command line, because a tikzpicture can ask TeX to run a program and this compiles a file a stranger uploaded to arXiv.
One picture gets two minutes, which is generous for a drawing and short next to the five a whole paper's conversion gets, and a drawing with a loop in it is stopped and falls back to the rendering's version.

What comes back is shaped before it is committed.
The root loses its width and height and keeps a viewBox, and a scaling transform on the top level group is folded into the viewBox rather than left where it is.
09-publish.md asks for that and names the reason: SVG is a core media type in EPUB and is safe everywhere with one known trap, which is that scaling a picture by a transform on a group breaks on some Kobo devices.
The two say the same thing and only one of them works on the reader somebody actually owns.
A rotation or a skew is refused rather than folded, because those change the shape of the frame rather than its size and folding one would mean writing a viewBox that is not a rectangle.

The picture is measured as it was compiled and committed as it will be published.
dvisvgm states the size in points, which is the size the drawing would print at and is what rule F06 reads, and the shaping takes that off, so the physical size comes from the one and the coordinates and the bytes from the other.
Then it goes through the same gate as every other figure.
A drawing is the author's own by construction, which answers who owns it and answers nothing about the licence of the version it is in.

The gate needed one repair to work here at all.
`F09` reads the four credit words next to a citation, and a citation was a link into the bibliography, which is what a caption looks like once something has converted it.
A caption read off a submission still has `\cite{smith2019}` in it, so the rule was quietly saying owned about every drawing this path produces.
It now knows the author's own `\cite` as well as the link, which is the same rule reading a third form of the same thing.

One drawing TeX will not compile costs the corpus that drawing and not the paper, because the rendering's version of the figure is already there and this path is the better copy of a picture and never the only one.
Every drawing failing is an installation that cannot build the paper, and that is reported as one thing rather than as a paper full of bad pictures.

TeX is the one dependency this project cannot vendor, so a machine without one says how to get one.

```
$ ax figures tikz 2501.00001v3
ax: tikz: latex is not on the PATH, and compiling a drawing from the author's own description needs a TeX installation here rather than at arXiv, so install one with brew install --cask mactex-no-gui or apt install texlive-pictures texlive-latex-extra dvisvgm
```

There is no TeX on the machine this was written on and none on the CI runner, so what runs there is two scripts that behave the way `latex` and `dvisvgm` behave, the same arrangement `latexml` already has.
That leaves everything this project wrote under test, which is the drawings it finds, the document it builds, the flags it passes, the shaping of the SVG and the gate over what came back, and it leaves the compile itself checked by nothing but the day somebody runs it with a real TeX.

Two things this does not do yet.
The caption in the manifest is the author's TeX rather than prose, because nothing has converted it at the point the drawing is found, and the body of the paper takes its caption from the conversion.
And a drawing written with the `\tikz` shorthand instead of the environment is not found, which is why a paper that loads TikZ and yields no drawings says so rather than saying it draws nothing.

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
# version: v1
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

`ax tags diff` is how a tag survives the author posting a new version.

An author posts v3 with a table added in the middle and every table after it renumbered.
Every identifier in the register still names something, so an assignment that matches on the identifier alone keeps every tag, finds nothing missing, reports nothing wrong, and has just moved every table's permanent name onto the table after it.
A reference that breaks is visible and a reference silently pointed at the wrong table is not, so the register records which version its identifiers belong to and `ax tags assign` refuses to run once the content plane has moved past it.

```
$ ax tags diff 2305.18290 -from v1 -to v3
2305.18290      v1 to v3, 55 objects to 58
  label         33
  number        21
  content       0
  sequence      1
  new           3
  gone          0
  fell through  7 per cent
  label         tab-1 becomes tab-2
  sequence      sc-2 becomes sc-3
  label         tab-2 becomes tab-3
  new           tab-1 is in v3 and nothing in v1 is it
```

That is the direct preference optimization paper, which gained a table and two other objects between May 2023 and July 2024.
Thirty three objects were matched by the label their author wrote, which is what carried every table's tag past the renumbering, and one subcaption was matched by the alignment.
Matches that put an object back where it already was are counted and not listed, since there are dozens of them and none of them is a decision worth arguing with.

The four passes are 03-tags.md section 6 and they run strongest evidence first, stopping at the first one that matches an object.
Pass one is the author's own `\label`, which survives a revision in a way numbering does not, because renumbering is automatic and relabelling is manual.
Pass two is the kind and the number in the same section, which is the ordinary case where nothing moved.
Pass three is a hash of the prose with the case, the mathematics and the spacing taken out of it, which is what catches a section moved wholesale.
Pass four aligns what is left in reading order by edit distance and keeps the pairs that read more than half alike, which is what catches a statement whose wording was lightly edited.
A pass only pairs two objects when exactly one on each side carries the key, because the same label on two objects is evidence of nothing.

`-w` writes the decisions into the register.
A tag whose object moved is pointed at where the object is now, and a tag whose object is not in the new version at all is not deleted.
It stays with a tombstone saying which version it was last present in, and the reading app serves it with a page saying so rather than a 404.

```
2X40,eq-7,gone:v3,"removed when section 4 was rewritten"
```

That is straight from the Stacks Project, which keeps the tag of a result that turned out to be wrong along with an explanation of its disappearance.
A reference that resolves to an explanation is a working reference and a reference that 404s is a broken promise.
The note is prose, and the command writes a placeholder saying what the object was and where it was, so somebody who knows that section four was rewritten should replace it with that.

Nothing is written when more than a fifth of a paper's objects fall through to pass four or match nothing at all, because a paper that changed that much is a paper somebody should read the diff of first.
The label is on the source path only, since neither arXiv's HTML nor LaTeXML's own carries it, so a paper on the render path is matched by number, content and sequence, which works and is weaker.
Both versions are read out of `work/`, which is where every conversion and every rendering is kept with its version in the name, and which is the reason they are kept: the content plane holds one extraction of a paper and a comparison needs two.

`ax tags report` is the same question asked of the whole corpus instead of one paper.

```
$ ax tags report
58 tags over 1 paper, 0 tombstoned
116 objects across 2 revisions, 3.4% fell through to pass four
  label     68
  number    44
  content   0
  sequence  1
  new       3
  gone      0
written to reports/tags.md
```

The fall through rate is the one number that says whether the permanent names in this corpus are being read off or guessed at.
Passes one to three match on equality and pass four matches on similarity, so an object that reaches pass four had its name decided by a judgement, and an object no pass could match got a new name on the same judgement from the other side.
The gate in `ax tags diff` refuses one paper at a time and only when somebody runs it, and this is how the corpus answers before a run meets the line.

Every version is compared against the next rather than the first against the last, because that is the comparison the corpus actually makes: a register is carried from the version it was assigned against onto the one after it.
That is why the direct preference optimization paper reads 3.4 per cent here and 7 per cent in the diff above, which is the same paper measured over two steps instead of one.
Each revision is matched again at report time and not read back from what the matcher said when the register was carried, because the matcher is the thing being measured and a number kept from an older one would answer about that instead.

A version the cache does not hold is skipped rather than fetched, so this reads the disk and never the network, and `reports/tags.md` lists each paper's cached versions next to its tags, which is what says why a paper with four versions has one revision in it.
A corpus with one version of everything has compared nothing, and the report says so rather than printing a rate of nought, which would claim every name was read off when nothing had looked.

`ax audit -plane content` is where all of that gets checked rather than trusted.

```
$ ax audit -plane content -q
rule  state    checked  findings
S01   pass     26
S04   pass     2
S07   pass     26
S08   not run  0
S09   not run  0
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
`S08` and `S09` are the two halves of asking whether the file that was read was the paper, and they say not run over this corpus because only the native path knows how many pages a file came off and neither of these two papers is on it.
`S08` is a file longer than its own pages could carry, which is a page numbering that was lost or two papers in one file, and `S09` is a paper that came back as a handful of characters over twenty pages, which is a PDF whose text layer was a cover sheet.
The second of the two is asked of the paper and the first of a file, because a short section shares its page with the rest of the paper and a section of one sentence is ordinary.

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
A paper off the native path is not asked, because a PDF's text layer holds the characters a formula was printed as and not the formula, so that path flattens every formula it reads and says so in `path: native`.
A rule that reports a path for doing the one thing it states it does is a rule that fires on a thousand papers and means nothing on any of them.

Group F reads the pictures and the tables, and the shape of it is that a figure is three things: a decision in the manifest, bytes on disk and a line in a body.
Every way a corpus goes wrong here is two of those three disagreeing.
`F01` is the one that goes to disk, from both sides, and it is also the rule that says `ax figures` has never been run: the extractor leaves arXiv's own path in the body until something decides about the picture, so a body still pointing at `2404.19756v5/kan_mlp.png` is a picture nobody has established the permission for.
That is not a hypothetical. It is what this rule found on KAN the first time it ran, where the one figure still served off arXiv was the teaser above the abstract, because `ax figures` walked the sections and never the abstract.
`F08` and `F09` are the two that are about permission rather than about quality, and both are deliberately blunt.
`F09` treats suspected exactly as confirmed, because the cost of being wrong one way is a missing picture and the cost of being wrong the other way is republishing somebody else's copyrighted work under a licence they never granted.
`F02`, `F03`, `F05` and `F06` read what `ax figures` measured rather than measuring again, since the command is what measures and a second opinion in the audit is a second answer to defend.
`F07` is the rule that loads the manifest at all, the way `G01` loads the register, so a manifest that does not parse is one finding here rather than six across the group, and the caption is checked by the loader and not again by the rule.

`F04` asks git rather than the corpus, which makes it the other half of the pair `G08` is in.
A figure is the only part of the corpus that is bytes and not text, so a picture no commit records is a picture nobody else has, and the same directory is where `F09` takes bytes out, which means a figure withheld here and still sitting in somebody's working tree is exactly what this finds.
Ignored is reported separately and is the worse of the two, because a rule that reads a directory git has been told to skip is a rule that cannot fail again.
A file that is not where a paper's figures go is reported whatever the run was looking at, since `ax figures` is the only program that writes under `figures/` and a file anywhere else there belongs to no paper to scope it to.
A corpus that is not a repository leaves it not run, the same way `G08` does, because a pass would be saying every picture in the corpus is in the repository and nothing has looked.

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

`G07` and `G08` are the two that are about a revision rather than about one version of a paper, and neither is in the run above, which was taken before they existed.
`G07` is the tombstone: a tag whose object is gone keeps its line, and the line has to say which version it went in, because a tombstone with no version answers the reader's question with the question.
`G08` is one of the two rules in the audit that read the repository rather than the corpus.
A tag is assigned once and never changes, which is a promise about lines that are no longer in a file, and a file cannot show you what used to be in it.
It reads every register line the history took out, and every one the working tree has taken out since the last commit, and a tag that went and never came back is the finding.
A removal is repaired by a revert and not by putting the line back, because anything that cited the tag in the meantime needs the history to say so.
A corpus nobody has committed leaves it not run, because a pass there would be saying that every tag ever handed out is still where it was and nothing has looked.

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

Five rules of group S are not here.
`S05` is the one that reads a record rather than a file, so it runs over the metadata plane and is in that report instead.
`S02` and `S06` are the two about translated files, one saying a no-derivatives paper has none and the other holding a translation to the licence the propagation table gives it, and nothing is translated yet, so both arrive with M8.
`S03` and `S11` are git and the takedown manifest, neither of which this tool has to read yet.

Three rules of group R are not here.
`R07` is the acquire selection report, which is a paper three or more papers in the content plane cite and which is not in the plane itself, and nothing writes that report yet.
`R08` reads a rendered reference section and nothing renders one yet.
`R09` compares a paper's resolution rate against its category's median, which needs the same baselines `M06` needs, so the two arrive together in M6.
`M04` and `M06` are not here: `M04` is every span parsing under KaTeX and the reader that carries KaTeX is M6, and `M06` compares a paper's displays per page with its category's median, which needs baselines computed over a corpus with more than two papers in it.
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
It is a small fraction of the first plane and it always will be, so which papers are in it is a decision, and `manifests/selected.yaml` is that decision with a reason on every row.

## The four extraction paths

| Path | Input | Model |
| --- | --- | --- |
| render | arXiv's own LaTeXML HTML5, at `/html/<id>` | none |
| source | the submitted TeX, compiled with LaTeXML here | none |
| native | the PDF's text layer | none |
| vision | page images, read by a vision model | yes |

Three of the four use no model at all, which matters more than it sounds.
About 90 per cent of arXiv has TeX source and about 97 per cent of recent submissions have an HTML rendering, so the paths that cost nothing cover almost everything and the vision path is the fallback for scanned submissions.
Which path a given paper is on is decided by `ax path decide` off what its submission holds, whether arXiv renders that version and whether its PDF has a text layer, and it is recorded per paper in `manifests/selected.yaml`.

## Related

- [tamnd/arxiv](https://github.com/tamnd/arxiv), the corpus this builds
- [tamnd/arxiv-cli](https://github.com/tamnd/arxiv-cli), the twelve arXiv surfaces, the id parser and the `ax://` URI space
- [tamnd/llm](https://github.com/tamnd/llm), the model transport, routing, queue and ledger

## Licence

Apache 2.0.
The corpus it builds is licensed separately and per paper, which is the point of the licence gate.
