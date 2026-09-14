// Package source reads a submission as its author uploaded it.
//
// This is the other half of what arXiv serves per version. The rendering is one
// HTML file that arXiv made, and it only exists for papers announced from
// December 2023 onward. The e-print is what the submitter sent, it exists for
// every paper back to 1991, and it is the only way to read the older half of
// arXiv at all.
//
// What arrives is one gzip stream, and what is inside it is usually a tar of
// the submission, sometimes a single TeX file with no tar around it, and
// sometimes a PDF the author made themselves. All three are ordinary and none
// of them is announced, so the format is worked out from the bytes.
//
// The work here stops at the main file. Deciding which of forty files the
// document starts from is a question about the submission, and running LaTeXML
// over it is a question about TeX, so they are different packages and this is
// the one that does not need latexmlc installed.
package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// maxUnpacked caps what one submission comes to once it is decompressed.
//
// arXiv caps a submission at fifty megabytes and that is the compressed size,
// so this is generous by design. It is here because a gzip stream says nothing
// about how much it expands to, and a reader with no cap on it is a reader that
// a crafted stream can turn into an out of memory crash.
const maxUnpacked = 512 << 20

// PDFOnly is the error a submission with no TeX in it fails with.
//
// A real and permitted kind of submission: arXiv accepts a PDF the author
// produced themselves, and both of the gravitational wave papers fetched while
// this was being written are one. There is no source to read, so this is not a
// defect in the download, and the paper belongs on the native path where a PDF
// is what is expected.
type PDFOnly struct {
	Bytes int
}

func (p *PDFOnly) Error() string {
	return fmt.Sprintf("source: this submission is a PDF of %d bytes and not TeX, which arXiv accepts and which leaves no source to read, so the paper is on the native path", p.Bytes)
}

// File is one file of a submission, with the name the submitter gave it.
type File struct {
	Name string
	Data []byte
}

// Bundle is one submission held in memory.
//
// In memory rather than unpacked to a directory, because most of what is asked
// of a submission is answered by reading two or three files of it and the main
// file question is answered by reading all of them. Unpacking is a separate
// step, for the one caller that has to hand the files to another program.
type Bundle struct {
	Files []File
	// Tarred is whether the stream held a tar. A submission that did not is a
	// single TeX file, which is worth knowing because nothing in it can be
	// included from anywhere and the main file question is already answered.
	Tarred bool
}

// Open reads the bytes of one e-print.
//
// The order the formats are tried in is the order they can be told apart in. A
// PDF says so in its first four bytes, both before and after decompression,
// because arXiv serves some of them gzipped and some of them not. A tar says so
// in its first header. Everything else is the single file case, and it has to
// look like TeX before it is treated as TeX.
func Open(b []byte) (*Bundle, error) {
	if isPDF(b) {
		return nil, &PDFOnly{Bytes: len(b)}
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("source: these %d bytes are not a gzip stream, and an e-print is always one: %w", len(b), err)
	}
	defer zr.Close()
	plain, err := io.ReadAll(io.LimitReader(zr, maxUnpacked+1))
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if len(plain) > maxUnpacked {
		return nil, fmt.Errorf("source: this submission expands to more than %d bytes, which is larger than anything arXiv accepts", maxUnpacked)
	}
	if isPDF(plain) {
		return nil, &PDFOnly{Bytes: len(plain)}
	}
	if files, ok, err := untar(plain); ok {
		if err != nil {
			return nil, err
		}
		return &Bundle{Files: files, Tarred: true}, nil
	}
	if !looksLikeTeX(plain) {
		return nil, fmt.Errorf("source: this submission is neither a tar nor a PDF nor a TeX file, and there is nothing else it is allowed to be")
	}
	// The name is this package's and not the submitter's, because a single file
	// submission arrives with no name at all: the tar is what carries names and
	// there is no tar. Every caller wants a name, so one is made up, and it is
	// the same one every time so that nothing downstream depends on which paper
	// it came from.
	return &Bundle{Files: []File{{Name: "paper.tex", Data: plain}}}, nil
}

func isPDF(b []byte) bool { return bytes.HasPrefix(b, []byte("%PDF-")) }

