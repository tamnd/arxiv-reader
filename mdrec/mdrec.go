// Package mdrec reads and writes a file of records as Markdown with a YAML
// header, the form of the planes of the corpus that were JSON Lines.
//
// The header holds the records in order, each with its fields in the order of
// its JSON form. The text of one field of each record, as the body_md of an
// object, goes to the body under a marker with the key of the record, so that
// the file reads as Markdown:
//
//	---
//	records:
//	    - local: thm-1
//	      kind: statement
//	---
//	<!-- thm-1: body_md -->
//	Each tree is a graph.
//	<!-- end thm-1 -->
//
// The codec works on the JSON form of each record, so a reader decodes the
// records with the types and the json tags it has, and a file read back gives
// the same JSON.
package mdrec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Ext is the extension of a file of records.
const Ext = ".md"

// keys are the fields that name a record in a marker, the first that the
// record has.
var keys = []string{"local", "id", "key"}

var (
	openMark = regexp.MustCompile(`^<!-- ([^\s<>]+): ([A-Za-z0-9_]+) -->$`)
	safeKey  = regexp.MustCompile(`^[^\s<>]+$`)
)

type header struct {
	Records []yaml.Node `yaml:"records"`
}

// Encode gives the file of the records, each a JSON object. The field body,
// when it is not empty and a record has it as a string, goes to the body.
func Encode(records [][]byte, body string) ([]byte, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	type text struct{ key, value string }
	var texts []text
	seen := map[string]bool{}
	for i, rec := range records {
		n, err := toNode(json.NewDecoder(bytes.NewReader(rec)))
		if err != nil {
			return nil, fmt.Errorf("mdrec: record %d: %w", i+1, err)
		}
		if n.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("mdrec: record %d is not an object", i+1)
		}
		if body != "" {
			if v, at := field(n, body); at >= 0 && v.Tag == "!!str" && v.Value != "" {
				k := name(n, i)
				end := "\n<!-- end " + k + " -->"
				if !seen[k] && !strings.Contains("\n"+v.Value+"\n", end+"\n") {
					seen[k] = true
					texts = append(texts, text{k, v.Value})
					n.Content = append(n.Content[:at], n.Content[at+2:]...)
				}
			}
		}
		seq.Content = append(seq.Content, n)
	}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "records"}, seq,
	}}
	if len(seq.Content) == 0 {
		seq.Style = yaml.FlowStyle
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	b.WriteString("---\n")
	for _, t := range texts {
		fmt.Fprintf(&b, "<!-- %s: %s -->\n%s\n<!-- end %s -->\n", t.key, body, t.value, t.key)
	}
	return b.Bytes(), nil
}

// Decode gives the records of a file, each as a JSON object.
func Decode(data []byte) ([][]byte, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return nil, errors.New("mdrec: the file does not open with a YAML header")
	}
	head, rest, ok := strings.Cut(s[4:], "\n---\n")
	if !ok {
		if strings.HasSuffix(s[4:], "\n---") {
			head, rest = strings.TrimSuffix(s[4:], "\n---"), ""
		} else {
			return nil, errors.New("mdrec: the YAML header has no end")
		}
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(head), &root); err != nil {
		return nil, fmt.Errorf("mdrec: the header: %w", err)
	}
	var h header
	if err := root.Decode(&h); err != nil {
		return nil, fmt.Errorf("mdrec: the header: %w", err)
	}
	byKey := map[string]*yaml.Node{}
	for i := range h.Records {
		if h.Records[i].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("mdrec: record %d is not a mapping", i+1)
		}
		if k := name(&h.Records[i], i); byKey[k] == nil {
			byKey[k] = &h.Records[i]
		}
	}
	for rest != "" {
		line, after, _ := strings.Cut(rest, "\n")
		if strings.TrimSpace(line) == "" {
			rest = after
			continue
		}
		m := openMark.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("mdrec: the body has %q where a marker must be", line)
		}
		end := "<!-- end " + m[1] + " -->"
		var value string
		if strings.HasPrefix(after, end+"\n") || after == end {
			value, rest = "", strings.TrimPrefix(strings.TrimPrefix(after, end), "\n")
		} else {
			i := strings.Index(after, "\n"+end+"\n")
			if i < 0 {
				if !strings.HasSuffix(after, "\n"+end) {
					return nil, fmt.Errorf("mdrec: the text of %s has no end", m[1])
				}
				i = len(after) - len(end) - 1
			}
			value, rest = after[:i], after[min(len(after), i+len(end)+2):]
		}
		n := byKey[m[1]]
		if n == nil {
			return nil, fmt.Errorf("mdrec: the body has a text of %s, and no record has that key", m[1])
		}
		if _, at := field(n, m[2]); at >= 0 {
			return nil, fmt.Errorf("mdrec: %s has %s in the header and in the body", m[1], m[2])
		}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: m[2]},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	out := make([][]byte, 0, len(h.Records))
	for i := range h.Records {
		var b bytes.Buffer
		if err := toJSON(&b, &h.Records[i]); err != nil {
			return nil, fmt.Errorf("mdrec: record %d: %w", i+1, err)
		}
		out = append(out, b.Bytes())
	}
	return out, nil
}

