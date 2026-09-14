package audit

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/tags"
)

// history is G08, the one rule that reads the repository rather than the corpus.
//
// A tag is assigned once and never changes, which is a promise about lines that
// are no longer in a file. The working tree cannot answer that, so this reads
// what git has: every register line any commit took out, and every register line
// the working tree has taken out since the last one. A tag that went and never
// came back is the finding.
//
// It is one git process over the whole run and not one per paper. A removal is
// repaired by a revert rather than by putting the line back, because anything
// that cited the tag in between needs the history to say so, and the message
// says as much.
func (c Content) history(col *collector, papers []axid.ID) error {
	if !col.wanted("G08") || len(papers) == 0 {
		return nil
	}
	prefix, ok := repository(c.Root)
	if !ok {
		// Not a repository, so there is no history to read and the rule has not
		// run. Saying it passed would be saying every tag ever handed out is
		// still there, which nothing here has checked.
		return nil
	}
	col.checked("G08")
	want := map[string]axid.ID{}
	for _, id := range papers {
		want[corpus.TagsPath("", id)] = id
	}
	// The committed history first and then the working tree, because a line
	// deleted and not committed yet is the removal somebody can still take back.
	out, err := git(c.Root, "log", "-p", "--unified=0", "--no-color", "--format=commit %H", "--", "tags")
	if err != nil {
		return err
	}
	tree, err := git(c.Root, "diff", "--unified=0", "--no-color", "HEAD", "--", "tags")
	if err != nil {
		return err
	}
	gone := map[string][]removal{}
	for _, r := range append(removed(out, prefix), removed(tree, prefix)...) {
		gone[r.path] = append(gone[r.path], r)
	}
	for file, rs := range gone {
		id, ok := want[file]
		if !ok {
			// A register of a paper this run is not auditing, or of one that is
			// not in the content plane at all.
			continue
		}
		reg, err := readRegister(corpus.TagsPath(c.Root, id))
		if err != nil {
			return err
		}
		held := map[tags.Tag]bool{}
		for _, e := range reg.entries {
			held[e.Tag] = true
		}
		for _, r := range rs {
			if held[r.tag] {
				// Taken out and put back, or moved onto another line, which is
				// what carrying a register onto a new version does to every tag
				// in it.
				continue
			}
			col.add(Finding{
				Rule: "G08", File: file, Shard: corpus.Shard(id), ID: id.Canonical,
				What: r.what(),
			})
		}
	}
	return nil
}

// untracked is F04, the other rule that asks git rather than the corpus.
//
// Every other figure rule reads a decision, a caption or a pixel count. This one
// reads whether the bytes are in the repository at all, which is a different
// question and the one that matters most for the only part of the corpus that is
// not text: a picture no commit records is a picture nobody else has, and a
// picture the third party rule withheld is supposed to be gone rather than
// sitting in a working tree.
//
// Ignored is reported separately and is the worse of the two, because a rule
// that reads a directory git has been told to skip is a rule that can never fail
// again.
func (c Content) untracked(col *collector, papers []axid.ID) error {
	if !col.wanted("F04") {
		return nil
	}
	if _, ok := repository(c.Root); !ok {
		// Not a repository, so nothing is tracked and nothing is untracked, and
		// there is no version of this rule that means anything over an unpacked
		// archive. Not run rather than a pass.
		return nil
	}
	col.checked("F04")
	// One git process for each of the two answers, over the whole run, because the
	// question is about a directory and not about a paper.
	plain, err := git(c.Root, "ls-files", "--others", "--exclude-standard", "--", "figures")
	if err != nil {
		return err
	}
	skipped, err := git(c.Root, "ls-files", "--others", "--ignored", "--exclude-standard", "--", "figures")
	if err != nil {
		return err
	}
	audited := map[string]axid.ID{}
	for _, id := range papers {
		audited[corpus.FiguresDir("", id)] = id
	}
	for _, f := range append(strays(plain, false), strays(skipped, true)...) {
		id, ok := audited[path.Dir(f.path)]
		if !ok && f.paper() {
			// A figure of a paper this run is not looking at. Auditing one paper
			// and hearing about another one's pictures is noise.
			continue
		}
		find := Finding{Rule: "F04", File: f.path, What: f.what()}
		if ok {
			find.Shard, find.ID = corpus.Shard(id), id.Canonical
		}
		col.add(find)
	}
	return nil
}

// stray is one file under figures/ that git is not carrying.
type stray struct {
	path    string
	ignored bool
}

