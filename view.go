package main

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"mygit/internal/git"
	"mygit/internal/patch"
	"mygit/internal/remote"
	loc "mygit/locales"
)

const rowHeight = 26

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	root := ui.Row(c).Fill().FontSize(12).AlignItems(ui.Stretch)
	root.Children(func() {
		if !prefs.RepoSidebarHidden {
			a.repositorySidebar(c)
		}
		ui.Column(c.Key("repository-content")).Grow(1).Basis(0).MinWidth(0).Background(t.Background).Children(func() {
			a.repositoryContent(c)
		})
		if root.FileDragOver() {
			dropHint(c)
		}
	})
	if paths := root.DroppedFiles(); len(paths) > 0 {
		a.openDropped(paths)
	}
	a.rebaseView(c)
	a.rewordView(c)
	a.pushView(c)
	a.promptView(c)
	a.worktreeView(c)
	a.remotesView(c)
	a.settingsView(c)
	a.confirmView(c)
	a.cred.view(c)
	a.githubLogin.view(c)
	a.paletteView(c)
	a.composerView(c)
	a.pullsView(c)
	a.submitView(c)
	c.OnShortcut(ui.Cmd, ui.KeyR, a.refresh)
	c.OnShortcut(ui.Cmd, ui.KeyB, a.toggleSidebar)
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyB, toggleRepositorySidebar)
	c.OnShortcut(ui.Cmd, ui.Key1, func() { a.selectTab(changesTab) })
	c.OnShortcut(ui.Cmd, ui.Key2, func() { a.selectTab(historyTab) })
	c.OnShortcut(ui.Cmd, ui.Key3, func() { a.selectTab(stashTab) })
	c.OnShortcut(ui.Cmd, ui.KeyF, a.focusHistory)
	c.OnShortcut(ui.Cmd, ui.KeyP, a.openFiles)
	c.OnShortcut(ui.Cmd, ui.KeyK, a.openCommands)
	c.OnShortcut(0, ui.KeyF7, func() { a.stepChange(false) })
	c.OnShortcut(ui.Shift, ui.KeyF7, func() { a.stepChange(true) })
	if !macOS { // the menu bar's on macOS, which other systems' windows don't have
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyO, openSSH)
		c.OnShortcut(ui.Cmd, ui.KeyComma, func() { a.settings.open = true })
	}
}

func (a *app) repositoryContent(c *ui.Context) {
	t := c.Theme()
	a.toolbar(c)
	a.updateBar(c)
	if a.notice != "" {
		ui.Row(c).Padding(6, 12).Gap(8).Background(t.Accent.Alpha(0.1)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.notice).Wrap().Grow(1)
			if ui.Button(c, loc.Close()).Clicked() {
				a.notice = ""
			}
		})
	}
	if a.errMsg != "" {
		ui.Row(c).Padding(6, 12).Gap(8).Background(t.Danger.Alpha(0.12)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.errMsg).TextColor(t.Danger).Wrap().Grow(1)
			if a.trustDir != "" && ui.Button(c, loc.TrustOpen()).Tooltip(loc.TrustTooltip()).Clicked() {
				if err := git.Trust(a.trustDir); err != nil {
					a.errMsg = err.Error()
				} else {
					a.openDropped([]string{a.trustDir})
				}
			}
			if ui.Button(c, loc.Close()).Clicked() {
				a.errMsg, a.trustDir = "", ""
			}
		})
	}
	a.operationBar(c)
	if prefs.SidebarHidden {
		ui.Column(c).Grow(1).Basis(0).Children(func() { a.detailView(c) })
	} else {
		ui.Split(c.Key("repository-panes"), &a.sidebarW, func() { a.leftPanel(c) }, func() { a.detailView(c) }).Grow(1)
	}
}

// remoteBadge says the repository is on another computer, and which.
// hostName is how a host shows: WSL · Ubuntu for a distribution of WSL's,
// which is written wsl+Ubuntu, else as it is written.
func hostName(host string) string {
	if distro, ok := remote.WSL(host); ok {
		return "WSL · " + distro
	}
	return host
}

