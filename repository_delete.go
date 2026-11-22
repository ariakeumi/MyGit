package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"

	"mygit/internal/git"
	loc "mygit/locales"
)

type repositoryDeletion struct {
	dir     string
	confirm bool
	running bool
	err     string
}

func (m *mainWindow) repositoryDeleteView(c *ui.Context) {
	d := &m.repoDelete
	if d.err != "" {
		open := true
		ui.AlertDialog(c, &open, loc.DeleteLocalRepositoryFailed(), d.err, loc.Close())
		if !open {
			*d = repositoryDeletion{}
		}
		return
	}
	if d.confirm && ui.AlertDialog(c, &d.confirm, loc.DeleteLocalRepositoryTitle(), loc.DeleteLocalRepositoryMessage(d.dir), loc.Cancel(), loc.TrashButton()) == 1 {
		m.deleteLocalRepository(d.dir)
	}
}

// Check the folder itself: opening a subfolder can find a parent repository,
// and a sidebar entry can also point to a folder that groups repositories.
func repositoryDeleteTarget(dir string) (string, os.FileInfo, error) {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) || filepath.Dir(dir) == dir {
		return "", nil, errors.New(loc.GitNotRepo(dir))
	}
	target, err := os.Lstat(dir)
	if err != nil {
		return "", nil, err
	}
	if !target.IsDir() {
		return "", nil, errors.New(loc.GitNotRepo(dir))
	}
	repo, err := git.Open(dir)
	if err != nil {
		return "", nil, err
	}
	root, err := os.Stat(repo.Dir)
	if err != nil {
		return "", nil, err
	}
	if !os.SameFile(target, root) {
		return "", nil, errors.New(loc.GitNotRepo(dir))
	}
	return repo.Dir, target, nil
}

func (m *mainWindow) deleteLocalRepository(dir string) {
	dir = filepath.Clean(dir)
	m.repoDelete.running = true
	update := welcomeWin.update
	go func() {
		root, target, err := repositoryDeleteTarget(dir)
		update(func() {
			if mainWin != m {
				return
			}
			if err == nil {
				err = m.trashLocalRepository(dir, root, target)
			}
			m.repoDelete = repositoryDeletion{}
			if err != nil {
				m.repoDelete.err = err.Error()
			}
		})
	}()
}

func (m *mainWindow) trashLocalRepository(dir, root string, target os.FileInfo) error {
	inFolder := func(parent, path string) bool {
		rel, err := filepath.Rel(parent, path)
		return err == nil && filepath.IsLocal(rel)
	}
	removed := func(path string) bool { return inFolder(root, path) || inFolder(dir, path) }
	for _, a := range allApps() {
		if a.local() && removed(a.repo.Dir) && (a.busy != "" || a.unsaved()) {
			return errors.New(loc.DeleteLocalRepositoryBusy())
		}
	}
	if clone := &welcomeWin.clone; clone.running && removed(clone.parent) {
		return errors.New(loc.DeleteLocalRepositoryBusy())
	}
	// Shell.TrashItem runs on the native main thread. Check the folder's
	// identity again before moving it, in case it changed during validation.
	current, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !os.SameFile(current, target) {
		return errors.New(loc.DeleteLocalRepositoryChanged())
	}
	if err := trash(dir); err != nil {
		return err
	}
	m.forgetDeletedRepositories(removed)
	return nil
}

func (m *mainWindow) forgetDeletedRepositories(removed func(string) bool) {
	current := m.current()
	next := current
	if current != nil && current.local() && removed(current.repo.Dir) {
		next = nil
		prefs.SidebarW, prefs.FilesW, prefs.StagedPct = current.sidebarW, current.filesW, current.stagedPct
	}
	for key, a := range apps {
		if a.local() && removed(a.repo.Dir) {
			a.stop()
			delete(apps, key)
		}
	}
	for key, ws := range workspaces {
		if ws.host != "" {
			continue
		}
		active := ws.apps[ws.active]
		ws.apps = slices.DeleteFunc(ws.apps, func(a *app) bool {
			if !removed(a.repo.Dir) {
				return false
			}
			a.stop()
			delete(ws.closed, a.repo.Dir)
			return true
		})
		if len(ws.apps) == 0 {
			delete(workspaces, key)
			continue
		}
		if i := slices.Index(ws.apps, active); i >= 0 {
			ws.active = i
		} else {
			ws.active = 0
			for i := range ws.apps {
				if !ws.hidden(i) {
					ws.active = i
					break
				}
			}
			if current == active {
				next = ws.apps[ws.active]
				next.sidebarW, next.filesW, next.filesH, next.stagedPct = active.sidebarW, active.filesW, active.filesH, active.stagedPct
			}
		}
	}
	prefs.LocalRepos = slices.DeleteFunc(prefs.LocalRepos, removed)
	prefs.Recent = slices.DeleteFunc(prefs.Recent, removed)
	for dir := range prefs.PinnedRepos {
		if removed(dir) {
			delete(prefs.PinnedRepos, dir)
		}
	}
	for dir := range prefs.HiddenRepos {
		if removed(dir) {
			delete(prefs.HiddenRepos, dir)
		}
	}
	prefs.save()
	welcomeWin.recent = slices.DeleteFunc(welcomeWin.recent, removed)
	m.active = current
	if next != current {
		m.active = nil
		if next == nil {
			showWelcome()
		} else {
			m.activate(next)
		}
	}
}
