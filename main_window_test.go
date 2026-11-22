package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"mygit/internal/git"
	loc "mygit/locales"
)

func testMainWindow(t *testing.T) *mainWindow {
	t.Helper()
	oldMain, oldWelcome, oldApps, oldWorkspaces, oldPrefs := mainWin, welcomeWin, apps, workspaces, *prefs
	t.Cleanup(func() {
		mainWin, welcomeWin, apps, workspaces, *prefs = oldMain, oldWelcome, oldApps, oldWorkspaces, oldPrefs
	})
	mainWin, welcomeWin = &mainWindow{}, &welcome{}
	apps, workspaces = map[string]*app{}, map[string]*workspace{}
	return mainWin
}

func TestMainWindowSwitching(t *testing.T) {
	m := testMainWindow(t)
	dir, repos, err := openFolders(context.Background(), git.Local, listLocal, workspaceDir(t))
	if err != nil {
		t.Fatal(err)
	}
	ws, view, settle := startWorkspace(&workspace{dir: dir, closed: map[string]bool{}}, repos, func(_ func(*ui.Context)) *ui.Tester {
		return ui.NewTester(m.view, 1200, 900)
	})
	workspaces[ws.dir] = ws
	a := newApp(testRepo(t))
	queue := make(chan func(), 64)
	a.update = func(fn func()) { queue <- fn }
	apps[a.repo.Location()] = a
	a.start()
	(&tester{Tester: view, a: a, queue: queue}).settle()
	prefs.LocalRepos = []string{a.repo.Dir, ws.dir}
	prefs.RepoSidebarHidden = false

	openSidebarRepository(a.repo.Dir)
	view.Frame()
	if err := view.Click(loc.CommitSummary()); err != nil {
		t.Fatal(err)
	}
	view.Type("Keep this draft")
	view.Frame()
	if a.message != "Keep this draft" {
		t.Fatalf("summary %q", a.message)
	}
	// Keep an edited file across both a workspace and the empty page.
	a.detail = detail{whole: true, change: &git.Change{Path: "notes.txt", Work: 'M'}, diff: textDiff("notes.txt", "", "remember\n")}
	a.detail.diff.New.Edit(editor.Edit{Start: 0, End: 0, Text: "unsaved\n"})
	diff := a.detail.diff
	for range 2 {
		if err := view.Click(ws.name()); err != nil {
			t.Fatal(err)
		}
		view.Frame()
		if m.current().ws != ws || !view.HasText("brush.ts") || !view.HasText("page.ts") {
			t.Fatalf("workspace not selected: %q", view.Texts())
		}
		ws.activate(1)
		settle()
		view.Frame()
		if m.current() != ws.apps[1] {
			t.Fatal("workspace selection did not reach the main view")
		}
		showWelcome()
		view.Frame()
		if m.current() != nil || !view.HasText(loc.CloneMenu()) {
			t.Fatal("empty page not selected")
		}
		if err := view.Click(a.repo.Name()); err != nil {
			t.Fatal(err)
		}
		view.Frame()
		if mainWin != m || m.current() != a || a.message != "Keep this draft" || a.detail.diff != diff || !a.unsaved() {
			t.Fatal("switching lost the window, repository, draft or edit")
		}
		view.Key(ui.Cmd|ui.Shift, ui.KeyB)
		view.Frame()
		view.Key(ui.Cmd|ui.Shift, ui.KeyB)
		view.Frame()
		if !view.HasText(loc.LocalRepositories()) {
			t.Fatal("sidebar did not return after switching")
		}
	}
	if out := os.Getenv("MYGIT_DUMP"); out != "" {
		f, err := os.Create(filepath.Join(out, "single-window.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, view.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMainWindowCloseHiddenEditors(t *testing.T) {
	m := testMainWindow(t)
	a := newApp(testRepo(t))
	apps[a.repo.Location()] = a
	ws := &workspace{dir: "workspace", closed: map[string]bool{}}
	b := newApp(testRepo(t))
	b.ws, ws.apps = ws, []*app{b}
	workspaces[ws.dir] = ws
	for _, a := range []*app{a, b} {
		a.detail = detail{whole: true, change: &git.Change{Path: "notes.txt", Work: 'M'}, diff: textDiff("notes.txt", "", "remember\n")}
		a.detail.diff.New.Edit(editor.Edit{Start: 0, End: 0, Text: "unsaved\n"})
	}
	view := ui.NewTester(m.view, 1200, 800)
	closed := false
	var closeWindow func()
	closeWindow = func() {
		if !m.leaving(closeWindow) {
			closed = true
		}
	}
	closeWindow()
	view.Frame()
	if closed || m.current() == nil || !view.HasText(loc.UnsavedTitle("notes.txt")) {
		t.Fatal("closing did not show a hidden unsaved editor")
	}
	if err := view.Click(loc.Cancel()); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if closed || !a.unsaved() || !b.unsaved() {
		t.Fatal("cancel discarded edits or closed the window")
	}
	closeWindow()
	for range 2 {
		view.Frame()
		if err := view.Click(loc.DontSave()); err != nil {
			t.Fatal(err)
		}
		view.Frame()
	}
	if !closed || a.unsaved() || b.unsaved() {
		t.Fatal("close did not visit both retained editors")
	}
}