func (a *app) remoteBadge(c *ui.Context) {
	t := c.Theme()
	host, dir := a.repo.Host(), a.repo.Dir
	label, tip := "SSH · "+host, loc.RemoteBadge(host, dir)
	if distro, ok := remote.WSL(host); ok {
		label, tip = hostName(host), loc.RemoteBadgeWsl(distro, dir)
	}
	ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 8).Radius(10).Background(t.Accent.Alpha(0.16)).
		TextColor(t.AccentText).Tooltip(tip).Children(func() {
		ui.Icon(c, icons["server"]).Size(12, 12)
		ui.Text(c, label).FontSize(12).Bold().TextColor(t.AccentText).SingleLine()
	})
}

// toggleSidebar hides the sidebar, or shows it again, both the repository's
// and the workspace's, keeping the choice for the next launch.
func (a *app) toggleSidebar() {
	prefs.SidebarHidden = !prefs.SidebarHidden
	prefs.save()
}

// sidebarLabel says what the toggle does now, as its button and the
// commands name it.
func (a *app) sidebarLabel() string {
	if prefs.SidebarHidden {
		return loc.ShowSidebar()
	}
	return loc.HideSidebar()
}

// changesPanel keeps the commit controls visible below the file lists.
func (a *app) changesPanel(c *ui.Context) {
	ui.Column(c).Grow(1).Basis(0).Children(func() {
		if len(a.status.Changes) > 0 || a.filter.query != "" {
			a.filterField(c)
		}
		staged := func() { a.changeSection(c, loc.Staged(), true, true) }
		unstaged := func() { a.changeSection(c, loc.Changes(), false, true) }
		switch {
		case len(a.staged) == 0:
			ui.Column(c.Key("working-tree")).Grow(1).Basis(0).Children(unstaged)
		case len(a.unstaged) == 0:
			ui.Column(c.Key("index")).Grow(1).Basis(0).Children(staged)
		default:
			ui.SplitVerticalPercent(c.Key("changes"), &a.stagedPct, func() {
				ui.Column(c).Fill().Children(staged)
			}, func() {
				ui.Column(c).Fill().Children(unstaged)
			}).Grow(1).Basis(0)
		}
		a.commitForm(c)
	})
	c.OnShortcut(ui.Cmd, ui.KeyEnter, a.commit)
}

func (a *app) commitForm(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(8).Padding(10).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		a.messageFields(c, 3)
		if ui.Checkbox(c, &a.amend, loc.Amend()).Changed() {
			a.toggleAmend()
		}
		label := loc.Commit()
		if a.status.Branch != "" {
			label = loc.CommitToBranch(a.status.Branch)
		}
		tip := shortcut("↩")
		if n := a.commitFileCount(); n > 0 {
			tip = loc.CommitFiles(n) + " · " + tip
		}
		ui.PrimaryButton(c, label).FillWidth().Disabled(!a.canCommit()).Tooltip(tip).OnClick(a.commit)
	})
}

func (a *app) messageFields(c *ui.Context, lines int) {
	in := ui.TextInput(c.Key("summary"), &a.message).Placeholder(loc.CommitSummary()).Label(loc.CommitSummary()).
		FillWidth().OnTextKey(a.recallKey)
	if a.recallEnd {
		n := utf8.RuneCountInString(a.message)
		in.SetTextSelection(n, n)
		a.recallEnd = false
	}
	ui.TextArea(c.Key("description"), &a.description).Placeholder(loc.CommitDescription()).Label(loc.CommitDescription()).
		FillWidth().Lines(lines, lines)
}

