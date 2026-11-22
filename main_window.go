package main

import (
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// mainWindow owns the single native window. Repository models stay alive
// while switching so drafts, edits and background operations are retained.
type mainWindow struct {
	win        *mygo.Window
	active     *app
	repoDelete repositoryDeletion
}

var mainWin *mainWindow

// Set apart from package initialization: the view reaches the SSH hooks,
// whose initializers in turn reach the main window through credential prompts.
var mainWindowView func(*mainWindow, *ui.Context)

func init() { mainWindowView = (*mainWindow).view }

func ensureMainWindow() *mainWindow {
	if mainWin != nil {
		return mainWin
	}
	m := &mainWindow{}
	w := &welcome{recent: recentLocations()}
	mainWin, welcomeWin = m, w
	m.win = mygo.NewWindow(repositoryWindowOptions(mygo.WindowOptions{
		Title: "MyGit", Width: 1200, Height: 800,
		MinWidth: 720, MinHeight: 480, StateKey: "main",
		Content: ui.View(func(c *ui.Context) { mainWindowView(m, c) }),
	}))
	w.win = m.win
	dropMenuBar(m.win)
	m.win.OnFocus(func() {
		if a := m.current(); a != nil {
			a.refresh()
		}
	})
	m.win.OnClose(func(e *mygo.CloseEvent) {
		if m.leaving(m.win.Close) {
			e.PreventDefault()
		}
	})
	m.win.OnClosed(func() {
		if a := m.current(); a != nil {
			prefs.SidebarW, prefs.FilesW, prefs.StagedPct = a.sidebarW, a.filesW, a.stagedPct
		}
		prefs.save()
		w.stopClone()
		w.stopConnect()
		w.githubLogin.close()
		for _, a := range allApps() {
			a.stop()
		}
		clear(apps)
		clear(workspaces)
		mainWin, welcomeWin = nil, nil
	})
	return m
}

func (m *mainWindow) current() *app {
	if a := m.active; a != nil && a.ws != nil {
		return a.ws.apps[a.ws.active]
	}
	return m.active
}

func (m *mainWindow) view(c *ui.Context) {
	defer m.repositoryDeleteView(c)
	if !macOS { // macOS dispatches the shortcut through the menu bar.
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyO, openClone)
	}
	if a := m.current(); a != nil {
		defer welcomeWin.cred.view(c)
		defer welcomeWin.cloneView(c)
		// Separate widget identity for each repository sharing this window.
		ui.Box(c.Key(a)).Fill().Children(func() { a.view(c) })
	} else {
		ui.Box(c.Key("welcome")).Fill().Children(func() { welcomeWin.view(c) })
	}
}

func (m *mainWindow) activate(a *app) {
	if from := m.current(); from != nil {
		prefs.SidebarW, prefs.FilesW, prefs.StagedPct = from.sidebarW, from.filesW, from.stagedPct
	}
	if a != nil && a.ws != nil {
		a.ws.activate(slices.Index(a.ws.apps, a))
	}
	m.active = a
	if m.win == nil {
		return // headless UI tests
	}
	title := "MyGit"
	if a != nil {
		name, host := a.repo.Name(), a.repo.Host()
		if a.ws != nil {
			name, host = a.ws.name(), a.ws.host
		}
		title = name + " — MyGit"
		if host != "" {
			title = name + " — " + hostName(host)
		}
	}
	m.win.SetTitle(title)
	m.win.Invalidate()
	m.win.Show()
	m.win.Focus()
}

// leaving visits every retained editor, including repositories currently
// hidden by the welcome page or another workspace, before closing or quitting.
func (m *mainWindow) leaving(next func()) bool {
	for _, a := range allApps() {
		if a.unsaved() {
			m.activate(a)
			return a.leaving(next)
		}
	}
	return false
}
