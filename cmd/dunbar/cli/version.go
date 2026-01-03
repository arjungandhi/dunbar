package cli

import (
	"fmt"

	Z "github.com/rwxrob/bonzai/z"
	"github.com/rwxrob/help"

	"github.com/arjungandhi/dunbar/pkg/version"
)

var Version = &Z.Cmd{
	Name:     "version",
	Summary:  "Display version information",
	Commands: []*Z.Cmd{help.Cmd},
	Description: `
Display the current version of the dunbar CLI.
`,
	Call: versionCommand,
}

func versionCommand(cmd *Z.Cmd, args ...string) error {
	fmt.Printf("dunbar version %s\n", version.Version)
	return nil
}