// changeSection lists the staged changes or the others, those the filter
// shows, filling the room left when fill, else as tall as its rows, as a
// workspace's repositories stack them.
func (a *app) changeSection(c *ui.Context, title string, staged, fill bool) {
	t := c.Theme()
	changes := a.changes(staged)
	list, sel, row := &a.unstagedList, &a.unstagedSel, &a.unstagedRow
	if staged {
		list, sel, row = &a.stagedList, &a.stagedSel, &a.stagedRow
	}
	// The list chooses rows; the app, changes, by their indices. A tree's
	// rows are folders too, which are no change.
	key := map[bool]string{true: "staged", false: "changes"}[staged]
	rows := a.shownRows(staged)
	items := rows
	if items == nil {
		items = make([]int, len(changes))
		for i := range items {
			items[i] = i
		}
	}
	tree, n, at := a.listRows(key, items, func(i int) string { return changes[i].Path })
	if tree != nil {
		*row = treeRowOf(tree, *sel)
	} else {
		*row = rowOf(rows, *sel)
	}
	// A fixed height, so the header doesn't move as its button comes and
	// goes; the button stays while git runs, doing nothing then.
	head := ui.Row(c).Height(32).Padding(4, 8, 0, 10).Gap(6).AlignItems(ui.Center)
	if !staged && !fill && len(a.staged) > 0 {
		// A line between the staged changes above and these, as a split
		// draws one between them filled.
		head.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	}
	head.Children(func() {
		if !staged {
			a.workingSelectionHeader(c, changes, items)
		}
		ui.Text(c, title).Bold().TextColor(t.TextMuted)
		count := fmt.Sprint(len(changes))
		if rows != nil {
			count = fmt.Sprintf("%d/%d", len(items), len(changes))
		}
		ui.Badge(c, count)
		ui.Box(c).Grow(1)
		if len(items) > 0 {
			treeToggle(c)
			idle := a.busy == ""
			if staged {
				if iconButton(c, "minus", loc.UnstageAll()) && idle {
					a.unstageAll()
				}
			} else {
				if iconButton(c, "undo", loc.DiscardAll()) && idle {
					a.discardAll()
				}
				if iconButton(c, "plus", loc.StageAll()) && idle {
					a.stageAll()
				}
			}
		}
	})
	l := ui.List(c.Key(title), list, n, func(i int) {
		if at(i) < 0 {
			// A folder, with the actions of its files while the pointer
			// is over it, as VS Code's changes tree: to stage or unstage
			// what is in it, or to discard it.
			folder := tree[i].dir
			row := a.folderRow(c, key, tree[i])
			if row.Hovered() {
				row.Children(func() {
					if staged {
						if iconButton(c, "minus", loc.UnstageFolder()) {
							a.unstagePaths(folderPaths(changes, items, folder)...)
						}
						return
					}
					if iconButton(c, "undo", loc.DiscardFolder()) {
						if chosen := folderChanges(changes, items, folder); len(chosen) > 0 {
							a.discardFolder(folder, chosen)
						}
					}
					if iconButton(c, "plus", loc.StageFolder()) {
						a.stagePaths(folderPaths(changes, items, folder)...)
					}
				})
			}
			if !staged {
				row.ContextMenu(func(m *ui.Menu) {
					chosen := folderChanges(changes, items, folder)
					if len(chosen) > 0 && m.Item(loc.DiscardFolderMenu()).Chosen() {
						a.discardFolder(folder, chosen)
					}
				})
			}
			return
		}
		ch := changes[at(i)]
		chosen := i == *row
		// As VS Code's: the name, its folder, the actions while the
		// pointer is over the row, and the change's letter at the end;
		// in a tree, the name alone, in from its folder's. A check box
		// leads the row and shows whether it will be committed.
		indent := float32(8)
		if tree != nil {
			indent = 6 + float32(tree[i].depth)*treeIndent
		}
		row := ui.Row(c).Height(24).Padding(0, 8, 0, indent).Gap(6).AlignItems(ui.Center)
		if chosen {
			row.TextColor(t.AccentText)
		}
		row.Children(func() {
			label := loc.IncludeCommitFile(ch.Path)
			if staged {
				label = loc.UnstageFile(ch.Path)
			}
			checked := staged || a.includeWorkingChange(ch)
			enabled := a.busy == "" && (staged || a.canIncludeWorkingChange(ch))
			if changeCheckbox(c, checked, chosen, enabled, label) {
				a.toggleCommitFile(ch, staged)
			}
			code := ch.Work
			if staged {
				code = ch.Staged
			}
			name, dir := path.Base(ch.Path), path.Dir(ch.Path)
			nameText := ui.Text(c, name).SingleLine().Shrink(0)
			if !chosen && isGenerated(ch.Path, a.genMarked, nil, nil) {
				nameText.TextColor(t.TextMuted)
			}
			muted := t.TextMuted
			if chosen {
				muted = t.AccentText.Alpha(0.75)
			}
			if dir != "." && tree == nil {
				ui.Text(c, dir).FontSize(11).TextColor(muted).SingleLine().Grow(1).Basis(0)
			} else {
				ui.Box(c).Grow(1)
			}
			if row.Hovered() || chosen {
				if iconButton(c, "open", loc.OpenFile()) {
					a.openInEditor(a.repo.Path(ch.Path), 0)
				}
				if !staged && !ch.Conflict && iconButton(c, "undo", loc.DiscardChanges()) {
					a.discard(ch)
				}
				icon, label := "plus", loc.Stage()
				if staged {
					icon, label = "minus", loc.Unstage()
				}
				if iconButton(c, icon, label) {
					a.toggleStage(ch, staged)
				}
			}
			letter := string(code)
			if code == '?' {
				letter = "U" // untracked, as VS Code says it
			}
			ui.Text(c, letter).Font("monospace").FontSize(11).Bold().TextColor(changeColor(t, code, chosen)).
				Width(14).TextAlign(ui.Center).Tooltip(changeName(code))
		})
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(map[bool]string{true: loc.Unstage(), false: loc.Stage()}[staged]).Chosen() {
				a.toggleStage(ch, staged)
			}
			if !staged && !ch.Conflict && m.Item(loc.DiscardMenu()).Chosen() {
				a.discard(ch)
			}
			m.Separator()
			if ch.Work != '?' {
				a.fileMenu(m, ch.Path, "WORKTREE")
			} else {
				a.ignoreItems(m, ch.Path)
			}
		})
	})
	if fill {
		l.Grow(1).Basis(0)
	} else {
		l.Height(float32(n * 24))
	}
	folder := *row >= 0 && *row < n && at(*row) < 0
	if (l.Changed() || l.Submitted()) && folder {
		a.toggleFolder(key, tree[*row].dir)
	} else if l.Changed() && *row >= 0 && *row < n {
		// One change is chosen at a time, of both lists.
		a.chooseChange(at(*row), staged)
	} else if l.Submitted() && *row >= 0 && *row < n {
		a.toggleCommitFile(changes[at(*row)], staged)
	}
	// Space toggles inclusion in the commit. Delete asks to discard the change.
	chosen := func(fn func(git.Change)) func() {
		return func() {
			if *sel >= 0 && *sel < len(changes) && a.busy == "" {
				fn(changes[*sel])
			}
		}
	}
	list.Handle.OnShortcut(c, 0, ui.KeySpace, chosen(func(ch git.Change) { a.toggleCommitFile(ch, staged) }))
	if !staged {
		discard := chosen(func(ch git.Change) {
			if !ch.Conflict {
				a.discard(ch)
			}
		})
		list.Handle.OnShortcut(c, 0, ui.KeyBackspace, discard)
		list.Handle.OnShortcut(c, 0, ui.KeyDelete, discard)
	}
}

