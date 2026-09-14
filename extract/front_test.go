package extract

import (
	"strings"
	"testing"
)

func sample() Document {
	return Document{
		Front: Front{
			Paper: "2501.00001", Version: "v3", Title: "A Paper About Nothing",
			Authors: []string{"Nobody"}, Categories: []string{"cs.LG"},
			Access: "open", Licence: "CC-BY-4.0", LicenceOfSource: "cc-by", LicenceFrom: "abs",
			Section: 1, SectionTitle: "Introduction", Kind: "section", Lang: "en", LocalID: "s1",
			Path: "render", SourceURL: "https://arxiv.org/html/2501.00001v3",
			Figures: []string{}, Tables: []string{}, Statements: []string{},
		},
		Body: "Nothing at all.\n",
	}
}

func TestADocumentRoundTrips(t *testing.T) {
	d := sample()
	b, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Body != d.Body {
		t.Fatalf("the body came back as %q", back.Body)
	}
	if back.Front.Paper != "2501.00001" || back.Front.LocalID != "s1" || back.Front.Licence != "CC-BY-4.0" {
		t.Fatalf("the front matter came back as %+v", back.Front)
	}
}

// The hash is written by Bytes and nowhere else, so the field and the bytes
// beside it cannot disagree by construction.
func TestTheContentHashIsStampedOnTheWayOut(t *testing.T) {
	d := sample()
	b, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Front.ContentSHA256 != Hash(d.Body) {
		t.Fatalf("the file says %q and the body hashes to %q", back.Front.ContentSHA256, Hash(d.Body))
	}
	if back.Corrected() {
		t.Fatal("a file straight out of Bytes says it has been corrected")
	}
}

// This is the whole mechanism: somebody fixes a sentence in an editor, and the
// pipeline knows without their having had to tell it.
func TestAnEditedBodyIsNoticed(t *testing.T) {
	b, err := sample().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.Replace(string(b), "Nothing at all.", "Nothing at all, really.", 1)
	d, err := ParseDocument([]byte(fixed))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Corrected() {
		t.Fatal("a hand corrected body was not noticed")
	}
}

// A file with no hash at all is not a corrected file. Nobody has looked, which
// is a different thing from somebody having changed it, and conflating the two
// is how a corpus ends up refusing to write a file it has never written.
func TestAFileWithNoHashIsNotCorrected(t *testing.T) {
	d := Document{Body: "anything"}
	if d.Corrected() {
		t.Fatal("a document with no content_sha256 says it has been corrected")
	}
}

func TestParseDocumentRefusesWhatIsNotAContentFile(t *testing.T) {
	cases := map[string]string{
		"no front matter":     "Nothing at all.\n",
		"front matter unshut": "---\npaper: \"2501.00001\"\nNothing at all.\n",
		"empty":               "",
	}
	for name, in := range cases {
		if _, err := ParseDocument([]byte(in)); err == nil {
			t.Errorf("%s was parsed as a content file", name)
		}
	}
}

// Every field is written even when it is empty, because a schema that changes
// shape depending on what was found is a schema every reader has to guess at.
func TestEveryFieldIsWritten(t *testing.T) {
	b, err := Document{Body: "x\n"}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"paper:", "version:", "title:", "authors:", "submitted:", "announced:",
		"primary_category:", "categories:", "msc_class:", "acm_class:", "doi:",
		"journal_ref:", "access:", "licence:", "licence_of_source:", "licence_from:",
		"section:", "section_title:", "kind:", "lang:", "tag:", "local_id:", "path:",
		"source_url:", "source_sha256:", "source_pages:", "extraction_model:",
		"prompt_sha256:", "objects:", "equations:", "figures:", "tables:",
		"statements:", "code_blocks:", "content_sha256:", "edited:",
	} {
		if !strings.Contains(string(b), field) {
			t.Errorf("%s is missing from an empty front matter", field)
		}
	}
}