// paper says whether the path is where a paper's figures go, which is the shape
// corpus.FiguresDir writes and nothing else. A file anywhere else under figures/
// was put there by hand and belongs to no paper, so no run can decide it is
// somebody else's problem.
func (s stray) paper() bool {
	return len(strings.Split(s.path, "/")) == 4
}

func (s stray) what() string {
	if s.ignored {
		return "is under figures/ and git has been told to ignore it, so it is in this corpus on this machine and in no other copy of it, and a rule that reads a directory git skips cannot fail twice"
	}
	if !s.paper() {
		return "is under figures/ and no commit records it, and it is not where a paper's figures go either, so nothing here wrote it"
	}
	return "is under figures/ and no commit records it, so this copy of the corpus has a picture no other copy does"
}

// strays reads the file names git printed.
//
// ls-files prints paths relative to the directory it ran in, which is the corpus
// root, so they arrive in the form a finding prints and need nothing done to
// them.
func strays(out []byte, ignored bool) []stray {
	var files []stray
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, stray{path: line, ignored: ignored})
		}
	}
	return files
}

// removal is one register line a commit or the working tree took out.
type removal struct {
	path   string
	tag    tags.Tag
	local  string
	commit string
}

func (r removal) what() string {
	where := "the working tree"
	if r.commit != "" {
		where = r.commit[:min(len(r.commit), 12)]
	}
	return "had " + string(r.tag) + " on " + r.local + " and " + where + " took the line out, and a tag is permanent, so this wants a revert rather than the line put back"
}

// removed reads a patch and returns every register line it deletes.
//
// The diff is read rather than every historical copy of every register, because
// a corpus is a repository with one commit per paper per run and reading each
// file once per commit is reading the whole corpus once per commit.
func removed(patch []byte, prefix string) []removal {
	var out []removal
	var commit, file string
	s := bufio.NewScanner(bytes.NewReader(patch))
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "commit "):
			commit = strings.TrimPrefix(line, "commit ")
		case strings.HasPrefix(line, "--- a/"):
			file = under(strings.TrimPrefix(line, "--- a/"), prefix)
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "---"):
			// The other half of the header, and a file that is being added has
			// no a/ side worth reading.
		case strings.HasPrefix(line, "-"):
			if file == "" {
				continue
			}
			t, local, ok := entryLine(strings.TrimPrefix(line, "-"))
			if !ok {
				continue
			}
			out = append(out, removal{path: file, tag: t, local: local, commit: commit})
		}
	}
	return out
}

// entryLine reads a register line the way G01 would, and says no to anything
// else.
//
// The comment block at the top of a register is rewritten whenever its wording
// changes and a version line moves every time the paper does, and neither of
// those is a tag going missing.
func entryLine(line string) (tags.Tag, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	fields := strings.SplitN(line, ",", 3)
	if len(fields) < 2 {
		return "", "", false
	}
	t := tags.Tag(fields[0])
	if !t.Valid() || fields[1] == "" {
		return "", "", false
	}
	return t, fields[1], true
}

// under puts a path reported by git back into the corpus's own terms.
//
// Git reports paths from the top of the repository and the corpus may be a
// directory inside one, so the prefix comes off the front. A path that is not
// under it is not this corpus's file.
func under(p, prefix string) string {
	if prefix == "" {
		return p
	}
	if !strings.HasPrefix(p, prefix) {
		return ""
	}
	return strings.TrimPrefix(p, prefix)
}

// repository says whether the corpus is inside a git repository with something
// committed to it, and where in that repository it sits.
//
// A corpus nobody has committed has no history, and a rule about what used to be
// in a file has nothing to read. That is reported as not run rather than as a
// pass, because a pass here would be saying every tag ever handed out is still
// there and nothing has looked.
func repository(root string) (string, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false
	}
	out, err := git(root, "rev-parse", "--show-prefix")
	if err != nil {
		return "", false
	}
	if _, err := git(root, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// git runs one git command in the corpus and returns what it printed.
func git(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	// A repository that has never been committed to has no HEAD, and a git that
	// reads the user's own configuration reads their rename and diff settings
	// too, which would make a rule pass on one machine and fail on another.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, gitErr{err: err, args: args, said: strings.TrimSpace(stderr.String())}
	}
	return out, nil
}

type gitErr struct {
	err  error
	args []string
	said string
}

func (e gitErr) Error() string {
	s := "git " + strings.Join(e.args, " ") + ": " + e.err.Error()
	if e.said != "" {
		s += ": " + e.said
	}
	return s
}

func (e gitErr) Unwrap() error { return e.err }