func (a *app) workingSelectionHeader(c *ui.Context, changes []git.Change, items []int) {
	total, included := 0, 0
	for _, i := range items {
		ch := changes[i]
		if a.canIncludeWorkingChange(ch) {
			total++
			if a.includeWorkingChange(ch) {
				included++
			}
		}
	}
	if total == 0 || !changeCheckbox(c.Key("include-working"), included == total, false, a.busy == "", loc.IncludeAllCommitFiles()) {
		return
	}
	for _, i := range items {
		if ch := changes[i]; a.canIncludeWorkingChange(ch) {
			a.includeInCommit(ch.Path, included != total)
		}
	}
}

func changeCheckbox(c *ui.Context, on, chosen, enabled bool, label string) bool {
	t := c.Theme()
	box := ui.CheckboxBase(c, &on).Size(14, 14).Radius(3).Center().Shrink(0).
		Label(label).Tooltip(label).Cursor(ui.CursorPointer).Disabled(!enabled)
	mark, ink := t.Accent, t.AccentText
	if chosen {
		mark, ink = t.AccentText, t.Accent
	}
	if on {
		box.Background(mark)
	} else {
		box.Border(1, t.Border.Mix(t.Text, 0.25))
		if box.Hovered() || chosen {
			box.BorderColor(mark)
		}
	}
	box.Children(func() {
		if on {
			ui.Icon(c, icons["check"]).Size(10, 10).TextColor(ink)
		}
	})
	return box.Changed()
}

