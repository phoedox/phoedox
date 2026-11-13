// Command phx is the Phoedox command line tool. On the server it also provides
// the emergency admin commands (break-glass) that work without any plugin.
package main

import (
	"fmt"
	"os"
)

const usage = `phx – Phoedox CLI

Usage:
  phx version
  phx admin plugins disable <id>   Disable a plugin on this instance (offline)
  phx admin unlock <user>          Unlock a user account
  phx admin break-glass            Create a one-time emergency admin login

Most commands are not implemented yet.`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("phx dev")
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}
