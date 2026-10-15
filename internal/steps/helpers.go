package steps

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// pagedGet reads every page of a paged list a write step needs: Jira's
// {isLast, values} pages, 100 at a time. Evaluation reads through the
// client's GetAll; a step's action reads through the Sender, so the read
// goes through the same audit log as its writes.
func pagedGet[T any](ctx context.Context, snd Sender, path string, query url.Values) ([]T, error) {
	var all []T
	for start := 0; ; {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("startAt", strconv.Itoa(start))
		q.Set("maxResults", "100")
		var page struct {
			IsLast bool `json:"isLast"`
			Values []T  `json:"values"`
		}
		if err := snd.Send(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		if page.IsLast || len(page.Values) == 0 {
			return all, nil
		}
		start += len(page.Values)
	}
}

// addedAndRemoved are what must be added to got, and taken out of it, to
// make it want: both in the order they appear, nil when nothing differs.
func addedAndRemoved(want, got []string) (add, remove []string) {
	for _, h := range want {
		if !slices.Contains(got, h) {
			add = append(add, h)
		}
	}
	for _, h := range got {
		if !slices.Contains(want, h) {
			remove = append(remove, h)
		}
	}
	return add, remove
}
