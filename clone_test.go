package main

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygit/internal/github"
	loc "mygit/locales"
)

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
