package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	"mygit/internal/github"
	loc "mygit/locales"
)

// clonePlan is the clone dialog: a URL, the folder to clone into, and the
// clone's progress once it runs.
type clonePlan struct {
	open           bool
	url, parent    string
	name           string // the folder's name, from the URL until edited
	named          bool
	running        bool
	progress, fail string
	cancel         context.CancelFunc
	repos          []github.Repository
	repoQuery      string
	repoErr        error
	cancelRepos    context.CancelFunc
}

var readCloneRepositories = github.New(context.Background(), "", "").Repositories

// defaultParent is where clones go: ~/Projects when there is one.
func defaultParent() string {
	home, _ := os.UserHomeDir()
	for _, d := range []string{"Projects", "projects", "Code", "code", "src"} {
		if fi, err := os.Stat(filepath.Join(home, d)); err == nil && fi.IsDir() {
			return filepath.Join(home, d)
		}
	}
	return home
}

func (w *welcome) openClone() {
	if !w.clone.open {
		w.clone = clonePlan{open: true, parent: defaultParent()}
		w.loadCloneRepositories()
	}
}

func (w *welcome) loadCloneRepositories() {
	p := &w.clone
	if p.cancelRepos != nil {
		p.cancelRepos()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelRepos, p.repos, p.repoErr = cancel, nil, nil
	read := readCloneRepositories
	go func() {
		request, finish := context.WithTimeout(ctx, 30*time.Second)
		defer finish()
		repos, err := read(request)
		w.update(func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			p.cancelRepos, p.repos, p.repoErr = nil, repos, err
		})
	}()
}

func (w *welcome) cloneRepositories(c *ui.Context) {
	p := &w.clone
	t := c.Theme()
	if errors.Is(p.repoErr, github.ErrNoToken) {
		return
	}
	ui.Text(c.Key("github-repos-title"), loc.CloneGithubRepos()).Bold()
	switch {
	case p.cancelRepos != nil:
		ui.Row(c.Key("github-repos-loading")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(14, 14)
			ui.Text(c, loc.CloneGithubLoading()).TextColor(t.TextMuted)
		})
	case p.repoErr != nil:
		ui.Text(c.Key("github-repos-error"), loc.CloneGithubFailed()).TextColor(t.Danger).Wrap().Tooltip(p.repoErr.Error())
		ui.Button(c.Key("github-repos-retry"), loc.CloneGithubRetry()).OnClick(w.loadCloneRepositories)
	case len(p.repos) == 0:
		ui.Text(c.Key("github-repos-empty"), loc.CloneGithubEmpty()).TextColor(t.TextMuted).Wrap()
	default:
		ui.TextInput(c.Key("github-repos-filter"), &p.repoQuery).Placeholder(loc.CloneGithubFilter()).Label(loc.CloneGithubFilter())
		repos := p.repos
		if query := strings.ToLower(strings.TrimSpace(p.repoQuery)); query != "" {
			repos = nil
			for _, repo := range p.repos {
				if strings.Contains(strings.ToLower(repo.FullName), query) {
					repos = append(repos, repo)
				}
			}
		}
		if len(repos) == 0 {
			ui.Text(c.Key("github-repos-no-match"), loc.CloneGithubNoMatch()).TextColor(t.TextMuted)
			return
		}
		ui.List(c.Key("github-repos-list"), nil, len(repos)).MaxHeight(150).
			ItemKey(func(i int) any { return repos[i].FullName }).
			Rows(func(row ui.ListRow) {
				repo := repos[row.Index]
				button := ui.Button(row.Context, repo.FullName).Tooltip(repo.FullName)
				if p.url == repo.CloneURL {
					button.Background(t.Selection)
				}
				button.OnClick(func() {
					p.url = repo.CloneURL
					if !p.named {
						p.name = git.RepoName(repo.CloneURL)
					}
				})
			})
	}
}

func (w *welcome) cloneView(c *ui.Context) {
	p := &w.clone
	if !p.open {
		return
	}
	t := c.Theme()
	if ui.Modal(c.Key("clone"), &p.open, func() {
		ui.Column(c).Gap(10).Width(dialogWidth(c, 460)).Children(func() {
			ui.Text(c, loc.CloneTitle()).Bold().FontSize(15)
			if ui.TextInput(c.Key("url"), &p.url).Placeholder(loc.CloneURLPlaceholder()).Label(loc.CloneURL()).AutoFocus().Disabled(p.running).Changed() && !p.named {
				p.name = git.RepoName(strings.TrimSpace(p.url))
			}
			if !p.running {
				w.cloneRepositories(c)
			}
			ui.Row(c.Key("parent")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, homeRelative(p.parent)).TextColor(t.TextMuted).SingleLine().Grow(1).Basis(0).Tooltip(p.parent)
				if ui.Button(c, loc.Change()).Disabled(p.running).Clicked() {
					go w.pickParent()
				}
			})
			if ui.TextInput(c.Key("name"), &p.name).Label(loc.FolderName()).Placeholder(loc.FolderName()).Disabled(p.running).Changed() {
				p.named = true
			}
			switch {
			case p.running:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, p.progress).TextColor(t.TextMuted).SingleLine().Grow(1).Basis(0)
				})
			case p.fail != "":
				ui.Text(c, p.fail).TextColor(t.Danger).Wrap()
			}
			ui.Row(c.Key("actions")).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, loc.Cancel()).Clicked() {
					w.stopClone()
				}
				ok := strings.TrimSpace(p.url) != "" && strings.TrimSpace(p.name) != "" && !p.running
				if ui.PrimaryButton(c, loc.Clone()).Disabled(!ok).Clicked() {
					w.startClone()
				}
			})
		})
	}).Dismissed() {
		w.stopClone()
	}
}

func (w *welcome) stopClone() {
	if w.clone.cancel != nil {
		w.clone.cancel()
	}
	if w.clone.cancelRepos != nil {
		w.clone.cancelRepos()
	}
	w.clone = clonePlan{}
}

func (w *welcome) pickParent() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w.win, Title: loc.CloneInto(), Directory: true, CreateDirectories: true})
	if err == nil && len(paths) > 0 {
		w.win.Update(func() { w.clone.parent = paths[0] })
	}
}

// startClone clones on a goroutine, showing git's progress, and opens the
// clone once done.
func (w *welcome) startClone() {
	p := &w.clone
	url, parent, name := strings.TrimSpace(p.url), p.parent, strings.TrimSpace(p.name)
	ctx, cancel := context.WithCancel(context.Background())
	p.running, p.fail, p.progress, p.cancel = true, "", loc.Connecting(), cancel
	go func() {
		err := git.Clone(ctx, parent, url, name, func(line string) {
			w.win.Update(func() {
				if ctx.Err() == nil {
					w.clone.progress = line
				}
			})
		})
		w.win.Update(func() {
			if ctx.Err() != nil {
				return // canceled
			}
			w.clone.running = false
			if err != nil {
				w.clone.fail = err.Error()
				return
			}
			w.stopClone()
			w.open(filepath.Join(parent, name))
		})
	}()
}

// initRepo asks for a folder, makes a repository in it and opens it. The
// dialog waits: call it on a goroutine.
func (w *welcome) initRepo() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w.win, Title: loc.InitDialog(), Directory: true, CreateDirectories: true})
	if err != nil || len(paths) == 0 {
		return
	}
	dir := paths[0]
	err = git.Init(context.Background(), dir)
	w.win.Update(func() {
		if err != nil {
			w.errMsg = err.Error()
			return
		}
		w.open(dir)
	})
}
