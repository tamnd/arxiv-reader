package licence

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// These three blocks are copied off arXiv, on 2026-09-14, from 2203.09431v1,
// 2203.09431 and 2509.24152. The whitespace and the nesting are theirs, because
// the parser has to survive exactly what they serve and not a tidied version of
// it.
const (
	absDefault = `      <div class="abs-license"><a href="http://arxiv.org/licenses/nonexclusive-distrib/1.0/" title="Rights to this article">view license</a></div>
    </div>`

	absCC = `      <div class="abs-license"><a href="http://creativecommons.org/licenses/by-sa/4.0/" title="Rights to this article" class="has_license">
          <img alt="license icon" role="presentation" src="https://arxiv.org/icons/licenses/by-sa-4.0.png"/>
          <span>view license</span>
        </a></div>
    </div>`

	absWithdrawn = `      <div class="abs-license"><div hidden>No license for this version due to withdrawn</div></div>
    </div>`
)

func page(block string) string {
	return "<html><body><div class=\"leftcolumn\">\n" + block + "\n</div></body></html>"
}

func TestParseAbs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		block     string
		want      corpus.Licence
		withdrawn bool
	}{
		{"the default licence", absDefault, corpus.LicenceArXiv, false},
		{"a creative commons licence", absCC, corpus.LicenceCCBYSA, false},
		{"a withdrawn version", absWithdrawn, corpus.LicenceUnknown, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, withdrawn, _, err := ParseAbs(page(tc.block))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("read %q, want %q", got, tc.want)
			}
			if withdrawn != tc.withdrawn {
				t.Errorf("withdrawn is %v, want %v", withdrawn, tc.withdrawn)
			}
		})
	}
}

// The link is what the page states, and it is kept so a disagreement can be
// argued about against arXiv's own words rather than against our reading of
// them.
func TestParseAbsKeepsTheLink(t *testing.T) {
	_, _, url, err := ParseAbs(page(absCC))
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://creativecommons.org/licenses/by-sa/4.0/" {
		t.Errorf("kept the link %q", url)
	}
	if _, _, url, _ := ParseAbs(page(absWithdrawn)); url != "" {
		t.Errorf("a withdrawn version kept the link %q", url)
	}
}

// A parser that reads a changed page as "no licence stated" marks everything it
// touches record access, which is the answer that stops work. A markup change
// has to arrive as a failure.
func TestParseAbsFailsRatherThanGuessing(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"a page that is not an abs page", "<html><body>404</body></html>", "states no licence block"},
		{"an empty body", "", "states no licence block"},
		{"a block with nothing in it", page(`<div class="abs-license"></div>`), "links to nothing"},
		{"a block whose sentence changed", page(`<div class="abs-license"><div hidden>this version has no licence</div></div>`), "links to nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, _, _, err := ParseAbs(tc.body)
			if err == nil {
				t.Fatalf("it read %q out of that", l)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("said %q, which does not mention %q", err, tc.want)
			}
			if l != "" {
				t.Errorf("it returned %q alongside the error", l)
			}
		})
	}
}

func TestParseAbsRejectsALinkItDoesNotKnow(t *testing.T) {
	_, _, _, err := ParseAbs(page(`<div class="abs-license"><a href="http://example.com/terms">view license</a></div>`))
	if err == nil {
		t.Fatal("a licence nobody has heard of was accepted")
	}
}

// serve is an abs page server, keyed by the versioned reference it is asked
// for, so a test can hand out a different licence per version.
func serve(t *testing.T, byRef map[string]string) (*Resolver, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/abs/")
		asked = append(asked, ref)
		block, ok := byRef[ref]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, page(block))
	}))
	t.Cleanup(srv.Close)
	// A pace of one nanosecond, because the fifteen second default is arXiv's
	// and this is localhost. The default is what a caller gets for forgetting,
	// and forgetting is not what a test does.
	return &Resolver{Base: srv.URL + "/abs/", Pace: time.Nanosecond}, &asked
}

