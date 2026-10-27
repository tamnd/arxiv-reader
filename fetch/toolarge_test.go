package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A body over the cap is a TooLarge, so a batch can tell it from a failure of
// the network and go on with the next paper.
func TestABodyOverTheCapIsTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("twelve bytes"))
	}))
	t.Cleanup(srv.Close)

	f := sourceFetcher(srv.URL)
	_, err := f.do(context.Background(), request{url: srv.URL + "/html/2610.22074v1", ref: "2610.22074v1", cap: 4, what: "a rendering"})
	var big *TooLarge
	if !errors.As(err, &big) {
		t.Fatalf("got %v, want a TooLarge", err)
	}
	if big.Cap != 4 || big.What != "a rendering" {
		t.Fatalf("the error is %+v", big)
	}
	if _, err := f.do(context.Background(), request{url: srv.URL + "/html/2610.22074v1", ref: "2610.22074v1", cap: 12, what: "a rendering"}); err != nil {
		t.Fatalf("a body at the cap gave %v", err)
	}
}
