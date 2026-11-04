package github

import (
	"context"
	"fmt"
	"net/http"
)

// Repository is a repository available to the authenticated GitHub account.
type Repository struct {
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
}

// Repositories lists the account's repositories, most recently pushed first.
func (c *Client) Repositories(ctx context.Context) ([]Repository, error) {
	c = c.withCredentials(ctx)
	if c.Token == "" {
		return nil, ErrNoToken
	}
	const pageSize = 100
	var repos []Repository
	for page := 1; ; page++ {
		path := fmt.Sprintf("/user/repos?sort=pushed&direction=desc&per_page=%d&page=%d", pageSize, page)
		var batch []Repository
		if err := c.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		repos = append(repos, batch...)
		if len(batch) < pageSize {
			return repos, nil
		}
	}
}