func (a *app) toggleCommitFile(ch git.Change, staged bool) {
	if a.busy != "" {
		return
	}
	if staged {
		a.unstage(ch)
	} else if a.canIncludeWorkingChange(ch) {
		a.includeInCommit(ch.Path, !a.includeWorkingChange(ch))
	}
}

func (a *app) toggleStage(ch git.Change, staged bool) {
	if staged {
		a.unstage(ch)
	} else {
		a.stage(ch)
	}
}

// changeColor colors a change's letter as VS Code does, white on a
// chosen row's accent.
func changeColor(t *ui.Theme, code byte, chosen bool) ui.Color {
	if chosen {
		return t.AccentText
	}
	// VS Code's colors of its dark themes, and of its light ones.
	pick := func(dark, light ui.Color) ui.Color {
		if t.Dark {
			return dark
		}
		return light
	}
	switch code {
	case 'A', '?', 'R', 'C':
		return pick(ui.RGB(115, 201, 145), ui.RGB(88, 124, 12))
	case 'D':
		return pick(ui.RGB(199, 78, 57), ui.RGB(173, 7, 7))
	case 'U':
		return pick(ui.RGB(228, 103, 107), ui.RGB(173, 7, 7))
	}
	return pick(ui.RGB(226, 192, 141), ui.RGB(137, 85, 3)) // modified
}

func changeName(code byte) string {
	switch code {
	case 'M':
		return loc.StatusM()
	case 'A':
		return loc.StatusA()
	case 'D':
		return loc.StatusD()
	case 'R':
		return loc.StatusR()
	case 'C':
		return loc.StatusC()
	case '?':
		return loc.StatusU()
	case 'U':
		return loc.StatusConflict()
	case 'T':
		return loc.StatusT()
	}
	return ""
}

// detailView is the diff of what was chosen last: a commit, with its files
// beside, or a change.
func (a *app) detailView(c *ui.Context) {
	d := &a.detail
	ui.Column(c).Fill().Children(func() {
		switch {
		case d.commit != nil:
			if d.pr != nil {
				a.pullHeader(c, d.pr)
			} else {
				a.commitHeader(c, d.commit)
			}
			files, diff := func() { a.filesPane(c) }, func() { a.diffView(c) }
			if a.detailWidth(c) < 600 {
				ui.SplitVertical(c.Key("files-vertical"), &a.filesH, files, diff).Grow(1)
			} else {
				ui.Split(c.Key("files"), &a.filesW, files, diff).Grow(1)
			}
		case d.blame != nil:
			a.blameView(c, d.blame)
		case d.merge != nil:
			a.mergeView(c, d.merge)
		case d.change != nil && d.change.Conflict:
			ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(18, 18) })
		case d.change != nil:
			a.detailHeader(c, func() {
				side := loc.SideWorktree()
				if d.staged {
					side = loc.SideStaged()
				}
				ui.Text(c, d.change.Path).Bold().SingleLine().Grow(1).Basis(0).Tooltip(d.change.Path)
				ui.Badge(c, side)
			}, func() {
				a.changeNav(c)
				if d.whole && d.diff != nil && ui.Toggle(c, &prefs.FoldUnchanged, loc.FoldUnchanged()).Changed() {
					d.diff.ExpandUnchanged = !prefs.FoldUnchanged
					prefs.save()
				}
				if d.diff != nil {
					wrapToggle(c, d.diff)
				}
				a.whitespaceMenu(c)
				a.lineButtons(c)
			})
			a.diffView(c)
		default:
			a.emptyDetail(c)
		}
	})
}

// detailHeader keeps actions reachable when the sidebar leaves a narrow diff.
func (a *app) detailHeader(c *ui.Context, title, actions func()) {
	if a.detailWidth(c) < 720 {
		ui.Column(c).Padding(8, 12).Gap(5).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(title)
			ui.ScrollHorizontal(c).FillWidth().Children(func() {
				ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(actions)
			})
		})
		return
	}
	ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		title()
		actions()
	})
}

func (a *app) commitHeader(c *ui.Context, cm *git.Commit) {
	t := c.Theme()
	d := &a.detail
	ui.Column(c).Padding(8, 12).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, cm.Subject).Bold().SingleLine().Tooltip(cm.Subject)
		metadata := func() {
			if comparing(cm) {
				ui.Text(c, loc.FilesDiffer(len(d.files))).TextColor(t.TextMuted).SingleLine().Grow(1).Basis(0)
				return
			}
			ui.Text(c, cm.Short()).Font("monospace").TextColor(t.Accent).Tooltip(cm.Hash).Shrink(0)
			ui.Text(c, cm.Author).TextColor(t.TextMuted).SingleLine().Shrink(0)
			ui.Text(c, cm.Time.Format("2006-01-02 15:04")).TextColor(t.TextMuted).SingleLine().Grow(1).Basis(0)
		}
		actions := func() {
			if !comparing(cm) {
				a.changeNav(c)
			}
			a.whitespaceMenu(c)
			if d.diff != nil {
				wrapToggle(c, d.diff)
			}
		}
		parentPicker := func() {
			if n := len(cm.Parents); n > 1 && !comparing(cm) {
				labels := make([]string, n)
				for i, p := range cm.Parents {
					labels[i] = loc.ParentN(i+1, p[:8])
				}
				label := labels[d.parent]
				if ui.Select(c.Key("parent"), &label, labels).Label(loc.ParentLabel()).Changed() {
					a.showParent(slices.Index(labels, label))
				}
			}
		}
		if a.detailWidth(c) < 720 {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				metadata()
				parentPicker()
			})
			ui.ScrollHorizontal(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(actions)
		} else {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				metadata()
				actions()
				parentPicker()
			})
		}
		if body := strings.TrimSpace(strings.TrimPrefix(d.message, cm.Subject)); body != "" && !comparing(cm) {
			ui.Text(c, body).TextColor(t.TextMuted).Wrap().MaxLines(6)
		}
	})
}

// whitespaceMenu is a pop-up button over the diff, as IntelliJ's, naming
// what the diffs leave out of whitespace changes and choosing one of the
// modes.
func (a *app) whitespaceMenu(c *ui.Context) {
	b := ui.ButtonBase(c.Key("whitespace")).Padding(3, 6).Gap(2).Radius(5).AlignItems(ui.Center).Shrink(0).
		Tooltip(loc.WhitespaceChanges()).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(c.Theme().Text.Alpha(0.12))
	}
	chosen := prefs.whitespaceMode()
	b.Children(func() {
		ui.Text(c, chosen.label()).SingleLine()
		ui.Icon(c, icons["chevron-down"]).Size(12, 12)
	})
	b.Menu(func(m *ui.Menu) {
		for _, mode := range whitespaceModes {
			if m.Item(mode.label()).Checked(mode.key == chosen.key).Chosen() && mode.key != chosen.key {
				prefs.Whitespace = mode.key
				prefs.save()
				a.reloadDiff()
			}
		}
	})
}

