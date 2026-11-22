// MyGit is a light git client: the history with its graph, the working
// tree's changes to stage and commit, and the diffs of both.
//
//	mygit [dir]   open the repository dir is in, the current folder's by default
//
// A running MyGit opens the repositories later launches name in its
// single main window.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/egoist/mygo"

	"mygit/internal/askpass"
	"mygit/internal/git"
	"mygit/internal/sysproxy"
	loc "mygit/locales"
)

// apps retains standalone repository models by working tree.
var apps = map[string]*app{}

// ready tells whether the app can show its window, and pending holds the
// folders the system asked to open before it could.
var (
	ready   bool
	pending []string
)

func main() {
	// Run by git for a prompt, hand it to the running app.
	if code, ok := askpass.Main(os.Args); ok {
		os.Exit(code)
	}
	mygo.App.SetName("MyGit")
	sysproxy.Install() // download the SSH helpers through the system's proxy
	loadSettings()
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running MyGit opens it
	}
	mygo.App.OnSecondInstance(func(args []string, cwd string) { openArg(args, cwd) })
	// Folders dropped on the icon, Open Recent, the Dock menu: those that
	// come as the app starts wait for it to be ready.
	mygo.App.OnOpenFile(func(path string) {
		if ready {
			openDir(path)
		} else {
			pending = append(pending, path)
		}
	})
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			openDefault("") // the Dock icon clicked with no window open
		}
	})
	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			// Use the original image instead of the framed legacy icon from macOS.
			if err := mygo.App.Dock.SetIcon(iconPNG); err != nil {
				log.Printf("set Dock icon: %v", err)
			}
		}
		mygo.App.SetMenu(appMenu())
		loadLocalRepositories()
		loadGitHubAccount()
		mygo.App.OnLocaleChanged(func(string) { mygo.App.SetMenu(appMenu()) }) // its labels, in the new language
		if s, err := askpass.Listen(askUser); err == nil {
			askServer = s
			git.Env = func(dir string) []string { return s.Env(dir) }
			mygo.App.OnQuit(func() { s.Close() })
		}
		mygo.App.OnQuit(closeHosts)
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if mainWin != nil && mainWin.leaving(mygo.App.Quit) {
				e.PreventDefault()
			}
		})
		ready = true
		if prefs.Language != "" { // the language chosen in Settings
			mygo.App.SetLocale(prefs.Language)
		}
		if len(pending) > 0 { // started to open them
			for _, p := range pending {
				openDir(p)
			}
			return
		}
		cwd, _ := os.Getwd()
		openArg(os.Args[1:], cwd)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// openArg opens the repository named by a command line's arguments, or
// the repositories under the folder they name; with none, the repository
// of its working folder, then the latest available local repository or
// workspace. With no repository available, the main window opens empty.
func openArg(args []string, cwd string) {
	if len(args) == 0 {
		openDefault(cwd)
		return
	}
	if host, dir, ok := parseLocation(args[0]); ok && !exists(filepath.Join(cwd, args[0])) {
		showWelcome()
		welcomeWin.connect(host, dir)
		return
	}
	dir := args[0]
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	err := openLocal(dir)
	if err == nil {
		return
	}
	showWelcome()
	welcomeWin.failed(dir, err)
}

// openDefault resolves repositories off the UI thread, without showing an
// intermediate welcome window or automatically connecting to a remote host.
func openDefault(cwd string) {
	locations := slices.Concat(prefs.Recent, prefs.LocalRepos, recentRepos())
	go func() {
		dir, repos := defaultRepositories(cwd, locations)
		mygo.RunOnMain(func() {
			if len(repos) > 0 {
				show("", dir, repos)
			} else if len(apps) == 0 && len(workspaces) == 0 {
				showWelcome()
			}
		})
	}()
}

func defaultRepositories(cwd string, locations []string) (string, []*git.Repo) {
	if cwd != "" {
		if repo, err := git.Open(cwd); err == nil {
			return repo.Dir, []*git.Repo{repo}
		}
	}
	seen := map[string]bool{}
	for _, dir := range locations {
		if !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		seen[dir] = true
		whole, repos, err := openFolders(context.Background(), git.Local, listLocal, dir)
		if err == nil && len(repos) > 0 {
			return whole, repos
		}
	}
	return "", nil
}

func openDir(dir string) {
	if err := openLocal(dir); err != nil {
		showWelcome()
		welcomeWin.failed(dir, err)
	}
}

// open selects a repository in the main window, reusing its retained model.
func open(repo *git.Repo) {
	m := ensureMainWindow()
	key := repo.Location()
	remember(repo.Host(), repo.Dir)
	a := apps[key]
	if a == nil {
		a = newApp(repo)
		a.win = m.win
		apps[key] = a
		a.start()
	}
	m.activate(a)
}

// pickAndOpen asks for a repository's folder and opens it. The dialogs
// wait for their answer: call it on a goroutine.
func pickAndOpen() {
	for {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: loc.OpenRepoDialog(), Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		whole, repos, err := openFolders(context.Background(), git.Local, listLocal, paths[0])
		if err == nil {
			mygo.RunOnMain(func() { show("", whole, repos) })
			return
		}
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Title: loc.CantOpenFolder(), Message: err.Error()})
	}
}

func appMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		// macOS' app menu includes settings even with no repository open.
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Label: loc.SettingsMenu(), Accelerator: "CmdOrCtrl+,", Click: func(_ *mygo.MenuItem, w *mygo.Window) { openSettings(w) }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: loc.MenuFile(), Submenu: []*mygo.MenuItem{
			{Label: loc.MenuOpenRepo(), Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { go pickAndOpen() }},
			{Label: loc.CloneMenu(), Accelerator: "CmdOrCtrl+Shift+O", Click: func(*mygo.MenuItem, *mygo.Window) { openClone() }},
			{Label: loc.SshMenu(), Click: func(*mygo.MenuItem, *mygo.Window) { openSSH() }},
			{Role: mygo.RoleOpenRecent, Label: loc.MenuRecent()},
			mygo.Separator(),
			{Label: loc.MenuInstallCLI(), Click: func(*mygo.MenuItem, *mygo.Window) { go installLauncher() }},
			{Label: loc.MenuUninstallCLI(), Click: func(*mygo.MenuItem, *mygo.Window) { go uninstallLauncher() }},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu, Label: loc.MenuEdit()},
		{Role: mygo.RoleWindowMenu, Label: loc.MenuWindow()},
	})
}

// openSettings opens settings for the content visible in the main window.
func openSettings(_ *mygo.Window) {
	m := ensureMainWindow()
	if a := m.current(); a != nil {
		a.settings.open = true
	} else {
		welcomeWin.settings.open = true
	}
	m.win.Invalidate()
}

// exists tells whether there is a file or a folder at p.
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// askUser asks a prompt of git's in the window of the repository it runs
// in, host:dir on another computer, or the welcome window, cloning or
// connecting, and waits for the answer.
func askUser(dir, prompt string) (string, bool) {
	done := make(chan credAnswer, 1)
	mygo.RunOnMain(func() {
		askUserInWindow(dir, prompt, done)
	})
	r := <-done
	return r.value, r.ok
}

func askUserInWindow(dir, prompt string, done chan credAnswer) {
	switch {
	case welcomeWin != nil && welcomeWin.clone.running && filepath.Clean(dir) == filepath.Clean(welcomeWin.clone.parent):
		// Clone runs in its parent folder, which may itself be an open repository.
		welcomeWin.cred.ask(welcomeWin.win, prompt, done)
	case appAt(dir) != nil:
		a := appAt(dir)
		mainWin.activate(a)
		a.askCredential(prompt, done)
	case welcomeWin != nil:
		showWelcome()
		welcomeWin.cred.ask(welcomeWin.win, prompt, done)
	default:
		done <- credAnswer{}
	}
}
