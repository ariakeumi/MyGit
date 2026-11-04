package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	"mygit/internal/github"
	loc "mygit/locales"
)

const (
	changesTab = iota
	historyTab
	stashTab
)

func (a *app) leftPanel(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Children(func() {
		ui.Row(c.Key("repository-tabs")).Height(36).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			for i, label := range []string{loc.ChangesTab(), loc.History(), loc.StashTab(len(a.stashes))} {
				tab := ui.ButtonBase(c.Key(i)).Label(label).Grow(1).Basis(0).Height(36).Center().Gap(6).Cursor(ui.CursorPointer)
				if tab.Hovered() {
					tab.Background(t.Text.Alpha(0.06))
				}
				if i == a.leftTab {
					tab.BorderWidth(0, 0, 2, 0).BorderColor(t.Accent)
				}
				tab.Children(func() {
					ui.Text(c, label).SingleLine().Bold()
					if n := len(a.status.Changes); i == changesTab && n > 0 {
						ui.Badge(c, strconv.Itoa(n))
					}
				})
				tab.OnClick(func() { a.selectTab(i) })
			}
		})
		ui.Column(c.Key(a.leftTab)).Grow(1).Basis(0).Children(func() {
			switch a.leftTab {
			case historyTab:
				a.logView(c)
			case stashTab:
				a.stashPanel(c)
			default:
				if a.ws != nil {
					a.ws.sidebar(c)
				} else {
					a.changesPanel(c)
				}
			}
		})
	})
}

func (a *app) selectTab(tab int) {
	if a.leftTab == tab {
		return
	}
	if a.leaving(func() { a.selectTab(tab) }) {
		return
	}
	a.leftTab = tab
	a.detail = detail{gen: a.detail.gen + 1}
	switch tab {
	case changesTab:
		if a.stagedSel >= 0 {
			a.chooseChange(a.stagedSel, true)
		} else if a.unstagedSel >= 0 {
			a.chooseChange(a.unstagedSel, false)
		}
	case historyTab:
		a.updateMatches()
		if a.logSel >= 0 && a.logSel < a.logCount() && !a.compareChosen() {
			a.showCommit(a.commitAt(a.logSel))
		}
	case stashTab:
		if a.stashSel >= 0 && a.stashSel < len(a.stashes) {
			a.showStash(a.stashes[a.stashSel])
		}
	}
}

func (a *app) focusHistory() {
	if a.leftTab != historyTab && a.leaving(a.focusHistory) {
		return
	}
	a.selectTab(historyTab)
	if prefs.SidebarHidden {
		a.toggleSidebar()
	}
	a.searchBox.Focus()
}

func (a *app) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := titleBar(c)
	if !prefs.RepoSidebarHidden {
		bar.Padding(0, c.TitleBar().Right+8, 0, 12)
	}
	bar.Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		if prefs.RepoSidebarHidden {
			repositorySidebarToggle(c, loc.ShowRepoSidebar())
		}
		ui.Image(c, icon).Size(20, 20)
		ui.Text(c, "MyGit").Bold()
		if a.ws != nil {
			ui.Text(c, a.ws.name()).TextColor(t.TextMuted).SingleLine()
		}
		ui.Box(c).Grow(1)
		if !a.local() {
			a.remoteBadge(c)
		}
		a.githubLogin.button(c)
		a.commentsButton(c)
		if iconButton(c, "panel-center", a.sidebarLabel(), a.sidebarLabel()+" · "+shortcut("B")) {
			a.toggleSidebar()
		}
	})
	ui.Row(c.Key("repository-bar")).Height(52).AlignItems(ui.Stretch).Background(t.Surface).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		repo := desktopButton(c.Key("repository"), "folder", loc.CurrentRepository(), a.repo.Name(), true).Tooltip(a.repo.Location())
		repo.Menu(a.repositorySwitcher)
		branch := a.status.Branch
		if branch == "" {
			branch = loc.Detached()
		}
		a.branchesButton(c, branch)
		a.syncButton(c)
		ui.Box(c).Grow(1)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).PaddingX(10).Gap(5).Children(func() {
			if iconButton(c, "pull-request", loc.PullRequestsMenu()) {
				a.openPulls()
			}
			iconMenu(c, "more", loc.Repository(), a.repoMenu)
		})
	})
}

