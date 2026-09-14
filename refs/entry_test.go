package refs

import (
	"testing"

	"github.com/tamnd/arxiv-reader/extract"
)

// The two entries below are real, taken from the renderings of 2312.00752 and
// 2404.19756. They are the two bibliography styles this parser has to read: an
// author-year style that quotes the title, and a numeric style that does not
// quote it and relies on the order of the blocks.
var authorYear = extract.Bibitem{
	ID:    "bib.bibx1",
	Label: "Arjovsky et al. (2016)",
	Blocks: []string{
		"Martin Arjovsky, Amar Shah and Yoshua Bengio",
		"“Unitary Evolution Recurrent Neural Networks”",
		"In *The International Conference on Machine Learning (ICML)*, 2016, pp. 1120–1128",
	},
}

var numeric = extract.Bibitem{
	ID:    "bib.bib12",
	Label: "[12]",
	Blocks: []string{
		"Ming-Jun Lai and Zhaiming Shen.",
		"The kolmogorov superposition theorem can break the curse of dimensionality when approximating high dimensional functions.",
		"arXiv preprint arXiv:2112.09963, 2021.",
	},
}

func TestAQuotedTitleStyle(t *testing.T) {
	e := entry(authorYear)
	if e.Title != "Unitary Evolution Recurrent Neural Networks" {
		t.Errorf("title is %q", e.Title)
	}
	if got, want := len(e.Authors), 3; got != want {
		t.Fatalf("%d authors, want %d: %q", got, want, e.Authors)
	}
	if e.Authors[2] != "Yoshua Bengio" {
		t.Errorf("last author is %q", e.Authors[2])
	}
	if e.Venue != "In The International Conference on Machine Learning (ICML), 2016, pp. 1120–1128" {
		t.Errorf("venue is %q", e.Venue)
	}
	if e.Year != 2016 {
		t.Errorf("year is %d", e.Year)
	}
}

func TestANumericStyleHasItsTitleByPosition(t *testing.T) {
	e := entry(numeric)
	if e.Title != "The kolmogorov superposition theorem can break the curse of dimensionality when approximating high dimensional functions" {
		t.Errorf("title is %q", e.Title)
	}
	if got, want := len(e.Authors), 2; got != want {
		t.Fatalf("%d authors, want %d: %q", got, want, e.Authors)
	}
	if e.ArXiv != "2112.09963" {
		t.Errorf("arXiv id is %q", e.ArXiv)
	}
	if e.Year != 2021 {
		t.Errorf("year is %d", e.Year)
	}
}

// A page range is the trap the year pattern exists for. This entry prints 1120
// and 1128 after the year it actually states, so a parser reading four digits
// anywhere would answer 1128 and a parser taking the last would agree with it.
func TestAPageRangeIsNotAYear(t *testing.T) {
	if got := entry(authorYear).Year; got != 2016 {
		t.Errorf("year is %d, want 2016", got)
	}
}

func TestTheOldStyleArXivIDIsRead(t *testing.T) {
	e := entry(extract.Bibitem{
		ID:     "bib.bib1",
		Blocks: []string{"Gerard Salton.", "A theory of indexing.", "arXiv:math.CO/0605137, 2006."},
	})
	if e.ArXiv != "math.CO/0605137" {
		t.Errorf("arXiv id is %q", e.ArXiv)
	}
}

// The DOI is printed as its own text and linked to dx.doi.org, so the entry
// carries the same string twice with the Markdown link syntax between the two
// copies. Both the DOI and the link it points at come out whole.
func TestADOIAndTheLinkItPointsAt(t *testing.T) {
	e := entry(extract.Bibitem{
		ID: "bib.bibx29",
		Blocks: []string{
			"Yassir Fathullah, Chunyang Wu and Mark Gales",
			"“Multi-Head State Space Model for Speech Recognition”",
			"In *Proc. INTERSPEECH 2023*, 2023, pp. 241–245",
			"DOI: [10.21437/Interspeech.2023-1036](https://dx.doi.org/10.21437/Interspeech.2023-1036)",
		},
	})
	if e.DOI != "10.21437/Interspeech.2023-1036" {
		t.Errorf("DOI is %q", e.DOI)
	}
	if e.URL != "https://dx.doi.org/10.21437/Interspeech.2023-1036" {
		t.Errorf("URL is %q", e.URL)
	}
}

// An entry printed as an author line and then everything else is one the parser
// does not read a title out of. Half of that second block is the title and half
// of it is the year, and a split that guesses where would invent a field.
func TestATwoBlockEntryIsNotGuessedAt(t *testing.T) {
	e := entry(extract.Bibitem{
		ID: "bib.bib44",
		Blocks: []string{
			"Aojun Lu, Tao Feng, Hangjie Yuan, Xiaotian Song, and Yanan Sun.",
			"Revisiting neural networks for continual learning: An architectural perspective, 2024.",
		},
	})
	if e.Title != "" {
		t.Errorf("title is %q, and there was nothing to read it from", e.Title)
	}
	if got, want := len(e.Authors), 5; got != want {
		t.Errorf("%d authors, want %d: %q", got, want, e.Authors)
	}
	if e.Year != 2024 {
		t.Errorf("year is %d", e.Year)
	}
}

// The text is the line the paper printed and it is published whatever the
// fields come to, so an entry nothing could be read out of still carries one.
func TestTheTextIsAlwaysKept(t *testing.T) {
	e := entry(extract.Bibitem{ID: "bib.bib9", Blocks: []string{"Bourbaki, Elements of Mathematics."}})
	if e.Text != "Bourbaki, Elements of Mathematics." {
		t.Errorf("text is %q", e.Text)
	}
}

func TestTheEmphasisComesOffTheFields(t *testing.T) {
	e := entry(extract.Bibitem{
		ID:     "bib.bib16",
		Blocks: []string{"Hadrien Montanelli and Haizhao Yang.", "Error bounds for deep relu networks.", "*Neural Networks*, 129:1–6, 2020."},
	})
	if e.Venue != "Neural Networks, 129:1–6, 2020." {
		t.Errorf("venue is %q, and a venue is compared against a metadata plane that holds it plain", e.Venue)
	}
}

func TestAnEntryWithNoAnchorIsStillReadable(t *testing.T) {
	out := Read([]extract.Bibitem{authorYear, numeric})
	if len(out) != 2 {
		t.Fatalf("%d entries, want 2", len(out))
	}
	if out[0].ID != "bib.bibx1" || out[1].ID != "bib.bib12" {
		t.Errorf("the entries came back as %q and %q", out[0].ID, out[1].ID)
	}
}

func TestPeople(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"Martin Arjovsky, Amar Shah and Yoshua Bengio", 3},
		{"Ming-Jun Lai and Zhaiming Shen.", 2},
		{"A. Vaswani, N. Shazeer & N. Parmar", 3},
		{"", 0},
	} {
		if got := people(c.in); len(got) != c.want {
			t.Errorf("%q split into %d names, want %d: %q", c.in, len(got), c.want, got)
		}
	}
}
