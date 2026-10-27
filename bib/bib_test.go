package bib

import (
	"reflect"
	"testing"

	"github.com/tamnd/arxiv-reader/source"
)

func TestFromSource(t *testing.T) {
	tex := `\begin{thebibliography}{99}
\bibitem[Hir64]{Hir64} H. Hironaka, Resolution of singularities, Ann. of Math. 79 (1964), 109--326.
\bibitem{KK07} J. Kahn and G. Kalai, Thresholds and expectation thresholds, Combin. Probab. Comput. 16 (2007), arXiv:math/0603218.
\bibitem{PP}
A. Park and H. Pham, A proof of the Kahn--Kalai conjecture, J. Amer. Math. Soc. (2024), \arxiv{2203.17207}, doi:10.1090/jams/1028.
\bibitem[{Sw}]{Sweedler}M. Sweedler, \emph{Hopf algebras}, 1969.
\bibitem{Ben90} C. Bennett, Stability, Physical Review A \textbf{41} (1990), 1932--1935.
\end{thebibliography}
% \bibitem{notakey} an entry in a comment.
`
	bbl := `\entry{BMP}{article}{}
  \field{eprint}{1905.01234}
  \verb{doi}
  \verb 10.1007/S00222-020-00987-X
  \endverb
  \field{year}{2020}
\endentry
`
	bibfile := `@article{Hir64, title={Resolution}, year = 1964, doi = {10.2307/1970486}}
@misc{unused, eprint = {2101.00001}, archivePrefix = {arXiv}, year = {2021}}
@string{jams = "J. Amer. Math. Soc."}
`
	got := FromSource([]source.File{{Name: "main.tex", Data: []byte(tex)}, {Name: "refs.bbl", Data: []byte(bbl)}, {Name: "refs.bib", Data: []byte(bibfile)}})
	want := []Entry{
		{Key: "BMP", ArXiv: "1905.01234", DOI: "10.1007/s00222-020-00987-x", Year: 2020, From: "biblatex"},
		{Key: "Ben90", Year: 1990, From: "bibitem"},
		{Key: "Hir64", DOI: "10.2307/1970486", Year: 1964, From: "bibitem"},
		{Key: "KK07", ArXiv: "math/0603218", Year: 2007, From: "bibitem"},
		{Key: "PP", ArXiv: "2203.17207", DOI: "10.1090/jams/1028", Year: 2024, From: "bibitem"},
		{Key: "Sweedler", Year: 1969, From: "bibitem"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}

func TestABibFileStandsAloneWhenTheSourceHasNoOtherForm(t *testing.T) {
	got := FromSource([]source.File{{Name: "refs.bib", Data: []byte(`@misc{a, eprint = {2101.00001}, year = {2021}}`)}})
	want := []Entry{{Key: "a", ArXiv: "2101.00001", Year: 2021, From: "bibtex"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
