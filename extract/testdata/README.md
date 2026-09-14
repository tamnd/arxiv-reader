# What is in here

Two LaTeXML renderings of a paper that does not exist.

The markup is not invented.
Every shape in these files was copied from arXiv's own rendering of a real paper, 2312.00752v2, and then had the paper's words taken out and replaced with words nobody owns.
That is deliberate twice over.
Taking the shapes from a real rendering is the only way a test says anything about what this package will meet in production, and taking the words out keeps a cc-by paper and its attribution obligation out of this repository's test fixtures.

If arXiv changes its markup, the way to update these files is to fetch a rendering, look at it, and change the shapes here to match.
Do not change them to match what the reader already does.

`rendering.html` is a paper that parses.
It has a title, two authors with a thanks note and a contact line, an abstract, sections nested three deep with a run-in paragraph heading, an equation group, a numbered display equation, a figure with an SVG, a figure of two panels where one panel is a table, a table float with a header row and a spanning cell, an algorithm float with a numbered listing, an itemize, a theorem with a run-in title, a proof, a quote, a piece of mathematics LaTeXML could not parse, and a bibliography with one entry it could not read at all.

`faulty.html` is the same paper with the conversion error moved into the body of section 1, which is what makes it a rejection.
