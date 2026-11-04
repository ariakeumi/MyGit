package main

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	loc "mygit/locales"
)

const repositorySidebarWidth = 204

func repositoryWindowOptions(opts mygo.WindowOptions) mygo.WindowOptions {
	if macOS {
		opts.Vibrancy = mygo.VibrancySidebar
	}
	return customTitleBar(opts)
}

// Seed the sidebar from earlier versions' local recents once.
func loadLocalRepositories() {
	if prefs.LocalRepos != nil {
		return
	}
	prefs.LocalRepos = []string{}
	for _, dir := range append(slices.Clone(prefs.Recent), recentRepos()...) {
		if filepath.IsAbs(dir) && !slices.Contains(prefs.LocalRepos, dir) {
			prefs.LocalRepos = append(prefs.LocalRepos, dir)
		}
	}
	prefs.save()
}

func invalidateRepositoryWindows() {
	for _, a := range apps {
		a.win.Invalidate()
	}
	for _, ws := range workspaces {
		ws.win.Invalidate()
	}
}

func toggleRepositorySidebar() {
	prefs.RepoSidebarHidden = !prefs.RepoSidebarHidden
	prefs.save()
	invalidateRepositoryWindows()
}

func repositorySidebarToggle(c *ui.Context, label string) {
	if iconButton(c, "panel-left", label, label+" · "+shiftShortcut("B")) {
		toggleRepositorySidebar()
	}
}

func (a *app) repositorySidebar(c *ui.Context) {
	t := c.Theme()
	side := ui.Column(c.Key("repository-sidebar")).Width(repositorySidebarWidth).Shrink(0).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
	background := t.Surface
	if c.Vibrancy() {
		background = ui.Transparent
	}
	side.Background(background)
	current := ""
	if a.local() {
		current = a.repo.Dir
		if a.ws != nil {
			current = a.ws.dir
		}
	}
	selected := ui.Local(side, "selection", func() string { return current })
	side.Children(func() {
		bar := titleBar(c)
		bar.Padding(0, 12, 0, max(c.TitleBar().Left+8, 12)).Children(func() {
			repositorySidebarToggle(c, loc.HideRepoSidebar())
			ui.Box(c).Grow(1)
			if iconButton(c, "plus", loc.AddLocalRepo(), loc.AddLocalRepo()+" · "+shortcut("O")) {
				go pickAndOpen()
			}
		})
		ui.Sidebar(c, selected, func() {
			ui.SidebarSection(c, loc.LocalRepositories(), nil, func() {
				for _, dir := range prefs.LocalRepos {
					ui.SidebarItem(c, dir, icons["repo"], filepath.Base(dir)).Tooltip(homeRelative(dir))
				}
			})
		}).Grow(1).Background(background).Label(loc.LocalRepositories()).OnChange(func() {
			dir := *selected
			*selected = current
			if dir != "" && dir != current {
				openSidebarRepository(dir)
			}
		})
	})
}

func openSidebarRepository(dir string) {
	if a := apps[dir]; a != nil {
		a.win.Show()
		a.win.Focus()
		return
	}
	if ws := workspaces[dir]; ws != nil {
		ws.win.Show()
		ws.win.Focus()
		return
	}
	go func() {
		whole, repos, err := openFolders(context.Background(), git.Local, listLocal, dir)
		mygo.RunOnMain(func() {
			if err != nil {
				showWelcome()
				welcomeWin.failed(dir, err)
				welcomeWin.win.Invalidate()
				return
			}
			show("", whole, repos)
		})
	}()
}
