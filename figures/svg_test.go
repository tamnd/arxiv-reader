package figures

import (
	"strings"
	"testing"
)

// scale is Scalable with the error out of the way, for the cases that are about
// what came out.
func scale(t *testing.T, in string) string {
	t.Helper()
	out, err := Scalable([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// dvisvgm writes exactly this shape: a size in points, a viewBox in user units
// and the placement of the whole drawing as a transform on the first group. All
// three of the things this has to deal with are in one file.
const drawn = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="288pt" height="216pt" viewBox="0 0 288 216">
<g transform="translate(10,20) scale(2)">
<path d="M0 0 L10 10"/>
</g>
</svg>
`

// The whole of the reason this exists: width and height pin a picture to one
// size, and a Kobo reading a transform on a group gets the scaling wrong.
func TestAPictureScalesByItsViewBoxAndNotByAnythingElse(t *testing.T) {
	got := scale(t, drawn)
	if strings.Contains(got, "width=") || strings.Contains(got, "height=") {
		t.Errorf("the root still states a size:\n%s", got)
	}
	if strings.Contains(got, "transform=") {
		t.Errorf("the group still carries its transform:\n%s", got)
	}
	if !strings.Contains(got, "<g>") {
		t.Errorf("the group came out as something other than a bare group:\n%s", got)
	}
}

// The picture has to be the same picture afterwards. The group drew at 2p+t and
// the old viewBox framed that, so the frame comes back through the same map:
// 0 0 288 216 becomes -5 -10 144 108.
func TestTheTransformIsFoldedIntoTheViewBoxAndNotDropped(t *testing.T) {
	got := scale(t, drawn)
	if !strings.Contains(got, `viewBox="-5 -10 144 108"`) {
		t.Errorf("the viewBox came out of:\n%s", got)
	}
}

// A picture with a size and no coordinate system needs one, and the conversion
// has to be exact: a hundred points is a hundred and thirty three and a third
// user units, and a hundred and thirty three is a drawing with the wrong shape.
func TestAPictureWithNoViewBoxGetsOneFromItsSize(t *testing.T) {
	got := scale(t, `<svg xmlns="http://www.w3.org/2000/svg" width="100pt" height="50pt"><path d="M0 0"/></svg>`)
	if !strings.Contains(got, `viewBox="0 0 133.333333 66.666667"`) {
		t.Errorf("the viewBox came out of:\n%s", got)
	}
}

// Every other unit an author writes goes the same way, and a bare number is
// already in the coordinate system a viewBox is written in.
func TestEveryUnitReachesTheSameCoordinateSystem(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`width="1in" height="2in"`, `viewBox="0 0 96 192"`},
		{`width="2.54cm" height="5.08cm"`, `viewBox="0 0 96 192"`},
		{`width="25.4mm" height="50.8mm"`, `viewBox="0 0 96 192"`},
		{`width="6pc" height="12pc"`, `viewBox="0 0 96 192"`},
		{`width="96" height="192"`, `viewBox="0 0 96 192"`},
		{`width="96px" height="192px"`, `viewBox="0 0 96 192"`},
	} {
		got := scale(t, `<svg `+tc.in+`><path d="M0 0"/></svg>`)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s came out as:\n%s", tc.in, got)
		}
	}
}

// A rotation or a skew changes the shape of the frame rather than its size, and
// folding one would mean writing a viewBox that is not a rectangle. The picture
// is better left alone and reported than quietly bent.
func TestATransformThatIsNotATranslateOrAScaleIsRefused(t *testing.T) {
	for _, list := range []string{"rotate(30)", "matrix(1 0 0 1 0 0)", "skewX(10)", "scale(2,3)"} {
		in := `<svg width="10" height="10" viewBox="0 0 10 10"><g transform="` + list + `"><path d="M0 0"/></g></svg>`
		_, err := Scalable([]byte(in))
		if err == nil {
			t.Errorf("%s was folded into a viewBox", list)
			continue
		}
		if !strings.Contains(err.Error(), list) && !strings.Contains(err.Error(), "scales by") {
			t.Errorf("%s came back with %v, which does not say what it was", list, err)
		}
	}
}

// Dropping the width and height from a document with no coordinate system leaves
// a drawing with no size at all.
func TestAPictureWithNoSizeAndNoViewBoxIsRefused(t *testing.T) {
	_, err := Scalable([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`))
	if err == nil || !strings.Contains(err.Error(), "nothing to scale it by") {
		t.Fatalf("a picture with no size came back with %v", err)
	}
	if _, err := Scalable([]byte("<html><body>not a picture</body></html>")); err == nil {
		t.Error("something with no root element was taken for a picture")
	}
}

