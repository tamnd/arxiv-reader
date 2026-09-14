package extract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Front is the YAML block every content file opens with.
//
// Four groups of fields and they answer four different questions. What the
// paper is, what may be done with it, where this file came from, and what is in
// it. A translation adds a fifth group saying what it was made from, and that
// arrives with the translator.
//
// Every field is written even when it is empty. A schema that changes shape
// depending on what was found is a schema every reader has to guess at, and an
// absent field and an empty one are different things only when somebody has
// decided they are.
type Front struct {
	Paper           string   `yaml:"paper"`
	Version         string   `yaml:"version"`
	Title           string   `yaml:"title"`
	Authors         []string `yaml:"authors"`
	Submitted       string   `yaml:"submitted"`
	Announced       string   `yaml:"announced"`
	PrimaryCategory string   `yaml:"primary_category"`
	Categories      []string `yaml:"categories"`
	MSCClass        string   `yaml:"msc_class"`
	ACMClass        string   `yaml:"acm_class"`
	DOI             string   `yaml:"doi"`
	JournalRef      string   `yaml:"journal_ref"`

	// Access is one of the four classes, Licence is what our file is under as
	// an SPDX identifier, and LicenceOfSource is the arXiv licence the article
	// itself carries.
	Access          string `yaml:"access"`
	Licence         string `yaml:"licence"`
	LicenceOfSource string `yaml:"licence_of_source"`
	// LicenceFrom is the surface that licence was read from, and it is here
	// because audit rule S10 needs it. A licence read off a bulk surface states
	// one licence for a whole paper and this file is one version of one paper,
	// so a content file whose licence came from anywhere but the abs page is a
	// file nobody has actually established the permission for.
	LicenceFrom string `yaml:"licence_from"`

	Section      int    `yaml:"section"`
	SectionTitle string `yaml:"section_title"`
	Kind         string `yaml:"kind"`
	Lang         string `yaml:"lang"`
	Tag          string `yaml:"tag"`
	// LocalID is the identifier of the object this whole file is, so s3 for the
	// file holding section 3.
	//
	// Every other object in the corpus carries its identifier in an attribute
	// block on the line it starts, and a section that is a whole file has no
	// such line: its heading is section_title up here. Without this field a
	// link to #s3 is a link to a section nothing in the corpus claims to be,
	// and building the index that resolves it would mean re-deriving s3 from
	// the printed number, which is the kind of rule that works until the first
	// appendix.
	LocalID string `yaml:"local_id"`

	Path string `yaml:"path"`
	// SourceURL is where the bytes this was read from came from.
	//
	// The spec called this field surface and named one of arxiv-cli's twelve
	// surfaces by number. The URL is better: it says which surface answered
	// without anybody having to hold the numbering in their head, and it is
	// already recorded in the sources manifest so the two cannot drift.
	SourceURL       string `yaml:"source_url"`
	SourceSHA256    string `yaml:"source_sha256"`
	SourcePages     string `yaml:"source_pages"`
	ExtractionModel string `yaml:"extraction_model"`
	PromptSHA256    string `yaml:"prompt_sha256"`

	Objects    int      `yaml:"objects"`
	Equations  int      `yaml:"equations"`
	Figures    []string `yaml:"figures"`
	Tables     []string `yaml:"tables"`
	Statements []string `yaml:"statements"`
	CodeBlocks int      `yaml:"code_blocks"`

	// ContentSHA256 is the hash of the body below, and it is the field that
	// makes a hand correction safe.
	//
	// ax split notices the body no longer matches it, ax extract refuses to
	// overwrite a file where it does not match, and ax split -accept restamps
	// it and sets Edited. So a correction somebody made without having read any
	// of this is protected by default, which is the only kind of protection
	// worth having.
	ContentSHA256 string `yaml:"content_sha256"`
	Edited        bool   `yaml:"edited"`
}

// Document is one content file: its front matter and its body.
type Document struct {
	Front Front
	Body  string
}

const fence = "---\n"

// Bytes is the file as it goes on disk, with the body hashed into the front
// matter.
//
// The hash is computed here and nowhere else, so the field and the bytes cannot
// disagree by construction.
func (d Document) Bytes() ([]byte, error) {
	d.Front.ContentSHA256 = Hash(d.Body)
	var buf bytes.Buffer
	buf.WriteString(fence)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d.Front); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString(fence)
	buf.WriteString("\n")
	buf.WriteString(d.Body)
	return buf.Bytes(), nil
}

// ParseDocument reads a content file back.
func ParseDocument(b []byte) (Document, error) {
	s := string(b)
	if !strings.HasPrefix(s, fence) {
		return Document{}, errors.New("extract: the file does not open with front matter")
	}
	rest := s[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return Document{}, errors.New("extract: the front matter is never closed")
	}
	var d Document
	if err := yaml.Unmarshal([]byte(rest[:end+1]), &d.Front); err != nil {
		return Document{}, fmt.Errorf("extract: the front matter is not YAML: %w", err)
	}
	d.Body = strings.TrimPrefix(rest[end+1+len(fence):], "\n")
	return d, nil
}

// Hash is the content hash of a body.
func Hash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Corrected reports whether the body has been changed since it was written.
//
// A file whose recorded hash does not match its body is a file somebody edited
// by hand, which is a thing this corpus expects and wants rather than a thing
// it guards against. What it guards against is losing that edit the next time
// the extractor runs.
func (d Document) Corrected() bool {
	return d.Front.ContentSHA256 != "" && d.Front.ContentSHA256 != Hash(d.Body)
}
