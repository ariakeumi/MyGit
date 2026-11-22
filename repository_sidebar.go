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

func invalidateMainWindow() {
	if mainWin != nil && mainWin.win != nil {
		mainWin.win.Invalidate()
	}
}

func toggleRepositorySidebar() {
	prefs.RepoSidebarHidden = !prefs.RepoSidebarHidden
	prefs.save()
	invalidateMainWindow()
}

func repositorySidebarToggle(c *ui.Context, label string) {
	if iconButton(c, "panel-left", label, label+" · "+shiftShortcut("B")) {
		toggleRepositorySidebar()
	}
}

func (a *app) repositorySidebar(c *ui.Context) {
	current := ""
	if a.local() {
		current = a.repo.Dir
		if a.ws != nil {
			current = a.ws.dir
		}
	}
	repositorySidebar(c, current)
}

// Keep each group's original order, so unpinning restores a project's place.
func sidebarRepositories() []string {
	dirs := make([]string, 0, len(prefs.LocalRepos))
	for _, pinned := range []bool{true, false} {
		for _, dir := range prefs.LocalRepos {
			if prefs.PinnedRepos[dir] == pinned {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

func repositorySidebar(c *ui.Context, current string) {
	t := c.Theme()
	side := ui.Column(c.Key("repository-sidebar")).Width(repositorySidebarWidth).Shrink(0).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
	background := t.Surface
	if c.Vibrancy() {
		background = ui.Transparent
	}
	side.Background(background)
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
				for _, dir := range sidebarRepositories() {
					pinned := prefs.PinnedRepos[dir]
					row := ui.SidebarItem(c, dir, icons["repo"], filepath.Base(dir)).Tooltip(homeRelative(dir))
					if pinned {
						row.Children(func() {
							ui.Icon(c, icons["pin"]).Size(12, 12).Shrink(0).Tooltip(loc.PinnedRepository())
						})
					}
					row.ContextMenu(func(m *ui.Menu) {
						label := loc.PinRepository()
						if pinned {
							label = loc.UnpinRepository()
						}
						if m.Item(label).Chosen() {
							if pinned {
								delete(prefs.PinnedRepos, dir)
							} else {
								if prefs.PinnedRepos == nil {
									prefs.PinnedRepos = map[string]bool{}
								}
								prefs.PinnedRepos[dir] = true
							}
							prefs.save()
							invalidateMainWindow()
						}
						m.Separator()
						if m.Item(loc.RemoveRepository()).Chosen() {
							prefs.LocalRepos = slices.DeleteFunc(prefs.LocalRepos, func(d string) bool { return d == dir })
							delete(prefs.PinnedRepos, dir)
							prefs.save()
							invalidateMainWindow()
						}
						if mainWin != nil && m.Item(loc.DeleteLocalRepository()).Disabled(mainWin.repoDelete.running).Chosen() {
							mainWin.repoDelete = repositoryDeletion{dir: dir, confirm: true}
						}
					})
				}
			})
		}).Grow(1).Background(background).Label(loc.LocalRepositories()).OnChange(func() {
			dir := *selected
			*selected = current
			if dir != "" && dir != current {
				openSidebarRepository(dir)
			}
		})
		if current != "" {
			ui.Row(c.Key("clone-action")).Padding(8).Children(func() {
				ui.Button(c, loc.CloneMenu()).Grow(1).OnClick(openClone)
			})
		}
	})
}

func openSidebarRepository(dir string) {
	if a := apps[dir]; a != nil {
		ensureMainWindow().activate(a)
		return
	}
	if ws := workspaces[dir]; ws != nil {
		ensureMainWindow().activate(ws.apps[ws.active])
		return
	}
	go func() {
		whole, repos, err := openFolders(context.Background(), git.Local, listLocal, dir)
		mygo.RunOnMain(func() {
			if !slices.Contains(prefs.LocalRepos, dir) {
				return
			}
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
