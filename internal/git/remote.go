package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// Remote is a remote repository and where it is.
type Remote struct {
	Name, URL string
}

// Remotes lists the remotes, with their fetch URLs.
func (r *Repo) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := r.Git(ctx, "remote", "-v")
	if err != nil {
		return nil, err
	}
	var list []Remote
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 3 && f[2] == "(fetch)" {
			list = append(list, Remote{f[0], f[1]})
		}
	}
	return list, nil
}

func (r *Repo) AddRemote(ctx context.Context, name, url string) error {
	_, err := r.Git(ctx, "remote", "add", name, url)
	return err
}

func (r *Repo) RemoveRemote(ctx context.Context, name string) error {
	_, err := r.Git(ctx, "remote", "remove", name)
	return err
}

func (r *Repo) RenameRemote(ctx context.Context, from, to string) error {
	_, err := r.Git(ctx, "remote", "rename", from, to)
	return err
}

func (r *Repo) SetRemoteURL(ctx context.Context, name, url string) error {
	_, err := r.Git(ctx, "remote", "set-url", name, url)
	return err
}

// RewriteRemoteURLs replaces matching URLs configured for the remote.
// The callback returns an empty string for URLs that should be left alone.
func (r *Repo) RewriteRemoteURLs(ctx context.Context, name string, rewrite func(string) string) error {
	out, err := r.Git(ctx, "config", "--local", "--null", "--list")
	if err != nil {
		return err
	}
	fetchKey, pushKey := "remote."+name+".url", "remote."+name+".pushurl"
	counts := map[string]int{}
	for entry := range strings.SplitSeq(out, "\x00") {
		counts[entry]++
	}
	var updatedFetch bool
	var errs []error
	for entry := range strings.SplitSeq(out, "\x00") {
		count := counts[entry]
		delete(counts, entry)
		key, old, _ := strings.Cut(entry, "\n")
		if count == 0 || (key != fetchKey && key != pushKey) {
			continue
		}
		next := rewrite(old)
		if next == "" {
			continue
		}
		// Replacing duplicate values with git config drops all but the last
		// occurrence, which can change the remote's primary fetch URL.
		if count > 1 {
			errs = append(errs, fmt.Errorf("duplicate URL entries for %s", key))
			continue
		}
		args := []string{"remote", "set-url"}
		if key == pushKey {
			args = append(args, "--push")
		}
		if _, err := r.Git(ctx, append(args, "--", name, next, "^"+regexp.QuoteMeta(old)+"$")...); err != nil {
			errs = append(errs, err)
		} else if key == fetchKey {
			updatedFetch = true
		}
	}
	if !updatedFetch && len(errs) == 0 {
		return errors.New("no matching fetch URL in local Git configuration")
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for _, push := range []bool{false, true} {
		args := []string{"remote", "get-url", "--all"}
		if push {
			args = append(args, "--push")
		}
		urls, err := r.Git(ctx, append(args, "--", name)...)
		if err != nil {
			return err
		}
		for old := range strings.SplitSeq(urls, "\n") {
			if next := rewrite(old); next != "" && next != old {
				return errors.New("a configured URL still points to the old repository")
			}
		}
	}
	return nil
}

// FetchRemote updates one remote's branches.
func (r *Repo) FetchRemote(ctx context.Context, name string) error {
	_, err := r.Git(ctx, "fetch", "--prune", name)
	return err
}

// RepoName is the folder a clone of url makes, as git names it.
func RepoName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return path.Base(url)
}

// Clone clones url into dir, a folder in parent, telling progress the
// lines git reports as it goes.
func Clone(ctx context.Context, parent, url, dir string, progress func(string)) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("repository URL is required")
	}
	if dir == "" || dir == "." || dir == ".." || strings.ContainsAny(dir, "/\\:\x00\r\n") {
		return errors.New("folder name must be a single directory name")
	}
	pipe, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := stream(ctx, Local, parent, io.Discard, w, "clone", "--progress", "--", url, dir)
		w.Close()
		done <- err
	}()
	var stderr bytes.Buffer
	sc := bufio.NewScanner(pipe)
	sc.Split(scanProgress)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		stderr.WriteString(line + "\n")
		if line != "" && progress != nil {
			progress(line)
		}
	}
	_, _ = io.Copy(io.Discard, pipe)
	if err := <-done; err != nil {
		return lastError(stderr.String(), err)
	}
	return nil
}

// scanProgress splits git's progress at \r as well as \n, as it rewrites
// a line in place.
func scanProgress(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// lastError is the fatal line of git's output, or err.
func lastError(out string, err error) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "fatal:") || strings.HasPrefix(lines[i], "error:") {
			return textErr(lines[i])
		}
	}
	return err
}

type textErr string

func (e textErr) Error() string { return string(e) }

// Init makes a repository in dir, with main as its first branch.
func Init(ctx context.Context, dir string) error {
	_, err := git(ctx, Local, dir, "", nil, "init", "-b", "main")
	return err
}
