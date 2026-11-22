package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	"mygit/internal/github"
	loc "mygit/locales"
)

func TestCloneFromRepository(t *testing.T) {
	m := testMainWindow(t)
	old := readCloneRepositories
	t.Cleanup(func() { readCloneRepositories = old; welcomeWin.stopClone() })
	readCloneRepositories = func(context.Context) ([]github.Repository, error) {
		return nil, github.ErrNoToken
	}
	w := welcomeWin
	queue := make(chan func(), 16)
	w.updFn = func(fn func()) { queue <- fn }
	a := newApp(testRepo(t))
	apps[a.repo.Dir] = a
	a.message = "Keep this draft"
	m.activate(a)
	prefs.RepoSidebarHidden = false
	view := ui.NewTester(m.view, 1200, 800)
	click := func(label string) {
		t.Helper()
		if err := view.Click(label); err != nil {
			t.Fatalf("%v; texts %q", err, view.Texts())
		}
		view.Frame()
	}
	loaded := func() {
		t.Helper()
		select {
		case update := <-queue:
			update()
		case <-time.After(5 * time.Second):
			t.Fatal("repository list did not finish")
		}
		view.Frame()
	}
	for range 2 {
		click(loc.CloneMenu())
		loaded()
		if !w.clone.open || !view.HasText(loc.CloneURL()) || m.current() != a {
			t.Fatalf("clone did not open over the current repository: %q", view.Texts())
		}
		click(loc.CloneURL())
		view.Type("git@example.com:team/project.git")
		view.Frame()
		if w.clone.name != "project" {
			t.Fatalf("folder name %q", w.clone.name)
		}
		// Authentication stays above the clone form, with the repository behind it.
		done := make(chan credAnswer, 1)
		w.clone.running, w.clone.parent = true, a.repo.Dir
		askUserInWindow(a.repo.Dir, "Password for 'https://example.com':", done)
		view.Frame()
		view.Type("test-token")
		click(loc.Ok())
		if answer := <-done; !answer.ok || answer.value != "test-token" {
			t.Fatalf("credential answer %+v", answer)
		}
		w.clone.running = false
		view.Frame()
		if out := os.Getenv("MYGIT_DUMP"); out != "" {
			f, err := os.Create(filepath.Join(out, "clone-repository.png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, view.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		view.Key(0, ui.KeyEscape)
		view.Frame()
		if w.clone.open || m.current() != a || a.message != "Keep this draft" {
			t.Fatal("canceling clone lost the current repository or draft")
		}
	}
	// The command remains available with the repository sidebar hidden.
	prefs.RepoSidebarHidden = true
	a.openCommands()
	a.palette.query = loc.CloneMenu()
	view.Frame()
	if !view.HasText(shiftShortcut("O")) {
		t.Fatal("the clone command didn't show its shortcut")
	}
	view.Key(0, ui.KeyEnter)
	view.Frame()
	loaded()
	if a.palette.open || !w.clone.open || m.current() != a {
		t.Fatal("the command palette did not open clone in place")
	}
	click(loc.Cancel())
}

func TestCloneShortcutWithoutMenuBar(t *testing.T) {
	m := testMainWindow(t)
	oldMacOS, oldRead := macOS, readCloneRepositories
	t.Cleanup(func() { macOS, readCloneRepositories = oldMacOS, oldRead })
	macOS = false
	readCloneRepositories = func(context.Context) ([]github.Repository, error) { return nil, nil }
	w := welcomeWin
	w.updFn = func(func()) {}
	for _, a := range []*app{nil, newApp(testRepo(t))} {
		m.activate(a)
		view := ui.NewTester(m.view, 1200, 800)
		view.Key(ui.Cmd|ui.Shift, ui.KeyO)
		view.Frame()
		if !w.clone.open || !view.HasText(loc.CloneURL()) || w.ssh.open || m.current() != a {
			t.Fatalf("expected the clone dialog over the current page, got %q", view.Texts())
		}
		w.stopClone()
	}
}

func TestCloneFailureRetryAndOpen(t *testing.T) {
	m := testMainWindow(t)
	oldRecent := addRecentDocument
	t.Cleanup(func() { addRecentDocument = oldRecent })
	addRecentDocument = func(string) error { return nil }
	w := welcomeWin
	queue := make(chan func(), 64)
	w.updFn = func(fn func()) { queue <- fn }
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefs.path = filepath.Join(t.TempDir(), "settings.json")
	source := testRepo(t)
	previous := newApp(source)
	m.activate(previous)
	w.clone = clonePlan{open: true, parent: parent, name: "copy", url: filepath.Join(parent, "missing")}
	finish := func() {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for w.clone.running {
			select {
			case update := <-queue:
				update()
			case <-deadline:
				t.Fatal("clone did not finish")
			}
		}
	}
	w.startClone()
	finish()
	if !w.clone.open || w.clone.fail == "" || m.current() != previous {
		t.Fatal("failed clone did not retain the form and previous repository")
	}
	// Retain a model for the destination so success can select it headlessly.
	destination := filepath.Join(parent, "copy")
	cloned := newApp(&git.Repo{Dir: destination})
	apps[destination] = cloned
	w.clone.url = source.Dir
	w.startClone()
	finish()
	if w.clone.open || w.errMsg != "" || m.current() != cloned || !slices.Contains(prefs.LocalRepos, destination) {
		t.Fatalf("clone was not opened and remembered: %q", w.errMsg)
	}
	repo, err := git.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	remotes, err := repo.Remotes(context.Background())
	if err != nil || len(remotes) != 1 || remotes[0].URL != source.Dir {
		t.Fatalf("cloned remotes %v: %v", remotes, err)
	}
}

func TestCloneCanceledResultIgnored(t *testing.T) {
	queue := make(chan func(), 1)
	w := &welcome{updFn: func(fn func()) { queue <- fn }}
	w.clone = clonePlan{open: true, parent: t.TempDir(), url: "https://example.com/repo.git", name: ".."}
	w.startClone()
	select {
	case update := <-queue:
		// Cancel and replace the form before the worker's result is published.
		w.stopClone()
		w.clone = clonePlan{open: true, name: "new-repository"}
		update()
	case <-time.After(5 * time.Second):
		t.Fatal("clone did not finish")
	}
	if !w.clone.open || w.clone.name != "new-repository" || w.clone.fail != "" {
		t.Fatalf("canceled result changed the new clone form: %+v", w.clone)
	}
}

func TestCloneGitHubRepositories(t *testing.T) {
	old := readCloneRepositories
	t.Cleanup(func() { readCloneRepositories = old })
	repo := github.Repository{FullName: "ada/project", CloneURL: "https://github.com/ada/project.git"}
	readCloneRepositories = func(context.Context) ([]github.Repository, error) {
		return []github.Repository{repo, {FullName: "org/tools", CloneURL: "https://github.com/org/tools.git"}}, nil
	}
	queue := make(chan func(), 1)
	w := &welcome{updFn: func(fn func()) { queue <- fn }}
	tt := ui.NewTester(w.view, 420, 520)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatalf("%v; texts %q", err, tt.Texts())
		}
		tt.Frame()
	}

	click(loc.CloneMenu())
	if !tt.HasText(loc.CloneGithubLoading()) {
		t.Fatalf("missing loading state: %q", tt.Texts())
	}
	select {
	case update := <-queue:
		update()
	case <-time.After(5 * time.Second):
		t.Fatal("the repository list did not load")
	}
	tt.Frame()
	click(loc.CloneGithubFilter())
	tt.Type("ADA/")
	tt.Frame()
	if tt.HasText("org/tools") {
		t.Fatalf("filter did not hide the other repository: %q", tt.Texts())
	}
	click(repo.FullName)
	if w.clone.url != repo.CloneURL || w.clone.name != "project" {
		t.Fatalf("clone form has URL %q and folder %q", w.clone.url, w.clone.name)
	}

	// Git can ask for credentials while the clone dialog is still open.
	w.clone.running = true
	done := make(chan credAnswer, 1)
	w.cred.ask(nil, "Password for 'https://github.com':", done)
	tt.Frame()
	tt.Type("test-token")
	click(loc.Ok())
	if answer := <-done; !answer.ok || answer.value != "test-token" {
		t.Fatalf("the credential prompt did not take input: %+v", answer)
	}
	click(loc.Cancel())
	if w.clone.open {
		t.Fatal("the clone dialog did not close")
	}
}
