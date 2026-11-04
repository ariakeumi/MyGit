package main

import (
	"context"
	"slices"
	"testing"

	"mygit/internal/github"
	loc "mygit/locales"
)

func TestGitHubAccountSignIn(t *testing.T) {
	tt := newTester(t)
	originalUpdate, originalRead, originalBrowser := gitHubAccountUpdate, readGitHubAccount, browserGitHubLogin
	originalAccount, originalVersion := currentGitHubAccount, gitHubAccountVersion
	t.Cleanup(func() {
		gitHubAccountUpdate, readGitHubAccount, browserGitHubLogin = originalUpdate, originalRead, originalBrowser
		currentGitHubAccount, gitHubAccountVersion = originalAccount, originalVersion
	})
	gitHubAccountUpdate = func(fn func()) { tt.queue <- fn }
	readGitHubAccount = func(context.Context) (github.Account, error) { return github.Account{}, nil }
	browserGitHubLogin = func(context.Context, func(string)) (github.Account, error) {
		return github.Account{}, github.ErrNoCLI
	}

	tt.click(t, loc.GithubSignIn())
	tt.click(t, loc.GithubBrowserSignIn())
	if !tt.HasText(loc.GithubCliMissing()) || !tt.HasText(loc.GithubToken()) {
		t.Fatalf("expected the token fallback: %q", tt.Texts())
	}

	finish := make(chan struct{})
	browserGitHubLogin = func(ctx context.Context, code func(string)) (github.Account, error) {
		code("AB12-CD34")
		select {
		case <-finish:
			return github.Account{Login: "ada"}, nil
		case <-ctx.Done():
			return github.Account{}, ctx.Err()
		}
	}
	if err := tt.Click(loc.GithubBrowserSignIn()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tt.a.githubLogin.close)
	select {
	case fn := <-tt.queue:
		fn()
		tt.Frame()
	default:
		tt.settle()
	}
	tt.click(t, loc.GithubCopyCode())
	tt.click(t, loc.GithubOpenBrowser())
	if tt.Clipboard() != "AB12-CD34" || !slices.Contains(tt.OpenedURLs(), githubDevicePage) {
		t.Fatal("the dialog did not copy the code or open GitHub")
	}
	close(finish)
	tt.settle()
	if !tt.HasText(loc.GithubSignedIn("ada")) || !tt.HasText("GitHub · @ada") {
		t.Fatalf("the signed-in account did not appear: %q", tt.Texts())
	}
}