// wrapToggle turns wrapping on or off for the diff shown, kept for the
// diffs opened next too.
func wrapToggle(c *ui.Context, d *editor.DiffState) {
	if ui.Toggle(c, &prefs.Wrap, loc.WrapLines()).Changed() {
		d.Wrap = prefs.Wrap
		prefs.save()
	}
}

// fileMenu is a file's context menu, of a commit's files or the changes.
func (a *app) fileMenu(m *ui.Menu, path, rev string) {
	if m.Item(loc.OpenInEditor()).Chosen() {
		a.openInEditor(a.repo.Path(path), 0)
	}
	if m.Item(loc.FileHistory()).Chosen() {
		a.setScope(git.Scope{Ref: a.scope.Ref, Path: path})
	}
	if m.Item(loc.Blame()).Chosen() {
		a.showBlame(rev, path)
	}
	m.Separator()
	if m.Item(loc.CopyPath()).Chosen() {
		mygo.Clipboard.WriteText(path)
	}
	if a.local() && m.Item(loc.ShowInFolder()).Chosen() {
		mygo.Shell.ShowItemInFolder(a.repo.Path(path))
	}
}

func (a *app) filesPane(c *ui.Context) {
	t := c.Theme()
	files := a.detail.files
	ui.Column(c).Fill().Children(func() {
		if len(files) > 1 {
			ui.Row(c).Height(26).Padding(0, 10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, loc.ViewedCount(a.viewedCount(), len(files))).FontSize(11).TextColor(t.TextMuted).Grow(1)
				treeToggle(c)
			})
		}
		a.filesList(c)
	})
}

func (a *app) filesList(c *ui.Context) {
	t := c.Theme()
	files := a.detail.files
	items := make([]int, len(files))
	for i := range items {
		items[i] = i
	}
	tree, n, at := a.listRows("files", items, func(i int) string { return fileName(files[i]) })
	a.fileRow = a.detail.fileSel
	if tree != nil {
		a.fileRow = treeRowOf(tree, a.detail.fileSel)
	}
	l := ui.List(c, &a.fileList, n, func(r int) {
		if at(r) < 0 {
			a.folderRow(c, "files", tree[r])
			return
		}
		i := at(r)
		f := files[i]
		code, name := byte('M'), f.NewName
		switch {
		case f.OldName == "":
			code = 'A'
		case f.NewName == "":
			code, name = 'D', f.OldName
		case f.OldName != f.NewName:
			code = 'R'
		}
		rev := ""
		if a.detail.commit != nil {
			rev = a.detail.commit.Hash
		}
		viewed, gen := a.isViewed(i), a.detail.generated[i]
		indent := float32(4)
		if tree != nil {
			indent = 16 + float32(tree[r].depth)*treeIndent
		}
		row := ui.Row(c).Height(24).Padding(0, 8, 0, indent).Gap(6).AlignItems(ui.Center).Tooltip(name)
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(map[bool]string{true: loc.MarkUnviewed(), false: loc.MarkViewed()}[viewed]).Chosen() {
				a.toggleViewed(i)
			}
			m.Separator()
			a.fileMenu(m, name, rev)
		}).Children(func() {
			chosen := i == a.detail.fileSel
			muted := t.TextMuted
			if chosen {
				muted = t.AccentText.Alpha(0.75)
			}
			// Viewed, as GitHub's checkbox: a check, or a ring while the
			// pointer is over the row.
			mark, label := "check", loc.MarkUnviewed()
			if !viewed {
				mark, label = "circle", loc.MarkViewed()
			}
			if viewed || row.Hovered() || chosen {
				if iconButton(c, mark, label) {
					a.toggleViewed(i)
				}
			} else {
				ui.Box(c).Size(22, 22)
			}
			nameText := ui.Text(c, path.Base(name)).SingleLine().Shrink(0)
			if (viewed || gen) && !chosen {
				nameText.TextColor(t.TextMuted)
			}
			if dir := path.Dir(name); dir != "." && tree == nil {
				ui.Text(c, dir).FontSize(11).TextColor(muted).SingleLine().Grow(1).Basis(0)
			} else {
				ui.Box(c).Grow(1)
			}
			if gen {
				ui.Text(c, loc.GeneratedTag()).FontSize(10).TextColor(muted).Shrink(0)
			}
			ui.Text(c, string(code)).Font("monospace").FontSize(11).Bold().TextColor(changeColor(t, code, chosen)).
				Width(14).TextAlign(ui.Center).Tooltip(changeName(code))
		})
	}).Grow(1).Basis(0)
	if r := a.fileRow; l.Changed() && r >= 0 && r < n {
		if at(r) < 0 {
			a.toggleFolder("files", tree[r].dir)
		} else {
			a.detail.fileSel = at(r)
			a.showFile(a.detail.fileSel)
		}
	}
	// Space marks the file chosen viewed and goes on to the next, as
	// GitHub's review does with its checkbox.
	a.fileList.Handle.OnShortcut(c, 0, ui.KeySpace, func() { a.toggleViewed(a.detail.fileSel) })
}

