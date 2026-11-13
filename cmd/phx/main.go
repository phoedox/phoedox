// Command phx is the Phoedox command line tool. The admin commands run on the
// server with the same PHOEDOX_* environment and work without any plugin
// (emergency access).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/phoedox/phoedox/internal/app"
	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

const usage = `phx – Phoedox CLI

Admin commands (run on the server, same environment as phoedox):
  phx admin create-user --email E --name N [--admin] [--password P]
  phx admin invite --email E [--name N] [--admin]  Invitation link (emailed if SMTP is set)
  phx admin reset-password --email E [--password P]
  phx admin reset-2fa --email E                    Remove TOTP, recovery codes and passkeys
  phx admin create-token --email E [--name N]     Personal API token (REST, MCP)
  phx admin reindex                                Rebuild the search index

Other:
  phx version`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return
	}
	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println("phx dev")
		return
	case "admin":
		err = admin(os.Args[2:])
	default:
		err = errors.New("unknown command")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "\n"+usage)
		os.Exit(1)
	}
}

func admin(args []string) error {
	if len(args) == 0 {
		return errors.New("missing admin command")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	email := fs.String("email", "", "email address")
	name := fs.String("name", "", "display name or token name")
	password := fs.String("password", "", "password (asked interactively if empty)")
	isAdmin := fs.Bool("admin", false, "make the user a system admin")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" && args[0] != "reindex" {
		return errors.New("--email is required")
	}
	ctx := context.Background()
	a, err := app.Open(ctx)
	if err != nil {
		return err
	}
	defer a.DB.Close()

	switch args[0] {
	case "create-user":
		if *name == "" {
			*name = strings.Split(*email, "@")[0]
		}
		pw, err := passwordOr(*password)
		if err != nil {
			return err
		}
		u, err := a.Auth.CreateUser(ctx, *email, *name, pw, *isAdmin)
		if err != nil {
			return err
		}
		fmt.Printf("created user %s (%s)%s\n", u.Email, u.ID, map[bool]string{true: " as system admin", false: ""}[u.IsAdmin])
	case "invite":
		cli := core.Actor{User: store.User{ID: "cli", Name: "Admin", IsAdmin: true}, Via: "cli"}
		res, err := a.Auth.Invite(ctx, cli, auth.InviteInput{Email: *email, Name: *name, Admin: *isAdmin}, baseURL(a.Config.PublicURL))
		if err != nil {
			return err
		}
		fmt.Println(res.Link)
		if res.Mailed {
			fmt.Fprintln(os.Stderr, "Invitation sent to", res.User.Email)
		}
		if a.Config.PublicURL == "" {
			fmt.Fprintln(os.Stderr, "Set PHOEDOX_PUBLIC_URL so links point to your domain.")
		}
	case "reset-password":
		u, err := a.DB.UserByEmail(ctx, *email)
		if err != nil {
			return fmt.Errorf("user %s: %w", *email, err)
		}
		pw, err := passwordOr(*password)
		if err != nil {
			return err
		}
		hash, err := auth.NewPasswordHash(pw)
		if err != nil {
			return err
		}
		if err := a.DB.SetPassword(ctx, u.ID, hash); err != nil {
			return err
		}
		_ = a.DB.AddEvent(ctx, "cli", "auth.password_reset", u.ID, nil)
		fmt.Println("password updated")
	case "reindex":
		n, err := a.Core.Reindex(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("indexed %d documents and entries\n", n)
	case "reset-2fa":
		u, err := a.DB.UserByEmail(ctx, *email)
		if err != nil {
			return fmt.Errorf("user %s: %w", *email, err)
		}
		cli := core.Actor{User: store.User{ID: "cli", IsAdmin: true}, Via: "cli"}
		if err := a.Auth.ResetSecondFactors(ctx, cli, u.ID); err != nil {
			return err
		}
		fmt.Println("two-factor authentication and passkeys removed")
	case "create-token":
		u, err := a.DB.UserByEmail(ctx, *email)
		if err != nil {
			return fmt.Errorf("user %s: %w", *email, err)
		}
		if *name == "" {
			*name = "CLI"
		}
		_, plain, err := a.Auth.CreateToken(ctx, u.ID, *name, "write", nil)
		if err != nil {
			return err
		}
		fmt.Println(plain)
		fmt.Fprintln(os.Stderr, "Store this token now – it is not shown again.")
	default:
		return fmt.Errorf("unknown admin command %q", args[0])
	}
	return nil
}

func baseURL(public string) string {
	if public != "" {
		return public
	}
	return "http://localhost:8080"
}

func passwordOr(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	fmt.Fprint(os.Stderr, "Password (min. 10 characters): ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}
