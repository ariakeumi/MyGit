package main

import (
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	loc "mygit/locales"
)

func TestRepositorySidebarPin(t *testing.T) {
	m := testMainWindow(t)
	root := t.TempDir()
	alpha, beta, gamma := filepath.Join(root, "alpha"), filepath.Join(root, "beta"), filepath.Join(root, "gamma")
	prefs.LocalRepos = []string{alpha, beta, gamma}
	prefs.PinnedRepos = nil
	prefs.path = filepath.Join(root, "settings.json")
	current := newApp(&git.Repo{Dir: alpha})
	apps[alpha] = current
	m.activate(current)
	view := ui.NewTester(func(c *ui.Context) { repositorySidebar(c, alpha) }, 204, 400)
	assertOrder := func(names ...string) {
		t.Helper()
		last := float32(-1)
		for _, name := range names {
			r, ok := view.Find(name)
			if !ok || r.Y <= last {
				t.Fatalf("incorrect sidebar order %v: %q", names, view.Texts())
			}
			last = r.Y
		}
		if !slices.Equal(prefs.LocalRepos, []string{alpha, beta, gamma}) {
			t.Fatal("pinning changed the original project order")
		}
		if m.current() != current {
			t.Fatal("context menu switched the active repository")
		}
	}
	choose := func(name, action string) {
		t.Helper()
		if err := view.RightClick(name); err != nil {
			t.Fatal(err)
		}
		if err := view.ChooseMenuItem(action); err != nil {
			t.Fatalf("%v; menu %q", err, view.Menu())
		}
		view.Frame()
	}
	assertOrder("alpha", "beta", "gamma")
	choose("beta", loc.PinRepository())
	assertOrder("beta", "alpha", "gamma")
	choose("gamma", loc.PinRepository())
	assertOrder("beta", "gamma", "alpha")

	// A fresh settings model and sidebar restore the saved pin state.
	data, err := os.ReadFile(prefs.path)
	if err != nil {
		t.Fatal(err)
	}
	restored := &settings{path: prefs.path}
	restored.read(data)
	if !restored.PinnedRepos[beta] || !restored.PinnedRepos[gamma] || restored.PinnedRepos[alpha] {
		t.Fatalf("saved pins %v", restored.PinnedRepos)
	}
	*prefs = *restored
	view = ui.NewTester(func(c *ui.Context) { repositorySidebar(c, alpha) }, 204, 400)
	assertOrder("beta", "gamma", "alpha")
	if out := os.Getenv("MYGIT_DUMP"); out != "" {
		f, err := os.Create(filepath.Join(out, "sidebar-pinned.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, view.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		choose("beta", loc.UnpinRepository())
		assertOrder("gamma", "alpha", "beta")
		choose("beta", loc.PinRepository())
		assertOrder("beta", "gamma", "alpha")
	}
	choose("beta", loc.UnpinRepository())
	choose("gamma", loc.UnpinRepository())
	assertOrder("alpha", "beta", "gamma")
	data, err = os.ReadFile(prefs.path)
	if err != nil {
		t.Fatal(err)
	}
	unpinned := &settings{}
	unpinned.read(data)
	if len(unpinned.PinnedRepos) != 0 {
		t.Fatalf("unpinning was not saved: %v", unpinned.PinnedRepos)
	}
}

func TestRepositorySidebarDelete(t *testing.T) {
	m := testMainWindow(t)
	root := t.TempDir()
	remote, dir, trashed := filepath.Join(root, "upstream.git"), filepath.Join(root, "local-copy"), filepath.Join(root, "trashed")
	run(t, root, "clone", "--bare", testRepo(t).Dir, remote)
	run(t, root, "clone", remote, dir)
	refs := func() string {
		t.Helper()
		out, err := exec.Command("git", "-C", remote, "show-ref").CombinedOutput()
		if err != nil {
			t.Fatalf("read upstream refs: %v\n%s", err, out)
		}
		return string(out)
	}
	remoteBefore := refs()
	notes := filepath.Join(dir, "local-only.txt")
	if err := os.WriteFile(notes, []byte("local work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prefs.LocalRepos = []string{dir}
	prefs.PinnedRepos = map[string]bool{dir: true}
	prefs.Recent = []string{dir}
	prefs.RepoSidebarHidden = false
	prefs.path = filepath.Join(root, "settings.json")
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(repo)
	apps[repo.Dir] = a
	m.activate(a)
	view := ui.NewTester(m.view, 1200, 800)
	queue := make(chan func(), 1)
	welcomeWin.updFn = func(fn func()) { queue <- fn }
	oldTrash := trash
	t.Cleanup(func() { trash = oldTrash })
	trashCalls := 0
	trash = func(string) error {
		trashCalls++
		return errors.New("trash is unavailable")
	}
	askDelete := func() {
		t.Helper()
		if err := view.RightClick(filepath.Base(dir)); err != nil {
			t.Fatal(err)
		}
		if err := view.ChooseMenuItem(loc.DeleteLocalRepository()); err != nil {
			t.Fatalf("%v; menu %q", err, view.Menu())
		}
		if !view.HasText(loc.DeleteLocalRepositoryMessage(dir)) || m.current() != a {
			t.Fatalf("expected confirmation for the selected repository: %q", view.Texts())
		}
	}
	askDelete()
	if err := view.Click(loc.Cancel()); err != nil {
		t.Fatal(err)
	}
	if trashCalls != 0 || m.repoDelete.running || !exists(notes) {
		t.Fatal("cancel changed the repository")
	}
	askDelete()
	confirmSidebarDeletion(t, view, queue)
	if trashCalls != 1 || !view.HasText("trash is unavailable") || !exists(notes) || m.current() != a || a.stopped || !prefs.PinnedRepos[dir] {
		t.Fatal("failed deletion changed the repository or did not show an error")
	}
	if err := view.Click(loc.Close()); err != nil {
		t.Fatal(err)
	}
	trash = func(path string) error { return os.Rename(path, trashed) }
	askDelete()
	confirmSidebarDeletion(t, view, queue)
	if exists(dir) || !exists(filepath.Join(trashed, ".git")) || !exists(filepath.Join(trashed, filepath.Base(notes))) {
		t.Fatal("the repository and its local changes were not moved together")
	}
	if m.current() != nil || appAt(repo.Dir) != nil || !a.stopped || view.HasText(filepath.Base(dir)) {
		t.Fatal("the deleted repository is still open or shown in the sidebar")
	}
	data, err := os.ReadFile(prefs.path)
	if err != nil {
		t.Fatal(err)
	}
	restored := &settings{path: prefs.path}
	restored.read(data)
	if restored.LocalRepos == nil || len(restored.LocalRepos) != 0 || len(restored.PinnedRepos) != 0 || len(restored.Recent) != 0 {
		t.Fatal("deleted repository remained in the saved settings")
	}
	if refs() != remoteBefore {
		t.Fatal("local deletion changed upstream refs")
	}
}

func TestRepositorySidebarDeleteInsideWorkspace(t *testing.T) {
	m := testMainWindow(t)
	root := t.TempDir()
	dir := filepath.Join(root, "workspace")
	alpha, beta := filepath.Join(dir, "alpha"), filepath.Join(dir, "beta")
	gitInit(t, alpha, "Alpha", "")
	gitInit(t, beta, "Beta", "")
	ws := &workspace{dir: dir}
	for _, path := range []string{alpha, beta} {
		repo, err := git.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		a := newApp(repo)
		a.ws = ws
		ws.apps = append(ws.apps, a)
	}
	workspaces[dir] = ws
	a, b := ws.apps[0], ws.apps[1]
	b.message = "Retain this draft"
	m.activate(a)
	prefs.LocalRepos = []string{dir, alpha}
	prefs.RepoSidebarHidden = false
	prefs.path = filepath.Join(root, "settings.json")
	view := ui.NewTester(m.view, 1200, 800)
	queue := make(chan func(), 1)
	welcomeWin.updFn = func(fn func()) { queue <- fn }
	oldTrash := trash
	t.Cleanup(func() { trash = oldTrash })
	trashCalls := 0
	trash = func(path string) error {
		trashCalls++
		return os.Rename(path, filepath.Join(root, "trashed"))
	}
	chooseDelete := func(name string) {
		t.Helper()
		if err := view.RightClick(name); err != nil {
			t.Fatal(err)
		}
		if err := view.ChooseMenuItem(loc.DeleteLocalRepository()); err != nil {
			t.Fatal(err)
		}
		confirmSidebarDeletion(t, view, queue)
	}
	chooseDelete(filepath.Base(dir))
	if trashCalls != 0 || !view.HasText(loc.DeleteLocalRepositoryFailed()) || !exists(alpha) || !exists(beta) {
		t.Fatal("a folder grouping repositories must remain on disk")
	}
	if err := view.Click(loc.Close()); err != nil {
		t.Fatal(err)
	}
	a.busy = loc.OpPull()
	view.Frame()
	chooseDelete(filepath.Base(alpha))
	if trashCalls != 0 || !view.HasText(loc.DeleteLocalRepositoryBusy()) || m.current() != a {
		t.Fatal("deletion did not wait for the running operation")
	}
	if err := view.Click(loc.Close()); err != nil {
		t.Fatal(err)
	}
	a.busy = ""
	view.Frame()
	chooseDelete(filepath.Base(alpha))
	if trashCalls != 1 || exists(alpha) || !exists(beta) || m.current() != b || b.message != "Retain this draft" || !slices.Equal(ws.apps, []*app{b}) {
		t.Fatal("deletion did not preserve the other repository and its draft in the workspace")
	}
}

func confirmSidebarDeletion(t *testing.T, view *ui.Tester, queue <-chan func()) {
	t.Helper()
	if err := view.Click(loc.TrashButton()); err != nil {
		t.Fatal(err)
	}
	select {
	case fn := <-queue:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("repository deletion did not complete")
	}
	for range 3 {
		view.Frame()
	}
}

func TestRepositorySidebarRemove(t *testing.T) {
	m := testMainWindow(t)
	root := t.TempDir()
	alpha, missing := filepath.Join(root, "alpha"), filepath.Join(root, "missing")
	if err := os.Mkdir(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(alpha, "notes.txt")
	if err := os.WriteFile(file, []byte("keep this file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prefs.LocalRepos = []string{alpha, missing}
	prefs.PinnedRepos = map[string]bool{alpha: true, missing: true}
	prefs.Recent = []string{alpha, missing}
	prefs.path = filepath.Join(root, "settings.json")
	current := newApp(&git.Repo{Dir: alpha})
	current.message = "Keep this draft"
	apps[alpha] = current
	m.activate(current)
	view := ui.NewTester(func(c *ui.Context) { repositorySidebar(c, alpha) }, 204, 400)

	// Missing folders can be removed, as can the active and last entry.
	for _, dir := range []string{missing, alpha} {
		name := filepath.Base(dir)
		if err := view.RightClick(name); err != nil {
			t.Fatal(err)
		}
		if err := view.ChooseMenuItem(loc.RemoveRepository()); err != nil {
			t.Fatalf("%v; menu %q", err, view.Menu())
		}
		for range 3 {
			view.Frame()
		}
		if view.HasText(name) || slices.Contains(prefs.LocalRepos, dir) || prefs.PinnedRepos[dir] {
			t.Fatalf("removed repository remains in the sidebar: %s", dir)
		}
		if m.current() != current || current.message != "Keep this draft" {
			t.Fatal("removing a sidebar entry changed the active repository or its draft")
		}
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep this file\n" {
		t.Fatalf("removing a sidebar entry changed its files: %q, %v", data, err)
	}
	data, err = os.ReadFile(prefs.path)
	if err != nil {
		t.Fatal(err)
	}
	restored := &settings{path: prefs.path}
	restored.read(data)
	*prefs = *restored
	loadLocalRepositories()
	view = ui.NewTester(func(c *ui.Context) { repositorySidebar(c, alpha) }, 204, 400)
	if prefs.LocalRepos == nil || len(prefs.LocalRepos) != 0 || len(prefs.PinnedRepos) != 0 || view.HasText("alpha") || view.HasText("missing") {
		t.Fatal("removed repositories returned after reloading settings")
	}
}
