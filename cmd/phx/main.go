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
	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

const usage = `phx – Phoedox CLI

Admin commands (run on the server, same environment as phoedox):
  phx admin create-user --email E --name N [--admin] [--password P]
  phx admin invite --email E [--name N] [--admin]  Invitation link (emailed if SMTP is set)
  phx admin reset-password --email E [--password P]
  phx admin reset-2fa --email E                    Remove TOTP, recovery codes and passkeys
  phx admin bot-protection-off                     Switch the bot check off for every place (emergency)
  phx admin delete-user --email E --yes            Delete and anonymise an account (like Settings → Account)
  phx admin deleted-projects                       List deleted projects (in the trash) and when they go for good
  phx admin restore-project --project SLUG         Restore a deleted project
  phx admin export --project SLUG --file F [--git] Export a project (ZIP, or a Git repository with history)
  phx admin entitle --feature F (--org O | --email E) [--revoke] [--note N]
                                                   Grant (or revoke) a paid feature (PHOEDOX_PAID_FEATURES)
  phx admin test-mail --email E                    Send a test mail with the SMTP settings (PHOEDOX_SMTP_*)
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
	yes := fs.Bool("yes", false, "confirm restore or delete-user")
	feature := fs.String("feature", "", "paid feature (entitle)")
	orgRef := fs.String("org", "", "organization id or slug (entitle)")
	revoke := fs.Bool("revoke", false, "revoke instead of grant (entitle)")
	note := fs.String("note", "", "note stored with the grant (entitle)")
	projectRef := fs.String("project", "", "project id or slug (restore-project, export)")
	gitFormat := fs.Bool("git", false, "export as a Git repository with history (export)")
	var domains multi
	fs.Var(&domains, "domain", "domain of the tenant (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" && args[0] != "reindex" && args[0] != "entitle" && args[0] != "create-tenant" && args[0] != "tenants" && args[0] != "backup" && args[0] != "restore" && args[0] != "deleted-projects" && args[0] != "restore-project" && args[0] != "export" && args[0] != "bot-protection-off" {
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
	case "deleted-projects":
		cli := core.Actor{User: store.User{ID: "cli", IsAdmin: true}, Via: "cli"}
		list, err := a.Core.TrashedProjects(ctx, cli)
		if err != nil {
			return err
		}
		for _, t := range list {
			until := "kept until restored"
			if t.PurgeAt != nil {
				until = "deleted for good on " + t.PurgeAt.Format("2006-01-02")
			}
			fmt.Printf("%s\t%s\tdeleted %s\t%s\n", t.Slug, t.Name, t.DeletedAt.Time.Format("2006-01-02 15:04"), until)
		}
	case "export":
		if *projectRef == "" || *file == "" {
			return errors.New("--project and --file are required")
		}
		f, err := os.Create(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		cli := core.Actor{User: store.User{ID: "cli", Name: "Admin", IsAdmin: true}, Via: "cli"}
		if *gitFormat {
			err = a.Core.ExportProjectGit(ctx, cli, *projectRef, f)
		} else {
			err = a.Core.ExportProject(ctx, cli, *projectRef, f)
		}
		if err != nil {
			return fmt.Errorf("export %s: %w", *projectRef, err)
		}
		fmt.Printf("exported %s to %s\n", *projectRef, *file)
	case "restore-project":
		if *projectRef == "" {
			return errors.New("--project is required")
		}
		cli := core.Actor{User: store.User{ID: "cli", IsAdmin: true}, Via: "cli"}
		p, err := a.Core.RestoreProject(ctx, cli, *projectRef)
		if err != nil {
			return fmt.Errorf("project %s: %w", *projectRef, err)
		}
		fmt.Printf("restored %s (%s)\n", p.Slug, p.Name)
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
	case "entitle":
		if *feature == "" || (*orgRef == "") == (*email == "") {
			return errors.New("use --feature and either --org or --email")
		}
		var sub core.Subject
		if *orgRef != "" {
			o, err := a.DB.OrgFor(ctx, *orgRef, "")
			if err != nil {
				return fmt.Errorf("organization %q: %w", *orgRef, err)
			}
			sub = core.OrgSubject(o)
		} else {
			u, err := a.DB.UserByEmail(ctx, *email)
			if err != nil {
				return fmt.Errorf("user %q: %w", *email, err)
			}
			sub = core.UserSubject(u)
		}
		cli := core.Actor{User: store.User{ID: "cli", Name: "Admin", IsAdmin: true}, Via: "cli"}
		if *revoke {
			ok, err := a.Core.RevokeFeature(ctx, cli, sub, *feature)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("nothing to revoke (grandfathered rights stay)")
				return nil
			}
			fmt.Println("revoked", *feature)
			return nil
		}
		if err := a.Core.GrantFeature(ctx, cli, sub, *feature, "", *note); err != nil {
			return err
		}
		fmt.Println("granted", *feature)
	case "bot-protection-off":
		// Emergency: switch the bot check off everywhere, e.g. when a
		// misconfigured provider locks people out.
		if a.Core.Bot == nil {
			return errors.New("bot protection is not available")
		}
		if err := a.Core.Bot.Off(ctx, "cli"); err != nil {
			return err
		}
		fmt.Println("bot protection switched off for every place; turn it on again under Administration → Bot protection")
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
	case "test-mail":
		if a.Core.Mail == nil || !a.Core.Mail.Enabled() {
			return fmt.Errorf("mail is not configured: set PHOEDOX_SMTP_HOST (Helm: mail.enabled)")
		}
		if *email == "" {
			return fmt.Errorf("add --email with the address to send to")
		}
		err := a.Core.Mail.Send(ctx, mail.Message{To: *email, Subject: "Phoedox: test mail",
			Body: "This test mail shows that Phoedox can send e-mail with its SMTP settings.\n\nDiese Test-Mail zeigt, dass Phoedox mit seinen SMTP-Einstellungen E-Mails versenden kann."})
		if err != nil {
			return err
		}
		fmt.Printf("test mail sent to %s via %s:%d (%s)\n", *email, a.Config.SMTP.Host, a.Config.SMTP.Port, a.Config.SMTP.TLS)
	case "delete-user":
		// Disabled accounts can be deleted too.
		u, err := a.DB.UserByEmailAny(ctx, *email)
		if err != nil {
			return fmt.Errorf("user %s: %w", *email, err)
		}
		if !*yes {
			return fmt.Errorf("deleting %s cannot be undone: add --yes", u.Email)
		}
		cli := core.Actor{User: store.User{ID: "cli", IsAdmin: true}, Via: "cli"}
		plan, err := a.Core.DeleteAccount(ctx, cli, u.ID)
		for _, b := range plan.Blockers {
			fmt.Fprintf(os.Stderr, "blocked by %s %s %s\n", b.Kind, b.Slug, b.Name)
		}
		if err != nil {
			return err
		}
		for _, d := range plan.Deletes {
			fmt.Printf("deleted %s %s\n", d.Kind, d.Slug)
		}
		fmt.Println("account deleted and anonymised")
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
