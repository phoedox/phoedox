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
	"github.com/phoedox/phoedox/internal/backup"
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
  phx admin backup [--file F]                      Make a backup (to the backup target, or to file F)
  phx admin restore --file F --yes                 Replace ALL data with the backup in file F
  phx admin create-tenant --id ID --name N --domain D   Hosted mode: new customer (domain may repeat)
  phx admin tenants                                List tenants and domains

In the hosted mode (PHOEDOX_MULTI_TENANT=true) add --tenant ID to user commands.

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
	tenant := fs.String("tenant", store.DefaultTenant, "tenant (hosted mode)")
	tenantID := fs.String("id", "", "tenant id (create-tenant)")
	file := fs.String("file", "", "backup file")
	yes := fs.Bool("yes", false, "confirm the restore")
	var domains multi
	fs.Var(&domains, "domain", "domain of the tenant (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" && args[0] != "reindex" && args[0] != "create-tenant" && args[0] != "tenants" && args[0] != "backup" && args[0] != "restore" {
		return errors.New("--email is required")
	}
	ctx := context.Background()
	a, err := app.Open(ctx)
	if err != nil {
		return err
	}
	defer a.DB.Close()
	ctx = store.WithTenant(ctx, *tenant)
	if args[0] == "reindex" || args[0] == "backup" || args[0] == "restore" {
		ctx = store.System(ctx)
	}

	switch args[0] {
	case "backup":
		if *file != "" {
			f, err := os.Create(*file)
			if err != nil {
				return err
			}
			m, err := backup.Export(ctx, a.DB, a.Core.Blobs, a.Config.BackupFiles, "phx", a.Core.Box.KeyID(), f)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			fmt.Printf("Backup written to %s (%d files).\n", *file, m.Blobs)
			return nil
		}
		if a.Backups == nil {
			return errors.New("no backups in the hosted mode")
		}
		info, err := a.Backups.Create(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Backup %s (%d bytes) stored (%s).\n", info.Name, info.Size, a.Backups.Target)
		return nil
	case "restore":
		if *file == "" || !*yes {
			return errors.New("--file and --yes are required: the restore replaces ALL data")
		}
		m, err := backup.Restore(ctx, a.DB, a.Core.Blobs, a.Core.TempDir, *file)
		if err != nil {
			return err
		}
		if _, err := a.Core.Reindex(ctx); err != nil {
			return err
		}
		fmt.Printf("Restored the backup from %s (made by %s on %s). Restart the server.\n", m.CreatedAt.Format("2006-01-02 15:04"), m.App, m.Dialect)
		if m.KeyID != "" && m.KeyID != a.Core.Box.KeyID() {
			fmt.Println("WARNING: the backup was made with another master key. Use that key (PHOEDOX_MASTER_KEY or master.key), otherwise secrets, webhook secrets and two-factor settings cannot be read.")
		}
		return nil
	case "create-tenant":
		if *tenantID == "" || *name == "" || len(domains) == 0 {
			return errors.New("--id, --name and at least one --domain are required")
		}
		if err := a.DB.CreateTenant(ctx, store.Tenant{ID: *tenantID, Name: *name, Domains: domains}); err != nil {
			return err
		}
		fmt.Printf("Tenant %s created for %s. Create its first admin with: phx admin create-user --tenant %s --admin --email …\n", *tenantID, strings.Join(domains, ", "), *tenantID)
		return nil
	case "tenants":
		ts, err := a.DB.Tenants(ctx)
		if err != nil {
			return err
		}
		for _, t := range ts {
			fmt.Printf("%-20s %-30s %s\n", t.ID, t.Name, strings.Join(t.Domains, ", "))
		}
		return nil
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

// multi collects a repeated string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }
