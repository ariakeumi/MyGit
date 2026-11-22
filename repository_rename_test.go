package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygit/internal/github"
	loc "mygit/locales"
)

func TestRenameGitHubRepository(t *testing.T) {
	tt := newTester(t)
	t.Cleanup(tt.a.stop)
	dir := tt.a.repo.Dir
	gitIn(t, dir, "remote", "add", "origin", "https://github.com/ada/old.git")
	gitIn(t, dir, "config", "--add", "remote.origin.pushurl", "git@github.com:ADA/Old.git")
	gitIn(t, dir, "config", "--add", "remote.origin.pushurl", "git@github.com:mirror/old.git")
	gitIn(t, dir, "remote", "add", "upstream", "https://github.com/team/old.git")

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPatch || r.Header.Get("Authorization") != "Bearer tok" || err != nil {
			t.Errorf("request %s %s, body %v, decode error %v", r.Method, r.URL.Path, body, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests.Add(1)
		switch {
		case r.URL.Path == "/repos/ada/old" && body["name"] == "new-project":
			fmt.Fprint(w, `{"name":"new-project"}`)
		case r.URL.Path == "/repos/ada/new-project" && body["name"] == "denied":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Permission denied"}`)
		case r.URL.Path == "/repos/ada/new-project" && body["name"] == "partial":
			fmt.Fprint(w, `{"name":"partial"}`)
		default:
			t.Errorf("unexpected request %s %v", r.URL.Path, body)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	oldClient := newGitHub
	newGitHub = func(_ context.Context, owner, name string) *github.Client {
		return &github.Client{Owner: owner, Repo: name, Token: "tok", BaseURL: srv.URL}
	}
	defer func() { newGitHub = oldClient }()
	tt.a.refresh()
	tt.settle()

	open := func(name string) {
		t.Helper()
		tt.click(t, loc.Repository())
		if err := tt.ChooseMenuItem(loc.RenameGithubRepo(), "origin · ada/"+name+"…"); err != nil {
			t.Fatalf("%v; menu %q", err, tt.Menu())
		}
		tt.Frame()
	}
	open("old")
	if !tt.HasText(loc.RenameGithubRepoTarget("ada/old", "origin")) {
		t.Fatalf("wrong rename target: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("wrong/name")
	tt.Frame()
	if !tt.HasText(loc.RenameGithubRepoInvalid()) {
		t.Fatalf("no validation message: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("new-project")
	tt.click(t, loc.Rename())

	if requests.Load() != 1 || !tt.HasText(loc.RenameGithubRepoSuccess("ada/new-project")) {
		t.Fatalf("rename result: %d requests, notice %q, error %q", requests.Load(), tt.a.notice, tt.a.errMsg)
	}
	open("new-project")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("denied")
	tt.click(t, loc.Rename())
	if requests.Load() != 2 || !tt.HasText(loc.OpFailed(loc.RenameGithubRepo(),
		loc.RenameGithubRepoPermission("GitHub: Permission denied (403)"))) {
		t.Fatalf("rename rejection: %d requests, notice %q, error %q", requests.Load(), tt.a.notice, tt.a.errMsg)
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"remote", "get-url", "origin"}, "https://github.com/ada/new-project.git"},
		{[]string{"remote", "get-url", "--push", "--all", "origin"}, "git@github.com:ADA/new-project.git\ngit@github.com:mirror/old.git"},
		{[]string{"remote", "get-url", "upstream"}, "https://github.com/team/old.git"},
	} {
		if got := gitIn(t, dir, check.args...); got != check.want {
			t.Errorf("git %v = %q, want %q", check.args, got, check.want)
		}
	}

	const current = "https://github.com/ada/new-project.git"
	gitIn(t, dir, "config", "--add", "remote.origin.url", "https://github.com/ada/other.git")
	gitIn(t, dir, "config", "--add", "remote.origin.url", current)
	open("new-project")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("partial")
	tt.click(t, loc.Rename())
	if requests.Load() != 3 ||
		!tt.HasText(loc.RenameGithubRepoRemoteFailed("ada/partial", "origin", "duplicate URL entries for remote.origin.url")) {
		t.Fatalf("partial rename: %d requests, notice %q, error %q", requests.Load(), tt.a.notice, tt.a.errMsg)
	}
	if got := gitIn(t, dir, "remote", "get-url", "origin"); got != current {
		t.Errorf("primary fetch URL changed to %q, want %q", got, current)
	}
}