// ReadFile reads the records of a file.
func ReadFile(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	recs, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return recs, nil
}

// WriteFile writes the records to a temporary file in the directory of path
// and renames it to path, so that a write that fails leaves the old file.
func WriteFile(path string, records [][]byte, body string) error {
	data, err := Encode(records, body)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return atomic(path, data)
}

func atomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Lines gives the records of JSON Lines, one for each line that is not blank.
func Lines(data []byte) [][]byte {
	var out [][]byte
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// Marshal gives the JSON form of each value, for Encode.
func Marshal[T any](vs []T) ([][]byte, error) {
	out := make([][]byte, 0, len(vs))
	for _, v := range vs {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// Unmarshal decodes the JSON form of each record into a value.
func Unmarshal[T any](recs [][]byte) ([]T, error) {
	out := make([]T, 0, len(recs))
	for i, r := range recs {
		var v T
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, fmt.Errorf("record %d: %w", i+1, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// field gives the value of a key of a mapping, and the index of the key.
func field(n *yaml.Node, key string) (*yaml.Node, int) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1], i
		}
	}
	return nil, -1
}

// name is the key of a record in a marker.
func name(n *yaml.Node, i int) string {
	for _, k := range keys {
		if v, at := field(n, k); at >= 0 && v.Kind == yaml.ScalarNode && v.Tag == "!!str" && safeKey.MatchString(v.Value) && !strings.HasPrefix(v.Value, "#") {
			return v.Value
		}
	}
	return "#" + strconv.Itoa(i+1)
}

// toNode reads one JSON value and gives it as a YAML node, with the keys in
// their order.
func toNode(dec *json.Decoder) (*yaml.Node, error) {
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := toNode(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.(string)}, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if len(n.Content) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, nil
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				v, err := toNode(dec)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if len(n.Content) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, nil
		}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(string(t), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(t)}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return nil, fmt.Errorf("the JSON token %v", tok)
}

// toJSON writes a YAML node as JSON, with the keys in their order.
func toJSON(w io.Writer, n *yaml.Node) error {
	switch n.Kind {
	case yaml.AliasNode:
		return toJSON(w, n.Alias)
	case yaml.MappingNode:
		io.WriteString(w, "{")
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				io.WriteString(w, ",")
			}
			if err := writeString(w, n.Content[i].Value); err != nil {
				return err
			}
			io.WriteString(w, ":")
			if err := toJSON(w, n.Content[i+1]); err != nil {
				return err
			}
		}
		io.WriteString(w, "}")
	case yaml.SequenceNode:
		io.WriteString(w, "[")
		for i, c := range n.Content {
			if i > 0 {
				io.WriteString(w, ",")
			}
			if err := toJSON(w, c); err != nil {
				return err
			}
		}
		io.WriteString(w, "]")
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			io.WriteString(w, "null")
		case "!!bool":
			var v bool
			if err := n.Decode(&v); err != nil {
				return err
			}
			io.WriteString(w, strconv.FormatBool(v))
		case "!!int", "!!float":
			if !json.Valid([]byte(n.Value)) {
				return fmt.Errorf("the number %q is not a JSON number", n.Value)
			}
			io.WriteString(w, n.Value)
		default:
			if err := writeString(w, n.Value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("a YAML node of kind %d", n.Kind)
	}
	return nil
}

