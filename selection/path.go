package selection

import (
	"fmt"
	"strings"
)

// Path is which of the four ways a paper gets read.
//
// The order is the order they are preferred in, and the preference is about cost:
// a rendering is one request and no model, a source compile is local work, a text
// layer is local work over a file already fetched, and the vision path is the only
// one that spends money per page.
type Path string

const (
	// PathRender is arXiv's own LaTeXML rendering of the version.
	PathRender Path = "render"
	// PathSource is the submitted TeX, compiled with LaTeXML here.
	PathSource Path = "source"
	// PathNative is the PDF's own text layer.
	PathNative Path = "native"
	// PathVision is pictures of the pages, read by a model.
	PathVision Path = "vision"
)

// Paths are the four, in the order they are preferred in.
var Paths = []Path{PathRender, PathSource, PathNative, PathVision}

// KnownPath says whether this is one of the four.
func KnownPath(p Path) bool {
	for _, k := range Paths {
		if k == p {
			return true
		}
	}
	return false
}

// ParsePath reads a path off the command line.
func ParsePath(s string) (Path, error) {
	if KnownPath(Path(s)) {
		return Path(s), nil
	}
	return "", fmt.Errorf("selection: %q is not one of the four paths, which are %s", s, PathNames())
}

// PathNames is the four, for a message.
func PathNames() string {
	out := make([]string, 0, len(Paths))
	for _, p := range Paths {
		out = append(out, string(p))
	}
	return strings.Join(out, ", ")
}

// Answer is yes, no, or nobody has looked.
//
// Three values and not a bool, for the same reason an empty licence is not the
// same as an unknown one. A paper nobody has asked about is not a paper the answer
// is no for, and a decision tree that cannot tell the two apart sends every
// unexamined paper down the fallback path, which here is the one that costs money.
type Answer string

const (
	// Yes is the fact established.
	Yes Answer = "yes"
	// No is the fact established the other way.
	No Answer = "no"
	// Nobody is nobody having looked, which is what an empty field means.
	Nobody Answer = ""
)

// Facts are what the path decision has to go on.
//
// All three are cheap. Whether the submission holds TeX is visible in the e-print
// as soon as it lands, whether arXiv renders this version is one HEAD, and whether
// the PDF holds a text layer is pdftotext over a file already on disk. None of them
// is a model call, which is the whole point: the decision that says whether a paper
// needs the expensive path must not itself be expensive.
type Facts struct {
	// TeX is whether the submission holds TeX source rather than being a PDF the
	// authors produced themselves. arXiv accepts both.
	TeX Answer
	// Rendering is whether arXiv serves an HTML rendering of this version. Of this
	// version and not of this paper: arXiv renders one version and does not
	// backfill the rest, and the version the licence gate chose is the only one
	// this corpus may read.
	Rendering Answer
	// TextLayer is whether the PDF holds text on enough of its pages to be read
	// rather than looked at.
	TextLayer Answer
}

// Missing is the error a decision that cannot be made yet fails with.
//
// A path nobody can work out is not a path of vision. It is a fact nobody has
// gathered, and the answer is to gather it, so this error says which fact and what
// to run rather than quietly routing the paper to the fallback.
type Missing struct {
	// Want is the fact that is not known, as a noun phrase.
	Want string
	// How is the command that would establish it.
	How string
}

func (m *Missing) Error() string {
	return fmt.Sprintf("selection: the path cannot be decided until %s, so run %s", m.Want, m.How)
}

// Decide is the decision tree, and it is the whole of the tree in one place.
//
// The shape of it is in the spec and it is worth stating in one sentence: a paper
// with TeX goes down the rendering path when arXiv rendered this version and down
// the source path when it did not, and a paper with no TeX goes down the native
// path when its PDF holds text and the vision path when it does not.
//
// The render answer is provisional in one respect that cannot be fixed here. About
// a quarter of arXiv's conversions carry a LaTeXML error, and a rendering with an
// error inside a section body falls through to the source path, which is only
// visible once the rendering is on disk and parsed. So this returns render and the
// extractor is the thing that demotes it. Recording the demotion rather than
// pretending it was decided here is what keeps reports/paths.md honest.
func Decide(f Facts) (Path, string, error) {
	switch f.TeX {
	case Nobody:
		return "", "", &Missing{
			Want: "somebody has looked at what the submission holds",
			How:  "ax fetch source",
		}
	case No:
		switch f.TextLayer {
		case Yes:
			return PathNative, "the submission is a PDF the authors made themselves and it holds a text layer, so the text is already there to be read", nil
		case No:
			return PathVision, "the submission is a PDF with no text layer worth reading, so the pages have to be read as pictures", nil
		default:
			return "", "", &Missing{
				Want: "somebody has looked at whether the PDF holds a text layer",
				How:  "ax fetch native",
			}
		}
	default:
		switch f.Rendering {
		case Yes:
			return PathRender, "the submission holds TeX and arXiv serves a rendering of this version, which costs one request and no model", nil
		case No:
			return PathSource, "the submission holds TeX and arXiv serves no rendering of this version, so the TeX is compiled here", nil
		default:
			return "", "", &Missing{
				Want: "somebody has asked arXiv whether it renders this version",
				How:  "ax path decide -probe",
			}
		}
	}
}

// Says is what a path is, for a report.
func (p Path) Says() string {
	switch p {
	case PathRender:
		return "arXiv's own LaTeXML rendering, one request and no model"
	case PathSource:
		return "the submitted TeX, compiled with LaTeXML here"
	case PathNative:
		return "the PDF's own text layer, read with pdftotext"
	case PathVision:
		return "pictures of the pages, read by a model, which is the path that costs money"
	}
	return ""
}
