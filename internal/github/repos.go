package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

// Repository is a repository available to the authenticated GitHub account.
type Repository struct {
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
}

var repositoryNameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ValidRepositoryName(name string) bool {
	return name != "." && name != ".." && repositoryNameRE.MatchString(name)
}

// RenameRepository returns the name from GitHub after updating the repository.
func (c *Client) RenameRepository(ctx context.Context, name string) (string, error) {
	if !ValidRepositoryName(name) {
		return "", errors.New("invalid GitHub repository name")
	}
	client := *c
	h := http.Client{Timeout: requestTimeout}
	if c.HTTP != nil {
		h = *c.HTTP
	}
	// A stale repository URL can redirect to its new location. Following
	// a redirect can turn a PATCH into a GET and look like a successful rename.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.HTTP = &h
	var result struct {
		Name string `json:"name"`
	}
	if err := client.do(ctx, http.MethodPatch, client.repoPath(""), map[string]string{"name": name}, &result); err != nil {
		return "", err
	}
	if !ValidRepositoryName(result.Name) {
		return "", errors.New("GitHub returned no valid repository name")
	}
	return result.Name, nil
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