// writeString writes a JSON string, with <, > and & as they are.
func writeString(w io.Writer, s string) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	_, err := w.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	return err
}

// bodies are the fields that Write sends to the body, the first that a record
// of the file has as a string: the text of an object, and the abstract of a
// paper.
var bodies = []string{"body_md", "abstract"}

// Old is the extension of the files of records before they were Markdown.
const Old = ".jsonl"

// old gives the JSON Lines file that path replaces, or "" when path is not a
// file of records.
func old(path string) string {
	if !strings.HasSuffix(path, Ext) {
		return ""
	}
	return strings.TrimSuffix(path, Ext) + Old
}

// Read reads the records of the file at path, or of the JSON Lines file of
// the same name when path is not there, which is a corpus that has not yet
// moved to Markdown. Neither file there is an error that is fs.ErrNotExist.
// A file that does not open with a header is read as JSON Lines, whatever its
// name.
func Read(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if o := old(path); errors.Is(err, os.ErrNotExist) && o != "" {
		if d, oerr := os.ReadFile(o); oerr == nil {
			data, err = d, nil
		} else if !errors.Is(oerr, os.ErrNotExist) {
			err = oerr
		}
	}
	if err != nil {
		return nil, err
	}
	if !IsMarkdown(data) {
		return Lines(data), nil
	}
	recs, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return recs, nil
}

// IsMarkdown reports whether data opens with the header of a file of
// records, and is not JSON Lines.
func IsMarkdown(data []byte) bool {
	return bytes.HasPrefix(data, []byte("---\n"))
}

// ReadLines reads the records of the file at path as Read does, and gives
// them as JSON Lines.
func ReadLines(path string) ([]byte, error) {
	recs, err := Read(path)
	if err != nil {
		return nil, err
	}
	return Join(recs), nil
}

// Join gives the records as JSON Lines.
func Join(recs [][]byte) []byte {
	var b bytes.Buffer
	for _, r := range recs {
		b.Write(r)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Write writes the records to path with WriteFile, with the text of the
// first field of bodies that a record has in the body, and removes the JSON
// Lines file that path replaces.
func Write(path string, records [][]byte) error {
	data, err := EncodeAuto(records)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return Put(path, data)
}

// EncodeAuto gives the file of the records as Encode does, with the text of
// the first field of bodies that a record has in the body.
func EncodeAuto(records [][]byte) ([]byte, error) {
	return Encode(records, bodyOf(records))
}

// Put writes the file data, as Encode gives it, to path as WriteFile does,
// and removes the JSON Lines file that path replaces.
func Put(path string, data []byte) error {
	if err := atomic(path, data); err != nil {
		return err
	}
	if o := old(path); o != "" {
		if err := os.Remove(o); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// WriteLines writes JSON Lines to path as Write does.
func WriteLines(path string, data []byte) error {
	return Write(path, Lines(data))
}

func bodyOf(records [][]byte) string {
	for _, f := range bodies {
		for _, r := range records {
			var m map[string]json.RawMessage
			if json.Unmarshal(r, &m) == nil && len(m[f]) > 0 && m[f][0] == '"' {
				return f
			}
		}
	}
	return ""
}

// Glob gives the files of records that match pattern, which ends in Ext, and
// for each JSON Lines file that matches it with the old extension and has no
// Markdown file, the name of the Markdown file, which Read reads.
func Glob(pattern string) ([]string, error) {
	md, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	o := old(pattern)
	if o == "" {
		return md, nil
	}
	js, err := filepath.Glob(o)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(md))
	for _, f := range md {
		seen[f] = true
	}
	for _, f := range js {
		if f = strings.TrimSuffix(f, Old) + Ext; !seen[f] {
			seen[f] = true
			md = append(md, f)
		}
	}
	sort.Strings(md)
	return md, nil
}