func TestResolve(t *testing.T) {
	r, asked := serve(t, map[string]string{
		"2203.09431v1": absDefault,
		"2203.09431v2": absCC,
	})

	v1, err := r.Resolve(context.Background(), "2203.09431v1")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Licence != corpus.LicenceArXiv {
		t.Errorf("v1 read as %q", v1.Licence)
	}
	if v1.ID != "2203.09431" || v1.Version != 1 {
		t.Errorf("v1 came back as %s v%d", v1.ID, v1.Version)
	}
	if v1.Ref() != "2203.09431v1" {
		t.Errorf("the reference reads %q", v1.Ref())
	}

	v2, err := r.Resolve(context.Background(), "2203.09431v2")
	if err != nil {
		t.Fatal(err)
	}
	// This is the whole reason the command exists. The bulk surfaces carry v2's
	// licence for both versions, and translating v1 on the strength of it would
	// be translating something never offered under a licence permitting it.
	if v2.Licence != corpus.LicenceCCBYSA {
		t.Errorf("v2 read as %q", v2.Licence)
	}
	if corpus.AccessFor(v1.Licence).MayTranslate() {
		t.Error("v1 came out translatable")
	}
	if !corpus.AccessFor(v2.Licence).MayTranslate() {
		t.Error("v2 came out untranslatable")
	}
	if len(*asked) != 2 {
		t.Errorf("asked for %v", *asked)
	}
}

// Resolving a paper without a version would read the latest, which is the
// licence every bulk surface already carries, so it would spend fifteen seconds
// to learn nothing.
func TestResolveNeedsAVersion(t *testing.T) {
	r, asked := serve(t, nil)
	_, err := r.Resolve(context.Background(), "2203.09431")
	if err == nil {
		t.Fatal("a bare id was accepted")
	}
	if !strings.Contains(err.Error(), "names no version") {
		t.Errorf("said %q", err)
	}
	if len(*asked) != 0 {
		t.Errorf("it asked arXiv anyway: %v", *asked)
	}
}

func TestResolveReportsABadStatus(t *testing.T) {
	r, _ := serve(t, map[string]string{"2203.09431v1": absDefault})
	_, err := r.Resolve(context.Background(), "2203.09431v9")
	if err == nil {
		t.Fatal("a 404 was read as a licence")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("said %q, which does not mention the status", err)
	}
}

func TestResolveAll(t *testing.T) {
	r, asked := serve(t, map[string]string{
		"2203.09431v1": absDefault,
		"2203.09431v2": absDefault,
		"2203.09431v3": absCC,
	})
	got, err := r.ResolveAll(context.Background(), "2203.09431", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d versions", len(got))
	}
	// Oldest first, because that is the order the versions are in and the order
	// a person reading the output is looking for a change in.
	if got[0].Version != 1 || got[2].Version != 3 {
		t.Errorf("read them in the order %v", *asked)
	}
	if got[2].Licence != corpus.LicenceCCBYSA {
		t.Errorf("v3 read as %q", got[2].Licence)
	}
}

func TestResolveAllNeedsAVersionCount(t *testing.T) {
	r, _ := serve(t, nil)
	if _, err := r.ResolveAll(context.Background(), "2203.09431", 0); err == nil {
		t.Fatal("a paper with no versions was read")
	}
}

// The pace is arXiv's and it is not optional. A test that leaves it at zero
// gets the polite default, which is what a caller who forgets the field gets.
func TestTheDefaultPaceIsThePoliteOne(t *testing.T) {
	r, _ := serve(t, map[string]string{"2203.09431v1": absDefault, "2203.09431v2": absDefault})
	r.Pace = 0
	if _, err := r.Resolve(context.Background(), "2203.09431v1"); err != nil {
		t.Fatal(err)
	}
	// The first request does not wait, so the wait is only observable on the
	// second, and waiting fifteen seconds to see it is not worth the test. The
	// context is cancelled instead, which is what the wait is selecting on.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.Resolve(ctx, "2203.09431v2"); err == nil {
		t.Fatal("the second request went straight out")
	}
	if time.Since(start) > Pace {
		t.Error("it waited the whole pace rather than giving up with the context")
	}
}

