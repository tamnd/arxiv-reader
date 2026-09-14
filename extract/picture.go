package extract

import "fmt"

// Pictures is what was decided about each of a paper's figure files, keyed by
// the path arXiv's rendering gave.
//
// The decision is made by ax figures and it is written down in the figure
// manifest. This package only reads it, and the reason it reads it at all is
// that one thing has to write the content plane. If ax figures edited the
// Markdown itself then the next ax extract render would put the arXiv paths
// back, and the two commands would take turns undoing each other.
//
// A path this has never heard of is a picture nothing has been decided about
// yet, which is the state of every figure until ax figures has run.
type Pictures map[string]Picture

// Picture is what happened to one figure file.
type Picture struct {
	// File is where the bytes were committed, rooted at the corpus, and empty
	// for a picture that was withheld.
	File string
	// Withheld is the sentence saying why, empty for a committed picture.
	Withheld string
	// At is where the picture can be seen on arXiv, which is published with the
	// reason so that a withheld figure is a signpost and not a hole.
	At string
}

// picture writes one image line.
//
// Three states and they are deliberately different lines rather than one line
// with a flag on it. A committed picture is an image. A withheld one is a note
// in the reader's language saying what is missing and where to see it, because
// the caption, the number and the tag are published either way and a reader who
// meets a caption with nothing above it has been left to guess. A picture
// nothing has decided about yet says truthfully where it is, which is on arXiv.
func (w *body) picture(img Image) {
	pic, known := w.pics[img.Src]
	switch {
	case !known:
		w.para(fmt.Sprintf("![%s](%s)", img.Alt, img.Src))
	case pic.File != "":
		w.para(fmt.Sprintf("![%s](%s)", img.Alt, pic.File))
	default:
		w.para(withheld(pic))
	}
}

func withheld(pic Picture) string {
	s := "The image is withheld"
	if pic.Withheld != "" {
		s += " because " + pic.Withheld
	}
	s += "."
	if pic.At != "" {
		s += " It is in the paper on arXiv at " + pic.At + "."
	}
	return "*" + s + "*"
}
