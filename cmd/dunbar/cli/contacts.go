package cli

import (
	Z "github.com/rwxrob/bonzai/z"
	"github.com/rwxrob/help"
)

var Contacts = &Z.Cmd{
	Name:     "contacts",
	Summary:  "Manage your contacts",
	Commands: []*Z.Cmd{help.Cmd},
}