func desktopButton(c *ui.Context, glyph, title, subtitle string, dropdown bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Width(198).MinWidth(0).Shrink(1).PaddingX(12).Gap(9).AlignItems(ui.Center).Justify(ui.Start).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Cursor(ui.CursorPointer).Label(title)
	if b.Hovered() {
		b.Background(t.Text.Alpha(0.07))
	}
	b.Children(func() {
		ui.Icon(c, icons[glyph]).Size(16, 16).Shrink(0).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(11).TextColor(t.TextMuted).SingleLine()
			ui.Text(c, subtitle).FontSize(13).Bold().SingleLine()
		})
		if dropdown {
			ui.Icon(c, icons["chevron-down"]).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
		}
	})
	return b
}

func (a *app) repositorySwitcher(m *ui.Menu) {
	if ws := a.ws; ws != nil {
		for i, other := range ws.apps {
			if !ws.hidden(i) && m.Item(other.repo.Name()).Radio(i == ws.active).Chosen() {
				ws.activate(i)
			}
		}
		m.Separator()
	}
	if m.Item(loc.MenuOpenRepo()).Chosen() {
		go pickAndOpen()
	}
	if m.Item(loc.CloneMenu()).Chosen() {
		showWelcome()
		welcomeWin.openClone()
		welcomeWin.win.Invalidate()
	}
	if recent := recentLocations(); len(recent) > 0 {
		m.Separator()
		for _, location := range recent {
			if m.Item(homeRelative(location)).Radio(location == a.repo.Location()).Chosen() {
				openRecent(location)
			}
		}
	}
}

// syncRemote uses the tracking remote when there is one, then origin or
// the first configured remote. A freshly added remote may have no refs yet.
func (a *app) syncRemote() string {
	tracking, fallback := "", ""
	for _, r := range a.remoteConfig {
		if strings.HasPrefix(a.status.Upstream, r.Name+"/") && len(r.Name) > len(tracking) {
			tracking = r.Name
		}
		if fallback == "" || r.Name == "origin" {
			fallback = r.Name
		}
	}
	if tracking != "" {
		return tracking
	}
	return fallback
}

func (a *app) detailWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	if !prefs.RepoSidebarHidden {
		width -= repositorySidebarWidth
	}
	if !prefs.SidebarHidden {
		width -= a.sidebarW + 1
	}
	return width
}