// A transform that was already doing nothing is left where it is, because
// rewriting an attribute that had no effect is a diff that says something
// happened when nothing did.
func TestATransformThatDoesNothingIsLeftAlone(t *testing.T) {
	in := `<svg width="10" height="10" viewBox="0 0 10 10"><g transform="translate(0,0) scale(1)"><path d="M0 0"/></g></svg>`
	got := scale(t, in)
	if !strings.Contains(got, `transform="translate(0,0) scale(1)"`) {
		t.Errorf("the transform was rewritten:\n%s", got)
	}
	if !strings.Contains(got, `viewBox="0 0 10 10"`) {
		t.Errorf("the viewBox moved:\n%s", got)
	}
}

// Only the first group, because that is where dvisvgm puts the placement of the
// whole drawing. A transform further in is part of how the picture is drawn and
// folding it would move one piece of the drawing and not the rest.
func TestOnlyTheTopGroupIsFolded(t *testing.T) {
	in := `<svg width="10" height="10" viewBox="0 0 10 10">
<g><g transform="scale(2)"><path d="M0 0"/></g></g>
</svg>`
	got := scale(t, in)
	if !strings.Contains(got, `transform="scale(2)"`) {
		t.Errorf("a transform inside the drawing was folded:\n%s", got)
	}
	if !strings.Contains(got, `viewBox="0 0 10 10"`) {
		t.Errorf("the viewBox moved for a transform that is not the drawing's placement:\n%s", got)
	}
}

// An XML declaration, a doctype and a comment before the root all belong to the
// author, and a picture that came back without its declaration is one an EPUB
// reader is entitled to complain about.
func TestWhateverComesBeforeTheRootIsKept(t *testing.T) {
	in := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- generated by dvisvgm -->\n" +
		`<svg width="10" height="10" viewBox="0 0 10 10"><g transform="scale(2)"><path d="M0 0"/></g></svg>`
	got := scale(t, in)
	if !strings.HasPrefix(got, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- generated by dvisvgm -->\n<svg") {
		t.Errorf("what came before the root did not come back:\n%s", got)
	}
}

// The drawing itself is not touched, only the root and the one attribute.
func TestTheDrawingIsNotRewritten(t *testing.T) {
	got := scale(t, drawn)
	if !strings.Contains(got, `<path d="M0 0 L10 10"/>`) {
		t.Errorf("the path changed:\n%s", got)
	}
	if !strings.HasSuffix(got, "</g>\n</svg>\n") {
		t.Errorf("the end of the document changed:\n%s", got)
	}
}

// Running twice is what happens when a figure is extracted again, and a picture
// that shifts every time it is written is a picture the corpus cannot compare
// across versions.
func TestScalingTwiceIsScalingOnce(t *testing.T) {
	for _, in := range []string{drawn, `<svg width="100pt" height="50pt"><path d="M0 0"/></svg>`} {
		once := scale(t, in)
		if twice := scale(t, once); twice != once {
			t.Errorf("a second pass changed the picture:\n%s\nand then:\n%s", once, twice)
		}
	}
}

// The root has to read as an element somebody can check against the picture,
// which means no run of spaces where two attributes used to be and no space
// before the bracket.
func TestTheRootIsLeftTidy(t *testing.T) {
	got := scale(t, drawn)
	root := got[strings.Index(got, "<svg") : strings.Index(got, "<svg")+strings.Index(got[strings.Index(got, "<svg"):], ">")+1]
	if strings.Contains(root, "  ") || strings.Contains(root, " >") {
		t.Errorf("the root came out %q", root)
	}
}

// A viewBox has four numbers in it, and a document that says otherwise is one to
// report rather than one to guess at.
func TestAViewBoxThatIsNotFourNumbersIsRefused(t *testing.T) {
	_, err := Scalable([]byte(`<svg width="10" height="10" viewBox="0 0 10"><path d="M0 0"/></svg>`))
	if err == nil || !strings.Contains(err.Error(), "a viewBox is four numbers") {
		t.Fatalf("a short viewBox came back with %v", err)
	}
}