func looksLikeTeX(b []byte) bool {
	head := b
	if len(head) > 1<<20 {
		head = head[:1<<20]
	}
	for _, marker := range []string{`\documentclass`, `\documentstyle`, `\begin{document}`, `\input`} {
		if bytes.Contains(head, []byte(marker)) {
			return true
		}
	}
	return false
}

// untar reads a tar if that is what these bytes are.
//
// The second return says whether it was a tar at all, which is separate from
// whether reading it worked. A stream that is not a tar is the ordinary single
// file submission and the caller carries on, and a tar that will not read is a
// download to look at.
func untar(b []byte) ([]File, bool, error) {
	tr := tar.NewReader(bytes.NewReader(b))
	var files []File
	for first := true; ; first = false {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if first {
				return nil, false, nil
			}
			return nil, true, fmt.Errorf("source: this tar stops partway through: %w", err)
		}
		// Directories, symlinks and the rest are stepped over. A submission is
		// its files, the directories are made again when anything is unpacked,
		// and a symlink in an archive is a way into the rest of the disk.
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name, err := safeName(h.Name)
		if err != nil {
			return nil, true, err
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxUnpacked+1))
		if err != nil {
			return nil, true, fmt.Errorf("source: reading %s out of this tar: %w", name, err)
		}
		files = append(files, File{Name: name, Data: data})
	}
	return files, true, nil
}

// safeName is the tar entry's name as a path inside the submission.
//
// A name that climbs out of the directory or starts at the root is refused
// rather than trimmed into shape. Unpacking one of those writes wherever it
// says, which is the oldest bug in archive handling, and a submission that
// carries one is not a submission anybody should be quietly repairing.
func safeName(name string) (string, error) {
	// A backslash is a legal character in a file name on this machine and a
	// separator on another one, so a name carrying one means two different paths
	// depending on where the corpus is unpacked. That is enough to refuse it, and
	// refusing it here is what makes the rest of this function mean the same
	// thing everywhere.
	if strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("source: this tar holds %q, and a backslash in a name is a separator on one machine and a character on another", name)
	}
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("source: this tar holds an entry with no name")
	}
	// The standard library's own answer to the question, rather than a path check
	// written by hand. It refuses the name that climbs out, the name that starts
	// at the root and the drive letter, which is the whole of what unpacking an
	// archive has to be careful about. Names are kept with forward slashes
	// because that is what a tar carries and what every caller here looks a file
	// up by, and the backslash refusal above is what makes the two agree.
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("source: this tar holds %q, which points outside the submission, and nothing here unpacks that", name)
	}
	return clean, nil
}

// Find is one file of the submission by name.
func (b *Bundle) Find(name string) ([]byte, bool) {
	for _, f := range b.Files {
		if f.Name == name {
			return f.Data, true
		}
	}
	return nil, false
}

