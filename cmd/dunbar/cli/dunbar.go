package cli

import (
	Z "github.com/rwxrob/bonzai/z"
	"github.com/rwxrob/help"
)

var Cmd = &Z.Cmd{
	Name:    "dunbar",
	Summary: "Personal Relationship Manager CLI",
	Commands: []*Z.Cmd{
		help.Cmd,
		Version,
		Contacts,
		Messages,
	},
	Description: `dunbar did not have the internet`,
}
