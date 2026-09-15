package fetch

import (
	"context"
	"errors"
	"net/http"
)

// Rendered asks whether arXiv serves a rendering of one version, without
// downloading it.
//
// This is the one request the path decision makes, and it is a HEAD because the
// question is whether the file exists and not what is in it. A rendering is a
// few hundred kilobytes and asking for the whole of it to learn one bit would
// make deciding the path for a thousand papers cost as much as extracting them.
//
// It goes through the same pace clock as every other request on this fetcher,
// because a HEAD is still a request to arXiv and a probe that ran flat out
// would be the one part of this tool that ignored the agreement the rest of it
// keeps.
//
// The spec has this probe reading the abstract page, which also says whether a
// rendering exists. The rendering itself answers the same question in the same
// one request and answers it directly rather than by parsing a page built for a
// person, so that is what this does.
func (f *Fetcher) Rendered(ctx context.Context, ref string) (bool, error) {
	_, err := f.do(ctx, request{
		method: http.MethodHead,
		url:    f.base() + ref,
		ref:    ref,
		cap:    maxBody,
		what:   "a rendering of a paper or a picture in one",
		missing: func(ref, status string) error {
			return &NotRendered{Ref: ref, Status: status}
		},
	})
	// A version with no rendering is the answer and not a failure. It is most of
	// arXiv, and a caller that had to tell this error apart from a network error
	// itself would get it wrong once and then route a paper down the wrong path.
	var none *NotRendered
	if errors.As(err, &none) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