// Names is every file in the submission, sorted.
func (b *Bundle) Names() []string {
	out := make([]string, 0, len(b.Files))
	for _, f := range b.Files {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// Write unpacks the submission into a directory.
//
// For the one caller that has to hand the files to another program, which is
// LaTeXML. Names are checked again here rather than trusted from Open, because
// Files is an exported field and a caller that built a bundle itself should not
// be able to write outside the directory it named.
func (b *Bundle) Write(dir string) error {
	for _, f := range b.Files {
		name, err := safeName(f.Name)
		if err != nil {
			return err
		}
		out := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, f.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Main is the file the document starts from.
//
// Four passes, in order of how much they are worth believing. The submitter
// saying so outright in a 00README is the first, because it is the one answer
// that is not a guess. Then the files that hold a document, which is most
// submissions answered. Then the include graph, which removes the chapter files
// of a paper whose sections each carry a preamble. Then the names people give a
// main file, which is a convention and is treated as one.
//
// A submission that gets through all four with more than one candidate left is
// usually two papers in one upload, and the answer to that is a person naming
// the file rather than this package picking.
func (b *Bundle) Main() (string, error) {
	if len(b.Files) == 0 {
		return "", errors.New("source: this submission holds no files at all")
	}
	if !b.Tarred {
		return b.Files[0].Name, nil
	}
	if named, ok := b.declared(); ok {
		if _, have := b.Find(named); !have {
			return "", fmt.Errorf("source: the 00README names %s as the top level file and the submission does not hold it", named)
		}
		return named, nil
	}

	candidates := b.documents()
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("source: no file in this submission begins a document, and the %d files in it are %s", len(b.Files), strings.Join(b.Names(), ", "))
	case 1:
		return candidates[0], nil
	}
	if left := b.notIncluded(candidates); len(left) == 1 {
		return left[0], nil
	} else if len(left) > 1 {
		candidates = left
	}
	if named := preferred(candidates); named != "" {
		return named, nil
	}
	return "", fmt.Errorf("source: %d files in this submission begin a document and nothing tells them apart, which are %s, so name the one to read", len(candidates), strings.Join(candidates, ", "))
}

// declared reads the file where the submitter says which file is the top level
// one.
//
// Two formats, because arXiv has had two. The JSON one is the current form and
// the plain text one has been accepted since the 1990s, and a submission from
// either era can turn up in any month of the harvest. The list is looked for at
// the top level and under process, so that a guess about the nesting cannot end
// with the one file that answers the question outright being ignored.
func (b *Bundle) declared() (string, bool) {
	if raw, ok := b.Find("00README.json"); ok {
		var doc struct {
			Sources []readmeEntry `json:"sources"`
			Process struct {
				Sources []readmeEntry `json:"sources"`
			} `json:"process"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			for _, list := range [][]readmeEntry{doc.Sources, doc.Process.Sources} {
				for _, e := range list {
					if strings.EqualFold(e.Usage, "toplevelfile") && e.Filename != "" {
						return e.Filename, true
					}
				}
			}
		}
	}
	// The plain text form is a line per file, the name first and what to do
	// with it second, and every other line in the file is a note to a human.
	for _, name := range []string{"00README.XXX", "00README.xxx", "00readme.xxx"} {
		raw, ok := b.Find(name)
		if !ok {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[1], "toplevelfile") {
				return fields[0], true
			}
		}
	}
	return "", false
}

type readmeEntry struct {
	Filename string `json:"filename"`
	Usage    string `json:"usage"`
}

// begins is the pair of lines that make a file a document rather than a part of
// one. Both are needed: a preamble with no body is a style file somebody wrote
// inline, and a body with no preamble is a chapter.
var (
	begins   = regexp.MustCompile(`(?m)^[^%\n]*\\document(class|style)`)
	bodies   = regexp.MustCompile(`(?m)^[^%\n]*\\begin\{document\}`)
	includes = regexp.MustCompile(`(?m)^[^%\n]*\\(?:input|include|subfile)\s*\{?\s*([^}\s]+)`)
)

// documents is every file that begins a document, sorted.
func (b *Bundle) documents() []string {
	var out []string
	for _, f := range b.Files {
		if !isTeXName(f.Name) {
			continue
		}
		if begins.Match(f.Data) && bodies.Match(f.Data) {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

// notIncluded drops the candidates that another file of the submission reads.
//
// A paper whose chapters each carry their own preamble is the case: every
// chapter compiles on its own, so every chapter looks like a document, and the
// one nothing else includes is the paper.
func (b *Bundle) notIncluded(candidates []string) []string {
	read := map[string]bool{}
	for _, f := range b.Files {
		if !isTeXName(f.Name) {
			continue
		}
		for _, m := range includes.FindAllSubmatch(f.Data, -1) {
			name := path.Clean(path.Join(path.Dir(f.Name), string(m[1])))
			read[name] = true
			read[name+".tex"] = true
		}
	}
	var out []string
	for _, c := range candidates {
		if !read[c] {
			out = append(out, c)
		}
	}
	return out
}

// preferred is the candidate whose name is one people give a main file.
//
// A convention and nothing more, which is why it is the last pass rather than
// the first. ms.tex is what arXiv's own submission help has used for years,
// main.tex is what every template ships, and the rest are what turns up.
func preferred(candidates []string) string {
	for _, want := range []string{"ms.tex", "main.tex", "paper.tex", "arxiv.tex", "manuscript.tex", "article.tex"} {
		for _, c := range candidates {
			if strings.EqualFold(path.Base(c), want) {
				return c
			}
		}
	}
	return ""
}

func isTeXName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".tex", ".ltx", "":
		return true
	}
	return false
}

// Read opens the e-print cached at one path.
func Read(file string) (*Bundle, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("source: there is no e-print at %s, so fetch it first with ax fetch source", file)
		}
		return nil, err
	}
	return Open(b)
}
