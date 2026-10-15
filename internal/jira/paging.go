package jira

import (
	"context"
	"maps"
	"net/url"
	"strconv"
)

// page is the startAt / maxResults / isLast shape most Jira list endpoints use.
type page[T any] struct {
	StartAt    int  `json:"startAt"`
	MaxResults int  `json:"maxResults"`
	Total      int  `json:"total"`
	IsLast     bool `json:"isLast"`
	Values     []T  `json:"values"`
}

// GetAll fetches every page of a startAt-paginated endpoint. The request
// budget bounds how many pages can be fetched.
func GetAll[T any](ctx context.Context, c *Client, apiPath string, query url.Values) ([]T, error) {
	q := url.Values{}
	if query != nil {
		q = maps.Clone(query)
	}
	var all []T
	for start := 0; ; {
		q.Set("startAt", strconv.Itoa(start))
		var p page[T]
		if err := c.Get(ctx, apiPath, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Values...)
		if p.IsLast || len(p.Values) == 0 {
			return all, nil
		}
		start += len(p.Values)
	}
}

// PostReadAll fetches every page of a paginated read-only POST endpoint, such
// as the custom field context lookup, sending the same body for each page.
func PostReadAll[T any](ctx context.Context, c *Client, apiPath string, body any) ([]T, error) {
	var all []T
	for start := 0; ; {
		var p page[T]
		if err := c.PostRead(ctx, apiPath, url.Values{"startAt": {strconv.Itoa(start)}}, body, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Values...)
		if p.IsLast || len(p.Values) == 0 {
			return all, nil
		}
		start += len(p.Values)
	}
}
