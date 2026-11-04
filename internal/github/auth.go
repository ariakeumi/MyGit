package github

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
)

const tokenKey = "github-token"

var ErrNoCLI = errors.New("GitHub CLI not found")

// CLI looks up gh on PATH and in common locations for macOS apps started
// from Finder, which do not inherit the user's shell PATH.
func CLI() (string, error) {
	for _, name := range []string{"gh", "/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
		if name != "gh" && !macOS {
			break
		}
		if exe, err := exec.LookPath(name); err == nil {
			return exe, nil
		}
	}
	return "", ErrNoCLI
}

// Token returns the account configured in MyGit, the environment, or gh.
func Token(ctx context.Context) string {
	token, _ := tokenSource(ctx)
	return token
}

func tokenSource(ctx context.Context) (string, bool) {
	if value, err := mygo.Secrets.Get(tokenKey); err == nil && len(value) != 0 {
		return string(value), true
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(key)); token != "" {
			return token, false
		}
	}
	gh, err := CLI()
	if err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, gh, "auth", "token", "--hostname", "github.com")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), false
}

// Account is the verified GitHub identity and whether MyGit owns its token.
type Account struct {
	Login string
	Saved bool
}

func CurrentAccount(ctx context.Context) (Account, error) {
	token, saved := tokenSource(ctx)
	if token == "" {
		return Account{}, nil
	}
	user, err := viewer(ctx, token)
	return Account{Login: user.Login, Saved: saved}, err
}

// SaveToken verifies the token before replacing the one in the system
// credential store.
func SaveToken(ctx context.Context, token string) (Account, error) {
	user, err := viewer(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if err := mygo.Secrets.Set(tokenKey, []byte(token)); err != nil {
		return Account{}, err
	}
	return Account{Login: user.Login, Saved: true}, nil
}

func ForgetToken(ctx context.Context) (Account, error) {
	if err := mygo.Secrets.Delete(tokenKey); err != nil {
		return Account{}, err
	}
	// After removal, the existing environment or CLI account may take over.
	account, _ := CurrentAccount(ctx)
	return account, nil
}

func viewer(ctx context.Context, token string) (User, error) {
	var user User
	client := Client{Token: token}
	err := client.do(ctx, http.MethodGet, "/user", nil, &user)
	return user, err
}

var deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)

// SignInWithCLI uses gh's non-interactive browser authorization. gh
// prints the one-time code, polls GitHub, and stores the resulting token.
func SignInWithCLI(ctx context.Context, code func(string)) (Account, error) {
	gh, err := CLI()
	if err != nil {
		return Account{}, err
	}
	cmd := exec.CommandContext(ctx, gh, "auth", "login", "--hostname", "github.com", "--web", "--skip-ssh-key")
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "CLICOLOR_FORCE=0")
	output := &loginOutput{code: code}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return Account{}, err
	}
	account, err := CurrentAccount(ctx)
	if err == nil && account.Login == "" {
		err = ErrNoToken
	}
	return account, err
}

// Only publish the device code. CLI output may include other account
// information and should never be copied into app logs or UI state.
type loginOutput struct {
	pending []byte
	code    func(string)
}

func (w *loginOutput) Write(p []byte) (int, error) {
	if w.code == nil {
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	for {
		line, rest, ok := bytes.Cut(w.pending, []byte("\n"))
		if !ok {
			break
		}
		w.pending = rest
		if bytes.Contains(bytes.ToLower(line), []byte("one-time code")) {
			if code := deviceCode.Find(line); code != nil {
				w.code(string(code))
				w.code, w.pending = nil, nil
				break
			}
		}
	}
	if len(w.pending) > 4096 {
		w.pending = nil
	}
	return len(p), nil
}