func (a *app) syncButton(c *ui.Context) {
	st, remote := a.status, a.syncRemote()
	pull := func(mode git.PullMode) {
		a.runPull(loc.OpPull(), func(ctx context.Context) error { return a.repo.Pull(ctx, mode) })
	}
	fetch := func(name string) {
		a.run(loc.OpFetchRemote(name), func(ctx context.Context) error { return a.repo.FetchRemote(ctx, name) }, nil)
	}
	label, glyph, action := loc.RemotesMenu(), "server", a.openRemotes
	subtitle := st.Upstream
	switch {
	case a.busy != "":
		label = a.busy + "…"
	case remote == "":
		subtitle = loc.RemotesNone()
	case st.Branch != "" && st.Upstream == "" && st.HasHEAD:
		label, glyph, action = loc.PublishBranch(), "up", a.pushBranch
		subtitle = remote + "/" + st.Branch
	case st.Upstream != "" && st.Behind > 0:
		label, glyph, action = loc.PullRemote(remote), "chevron-down", func() { pull(git.PullConfigured) }
	case st.Upstream != "" && st.Ahead > 0:
		label, glyph, action = loc.PushRemote(remote), "up", a.pushBranch
	default:
		label, glyph, action = loc.FetchRemote(remote), "refresh", func() { fetch(remote) }
		subtitle = remote
	}
	if count := counts(st); count != "" {
		subtitle += "  " + count
	}
	ui.Row(c.Key("sync")).Width(230).MinWidth(0).Shrink(1).AlignItems(ui.Stretch).Children(func() {
		desktopButton(c.Key("primary"), glyph, label, subtitle, false).Grow(1).Basis(0).
			Disabled(a.busy != "").OnClick(action)
		drop := ui.ButtonBase(c.Key("options")).Width(30).Shrink(0).Center().Label(loc.SyncOptions()).
			Tooltip(loc.SyncOptions()).Disabled(a.busy != "").BorderWidth(0, 1, 0, 0).BorderColor(c.Theme().Border)
		if drop.Hovered() {
			drop.Background(c.Theme().Text.Alpha(0.07))
		}
		drop.Children(func() { ui.Icon(c, icons["chevron-down"]).Size(13, 13) })
		drop.Menu(func(m *ui.Menu) {
			if m.Item(loc.FetchAll()).Disabled(remote == "").Chosen() {
				a.run(loc.OpFetch(), a.repo.Fetch, nil)
			}
			for _, r := range a.remoteConfig {
				if m.Item(loc.FetchRemote(r.Name)).Chosen() {
					fetch(r.Name)
				}
			}
			m.Separator()
			for _, opt := range []struct {
				label string
				mode  git.PullMode
			}{{loc.PullFF(), git.PullFastForward}, {loc.PullRebase(), git.PullRebase}, {loc.PullMerge(), git.PullMerge}} {
				if m.Item(opt.label).Disabled(st.Upstream == "").Chosen() {
					pull(opt.mode)
				}
			}
			if m.Item(loc.PushMenu()).Disabled(remote == "" || st.Branch == "" || !st.HasHEAD).Chosen() {
				a.openPush()
			}
			m.Separator()
			if m.Item(loc.Refresh()).Chosen() {
				a.refresh()
			}
		})
	})
}

func (a *app) gitHubURL() string {
	preferred := a.syncRemote()
	fallback := ""
	for _, r := range a.remoteConfig {
		if owner, repo, ok := github.ParseRemote(r.URL); ok {
			link := "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
			if r.Name == preferred {
				return link
			}
			if fallback == "" {
				fallback = link
			}
		}
	}
	return fallback
}

func (a *app) showInFolder() {
	mygo.Shell.ShowItemInFolder(a.repo.Dir)
}

func (a *app) emptyDetail(c *ui.Context) {
	title, hint, glyph := loc.SelectChange(), loc.SelectChangeHint(), "list"
	clean := a.leftTab == changesTab && len(a.status.Changes) == 0 && !a.refreshing
	switch {
	case a.leftTab == historyTab:
		title, hint, glyph = loc.SelectCommit(), loc.SelectCommitHint(), "history"
	case a.leftTab == stashTab:
		title, hint, glyph = loc.SelectStash(), loc.SelectStashHint(), "folder"
	case clean:
		title, hint, glyph = loc.NoLocalChanges(), loc.NoLocalChangesHint(), "check"
	}
	t := c.Theme()
	ui.Column(c).Fill().Center().Padding(24).Gap(10).Children(func() {
		if a.refreshing && a.leftTab == changesTab && len(a.status.Changes) == 0 {
			ui.Spinner(c).Size(20, 20)
			return
		}
		ui.Box(c).Size(48, 48).Radius(12).Background(t.Accent.Alpha(0.10)).Center().Children(func() {
			ui.Icon(c, icons[glyph]).Size(22, 22).TextColor(t.Accent)
		})
		ui.Text(c, title).FontSize(18).Bold().TextAlign(ui.Center)
		ui.Text(c, hint).Wrap().MaxWidth(390).TextAlign(ui.Center).TextColor(t.TextMuted)
		if clean {
			if link := a.gitHubURL(); link != "" {
				services := c.Services()
				ui.Button(c, loc.ViewOnGitHub()).OnClick(func() { services.OpenURL(link) })
			} else if a.local() {
				ui.Button(c, loc.ShowInFolder()).OnClick(a.showInFolder)
			}
		}
	})
}
