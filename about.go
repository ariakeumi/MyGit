package main

import (
	"github.com/egoist/mygo"

	loc "mygit/locales"
)

// about tells the version. The dialog waits: call it on a goroutine.
func about() {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Title:   loc.About(),
		Message: "MyGit " + mygo.App.Version(),
		Detail:  loc.AboutDetail(),
	})
}
