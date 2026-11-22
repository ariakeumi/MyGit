package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	"mygit/internal/github"
	loc "mygit/locales"
)

type repositoryRenameTarget struct {
	remote      git.Remote
	owner, name string
}

type repositoryRenameDialog struct {
	open   bool
	target repositoryRenameTarget
	name   string
}

func (a *app) repositoryRenameMenu(m *ui.Menu) {
	var targets []repositoryRenameTarget
	for _, remote := range a.remoteConfig {
		if owner, name, ok := github.ParseRemote(remote.URL); ok {
			targets = append(targets, repositoryRenameTarget{remote, owner, name})
		}
	}
	open := func(target repositoryRenameTarget) {
		a.repoRename = repositoryRenameDialog{open: true, target: target, name: target.name}
	}
	switch len(targets) {
	case 0:
	case 1:
		if m.Item(loc.RenameGithubRepoMenu()).Disabled(a.busy != "").Chosen() {
			open(targets[0])
		}
	default:
		m.Submenu(loc.RenameGithubRepo(), func(m *ui.Menu) {
			for _, target := range targets {
				label := target.remote.Name + " · " + target.owner + "/" + target.name + "…"
				if m.Item(label).Disabled(a.busy != "").Chosen() {
					open(target)
				}
			}
		}).Disabled(a.busy != "")
	}
}

func (a *app) repositoryRenameView(c *ui.Context) {
	p := &a.repoRename
	ui.Modal(c.Key("rename-github-repo"), &p.open, func() {
		ui.Column(c).Gap(10).Width(dialogWidth(c, 430)).Children(func() {
			t := p.target
			ui.Text(c, loc.RenameGithubRepo()).Bold().FontSize(16)
			ui.Text(c, loc.RenameGithubRepoTarget(t.owner+"/"+t.name, t.remote.Name)).Wrap()
			label := loc.RenameGithubRepoName()
			submit := func() {
				name := strings.TrimSpace(p.name)
				if name != t.name && github.ValidRepositoryName(name) && a.busy == "" {
					p.open = false
					a.renameGitHubRepository(t, name)
				}
			}
			ui.TextInput(c.Key("name"), &p.name).Label(label).Placeholder(label).AutoFocus().OnSubmit(submit)
			name := strings.TrimSpace(p.name)
			valid := github.ValidRepositoryName(name)
			if name != "" && !valid {
				ui.Text(c, loc.RenameGithubRepoInvalid()).TextColor(c.Theme().Danger).Wrap()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, loc.Cancel()).OnClick(func() { p.open = false })
				ui.PrimaryButton(c, loc.Rename()).Disabled(!valid || name == t.name || a.busy != "").OnClick(submit)
			})
		})
	})
}

func (a *app) renameGitHubRepository(target repositoryRenameTarget, name string) {
	label := loc.RenameGithubRepo()
	a.busy, a.errMsg, a.notice = label, "", ""
	repo := a.repo
	work(a, func(ctx context.Context) (string, error) {
		remotes, err := repo.Remotes(ctx)
		if err != nil {
			return "", err
		}
		if !slices.Contains(remotes, target.remote) {
			return "", errors.New(loc.RenameGithubRepoChanged())
		}
		client := newGitHub(ctx, target.owner, target.name)
		renamed, err := client.RenameRepository(ctx, name)
		if err != nil {
			return "", err
		}
		err = repo.RewriteRemoteURLs(ctx, target.remote.Name, func(rawURL string) string {
			return github.RenamedRemoteURL(rawURL, target.owner, target.name, renamed)
		})
		return renamed, err
	}, func(renamed string, err error) {
		a.busy = ""
		switch {
		case renamed != "":
			for _, pr := range a.pullReviews {
				if strings.EqualFold(pr.client.Owner, target.owner) && strings.EqualFold(pr.client.Repo, target.name) {
					client := *pr.client
					client.Repo = renamed
					pr.client = &client
				}
			}
			slug := target.owner + "/" + renamed
			if err != nil {
				a.errMsg = loc.RenameGithubRepoRemoteFailed(slug, target.remote.Name, err.Error())
			} else {
				a.notice = loc.RenameGithubRepoSuccess(slug)
			}
		case err != nil:
			var apiErr *github.Error
			errors.As(err, &apiErr)
			msg := err.Error()
			switch {
			case errors.Is(err, github.ErrNoToken) || apiErr != nil && apiErr.Status == http.StatusUnauthorized:
				msg = loc.RenameGithubRepoSignIn()
			case apiErr != nil && (apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound):
				msg = loc.RenameGithubRepoPermission(msg)
			}
			a.errMsg = loc.OpFailed(label, msg)
		}
		a.refresh()
	})
}