func TestApply(t *testing.T) {
	rec := kaggle("2203.09431", 3, corpus.LicenceCCBYSA, "math.AG")
	got, changed, err := Apply(rec, []Resolution{
		{ID: "2203.09431", Version: 1, Licence: corpus.LicenceArXiv},
		{ID: "2203.09431", Version: 2, Licence: corpus.LicenceArXiv},
		{ID: "2203.09431", Version: 3, Licence: corpus.LicenceCCBYSA},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Errorf("%d versions changed, want the two the snapshot had wrong", len(changed))
	}
	for i, want := range []corpus.Licence{corpus.LicenceArXiv, corpus.LicenceArXiv, corpus.LicenceCCBYSA} {
		if got.Versions[i].Licence != want {
			t.Errorf("v%d is %q, want %q", i+1, got.Versions[i].Licence, want)
		}
		// The authority is the point. A content plane paper whose licence came
		// from the snapshot is exactly what rule S10 fails.
		if got.Versions[i].LicenceFrom != metadata.SourceAbs {
			t.Errorf("v%d names %q as its authority", i+1, got.Versions[i].LicenceFrom)
		}
	}
	// The record handed in is not touched, so a caller that decides not to
	// write still holds what it read off disk.
	if rec.Versions[0].Licence != corpus.LicenceCCBYSA {
		t.Error("the record passed in was modified")
	}
	if rec.Versions[0].LicenceFrom != metadata.SourceKaggle {
		t.Error("the authority on the record passed in was modified")
	}
}

func TestApplyRefusesToGuess(t *testing.T) {
	rec := kaggle("2203.09431", 2, corpus.LicenceCCBYSA, "math.AG")
	// A version the record has never heard of means the record is older than
	// the page, and writing to it would be writing a guess about which version
	// the count came from.
	if _, _, err := Apply(rec, []Resolution{{ID: "2203.09431", Version: 3, Licence: corpus.LicenceCCBY}}); err == nil {
		t.Error("a v3 was applied to a two version record")
	}
	if _, _, err := Apply(rec, []Resolution{{ID: "2106.09685", Version: 1, Licence: corpus.LicenceCCBY}}); err == nil {
		t.Error("a resolution for another paper was applied")
	}
}

// A record whose versions were all already right changes nothing, which is what
// keeps a re-resolve out of the git history.
func TestApplyReportsNoChangeWhenThereIsNone(t *testing.T) {
	rec := paper("2203.09431", []string{"math.AG"}, 2, corpus.LicenceCCBY, metadata.SourceAbs)
	rec.Source = metadata.SourceKaggle
	got, changed, err := Apply(rec, []Resolution{
		{ID: "2203.09431", Version: 1, Licence: corpus.LicenceCCBY},
		{ID: "2203.09431", Version: 2, Licence: corpus.LicenceCCBY},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Errorf("%d versions changed", len(changed))
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The abs page states a licence for a version and nothing else about the paper,
// so it can be an authority for a licence and can never be where a record was
// harvested from.
func TestTheAbsPageIsAnAuthorityAndNotAHarvestSurface(t *testing.T) {
	if metadata.SourceAbs.Valid() {
		t.Error("a record could claim to have been harvested from an abs page")
	}
	if !metadata.SourceAbs.ValidAuthority() {
		t.Error("the abs page cannot be named as the authority for a licence it stated")
	}
	rec := paper("2203.09431", []string{"math.AG"}, 1, corpus.LicenceCCBY, metadata.SourceAbs)
	rec.Source = metadata.SourceKaggle
	if err := rec.Validate(); err != nil {
		t.Fatal(err)
	}
	rec.Versions[0].LicenceFrom = "somewhere"
	if err := rec.Validate(); err == nil {
		t.Error("an authority nobody has heard of was accepted")
	}
}