func (a *app) diffView(c *ui.Context) {
	t := c.Theme()
	d := &a.detail
	switch {
	case d.loading:
		ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(18, 18) })
	case d.images != nil:
		a.imageView(c, d.images)
	case d.folded:
		var p *editor.FilePatch
		if d.commit != nil && d.fileSel >= 0 && d.fileSel < len(d.files) {
			p = &d.files[d.fileSel]
		} else {
			p = d.patch
		}
		a.foldedView(c, p, a.unfold)
	case d.binary, d.commit != nil && d.fileSel >= 0 && d.fileSel < len(d.files) && d.files[d.fileSel].Binary:
		ui.Column(c).Fill().Center().Children(func() { ui.Text(c, loc.BinaryFile()).TextColor(t.TextMuted) })
	case d.diff != nil:
		a.reviewDiff(c, d.diff)
		row := ui.Row(c).Fill()
		row.Children(func() {
			dv := editor.DiffView(c.Key(d.gen*1000+d.fileSel), d.diff).Grow(1).Basis(0).AlignSelf(ui.Stretch)
			if d.whole {
				dv.OnShortcut(ui.Cmd, ui.KeyS, a.saveFile) // as the file is typed in
			}
			overview(c, d.diff, row, func(i int) int { return a.fileLine(editor.DiffNew, i) })
		})
	default:
		ui.Column(c).Fill().Center().Children(func() { ui.Text(c, loc.NoDiff()).TextColor(t.TextMuted) })
	}
}

// refLabel is how a ref shows: a tag without its "tag: ".
func refLabel(ref string) (string, bool) {
	if tag, ok := strings.CutPrefix(ref, "tag: "); ok {
		return tag, true
	}
	return ref, false
}

// lineButtons act on the lines selected in the diff of a change.
func (a *app) lineButtons(c *ui.Context) {
	if ui.Button(c, loc.OpenInEditor()).Clicked() {
		a.openChangeInEditor()
	}
	if a.detail.whole {
		if ui.PrimaryButton(c, loc.Save()).Disabled(!a.unsaved()).Tooltip(shortcut("S")).Clicked() {
			a.saveFile()
		}
		c.OnShortcut(ui.Cmd, ui.KeyS, a.saveFile)
	}
	if a.detail.patch == nil || !patch.Partial(*a.detail.patch) || prefs.whitespace().Any() {
		return
	}
	chosen := a.selectedLines()
	none := chosen == nil || a.busy != "" || a.unsaved()
	if a.detail.staged {
		if ui.Button(c, loc.UnstageLines()).Disabled(none).Clicked() {
			a.applyLines(chosen, lineUnstage)
		}
		return
	}
	if ui.Button(c, loc.DiscardLines()).Disabled(none).Clicked() {
		a.applyLines(chosen, lineDiscard)
	}
	if ui.Button(c, loc.StageLines()).Disabled(none).Tooltip(loc.StageLinesTooltip()).Clicked() {
		a.applyLines(chosen, lineStage)
	}
}
