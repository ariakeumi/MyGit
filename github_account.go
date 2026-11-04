package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygit/internal/github"
	loc "mygit/locales"
)

const githubDevicePage = "https://github.com/login/device"

var (
	currentGitHubAccount github.Account
	gitHubAccountTask    *githubAccountDialog
	gitHubAccountVersion int

	// The dispatcher and API calls can be substituted in UI tests.
	gitHubAccountUpdate = mygo.RunOnMain
	readGitHubAccount   = github.CurrentAccount
	saveGitHubToken     = github.SaveToken
	browserGitHubLogin  = github.SignInWithCLI
)

type githubAccountDialog struct {
	open   bool
	token  string
	code   string
	err    string
	cancel context.CancelFunc
}

func loadGitHubAccount() {
	if gitHubAccountTask != nil {
		return
	}
	gitHubAccountVersion++
	version := gitHubAccountVersion
	update, read := gitHubAccountUpdate, readGitHubAccount
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		account, _ := read(ctx)
		update(func() {
			if version == gitHubAccountVersion {
				currentGitHubAccount = account
				redrawGitHubAccount()
			}
		})
	}()
}

func redrawGitHubAccount() {
	for _, a := range allApps() {
		if a.win != nil {
			a.win.Invalidate()
		}
	}
	if welcomeWin != nil {
		welcomeWin.win.Invalidate()
	}
}

func (p *githubAccountDialog) button(c *ui.Context) {
	label := loc.GithubSignIn()
	if name := currentGitHubAccount.Login; name != "" {
		label = "GitHub · @" + name
	}
	ui.Button(c.Key("github-account"), label).MaxWidth(218).Tooltip(label).OnClick(func() {
		p.open, p.err = true, ""
		loadGitHubAccount()
	})
}

func (p *githubAccountDialog) close() {
	p.open = false
	p.token, p.code = "", ""
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *githubAccountDialog) run(timeout time.Duration, fallback string, job func(context.Context) (github.Account, error)) {
	if gitHubAccountTask != nil {
		return
	}
	gitHubAccountVersion++
	gitHubAccountTask = p
	p.err, p.code = "", ""
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	p.cancel = cancel
	update := gitHubAccountUpdate
	redrawGitHubAccount()
	go func() {
		defer cancel()
		account, err := job(ctx)
		canceled := errors.Is(ctx.Err(), context.Canceled)
		update(func() {
			p.cancel = nil
			gitHubAccountTask = nil
			p.code = ""
			switch {
			case err == nil:
				currentGitHubAccount = account
			case canceled:
			case errors.Is(err, github.ErrNoCLI):
				p.err = loc.GithubCliMissing()
			default:
				p.err = fallback
			}
			redrawGitHubAccount()
		})
	}()
}

func (p *githubAccountDialog) browser() {
	login, update := browserGitHubLogin, gitHubAccountUpdate
	p.run(15*time.Minute, loc.GithubBrowserFailed(), func(ctx context.Context) (github.Account, error) {
		return login(ctx, func(code string) {
			update(func() {
				if ctx.Err() == nil && p.open {
					p.code = code
					redrawGitHubAccount()
				}
			})
		})
	})
}

func (p *githubAccountDialog) view(c *ui.Context) {
	if !p.open {
		return
	}
	t := c.Theme()
	services := c.Services()
	busy := gitHubAccountTask != nil
	if ui.Modal(c.Key("github-sign-in"), &p.open, func() {
		ui.Column(c).Width(dialogWidth(c, 430)).Gap(10).Children(func() {
			ui.Text(c, loc.GithubAccount()).FontSize(16).Bold()
			account := currentGitHubAccount
			switch {
			case account.Login != "":
				ui.Text(c, loc.GithubSignedIn(account.Login)).Wrap()
				if !account.Saved {
					ui.Text(c, loc.GithubExternalAccount()).TextColor(t.TextMuted).Wrap()
				}
			case p.code != "":
				code := p.code
				ui.Text(c, loc.GithubDeviceHint()).TextColor(t.TextMuted).Wrap()
				ui.Text(c, code).Font("monospace").FontSize(20).Bold()
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c, loc.GithubCopyCode()).OnClick(func() { services.WriteClipboard(code) })
					ui.PrimaryButton(c, loc.GithubOpenBrowser()).OnClick(func() { services.OpenURL(githubDevicePage) })
				})
			case !busy:
				ui.Text(c, loc.GithubBrowserHint()).TextColor(t.TextMuted).Wrap()
				ui.PrimaryButton(c, loc.GithubBrowserSignIn()).OnClick(p.browser)
				ui.Text(c, loc.GithubTokenHint()).TextColor(t.TextMuted).Wrap()
				ui.TextInput(c.Key("token"), &p.token).Label(loc.GithubToken()).Placeholder(loc.GithubToken()).Password()
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c, loc.GithubCreateToken()).OnClick(func() {
						services.OpenURL("https://github.com/settings/tokens")
					})
					ui.Button(c, loc.GithubUseToken()).Disabled(strings.TrimSpace(p.token) == "").OnClick(func() {
						token := strings.TrimSpace(p.token)
						p.token = ""
						save := saveGitHubToken
						p.run(30*time.Second, loc.GithubTokenFailed(), func(ctx context.Context) (github.Account, error) {
							return save(ctx, token)
						})
					})
				})
			}
			if busy {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, loc.GithubSigningIn()).TextColor(t.TextMuted).Wrap()
				})
			}
			if p.err != "" {
				ui.Text(c, p.err).TextColor(t.Danger).Wrap()
				if p.err == loc.GithubCliMissing() {
					ui.Button(c, loc.GithubInstallCLI()).OnClick(func() { services.OpenURL("https://cli.github.com/") })
				}
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if account.Saved {
					ui.Button(c, loc.GithubForgetToken()).Disabled(busy).OnClick(func() {
						p.run(30*time.Second, loc.GithubForgetFailed(), github.ForgetToken)
					})
				}
				ui.Button(c, loc.Close()).OnClick(p.close)
			})
		})
	}).Dismissed() {
		p.close()
	}
}
