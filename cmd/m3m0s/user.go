package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/rafaelvieiras/m3m0s/internal/apitypes"
	"github.com/rafaelvieiras/m3m0s/internal/client"
)

// runUser is `m3m0s user`: account administration (D35). It talks to the
// server with the API key (M3M0S_API_KEY), never with a browser session.
//
//	m3m0s user list
//	m3m0s user add <username>      creates the account and prints a link to set its password
//	m3m0s user reset <username>    prints a single-use link to set a new password
//	m3m0s user delete <username> --yes
func runUser(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: m3m0s user <list|add|reset|delete> [username]")
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("user "+cmd, flag.ExitOnError)
	baseURL := fs.String("url", "", "public URL used to build the link (default: M3M0S_URL)")
	yes := fs.Bool("yes", false, "confirm a deletion")
	if err := fs.Parse(reorderFlagsFirst(rest, "yes")); err != nil {
		return err
	}
	c := client.NewFromEnv()

	needName := func() (string, error) {
		if fs.NArg() != 1 {
			return "", fmt.Errorf("usage: m3m0s user %s <username>", cmd)
		}
		return fs.Arg(0), nil
	}

	switch cmd {
	case "list":
		resp, err := c.ListUsers(ctx)
		if err != nil {
			return err
		}
		if len(resp.Users) == 0 {
			fmt.Println("no accounts yet — open the WebUI to run the first-time setup, or run: m3m0s user add <username>")
			return nil
		}
		for _, u := range resp.Users {
			state := "password set"
			if !u.HasPassword {
				state = "waiting for password"
			}
			last := "never signed in"
			if u.LastLoginAt != nil {
				last = "last sign-in " + u.LastLoginAt.Local().Format("2006-01-02 15:04")
			}
			fmt.Printf("%-24s %-22s %s\n", u.Username, state, last)
		}
		return nil
	case "add":
		name, err := needName()
		if err != nil {
			return err
		}
		resp, err := c.CreateUser(ctx, name)
		if err != nil {
			return err
		}
		printLink(c, *baseURL, resp, "Account created. Open this link to choose its password")
		return nil
	case "reset":
		name, err := needName()
		if err != nil {
			return err
		}
		resp, err := c.ResetUser(ctx, name)
		if err != nil {
			return err
		}
		printLink(c, *baseURL, resp, "Open this link to choose a new password (the current one keeps working until then)")
		return nil
	case "delete":
		name, err := needName()
		if err != nil {
			return err
		}
		if !*yes {
			return fmt.Errorf("this deletes the account %q and signs it out everywhere; repeat with --yes to confirm", name)
		}
		if err := c.DeleteUser(ctx, name); err != nil {
			return err
		}
		fmt.Println("deleted", name)
		return nil
	default:
		return fmt.Errorf("unknown user command %q (expected list, add, reset or delete)", cmd)
	}
}

func printLink(c *client.Client, override string, resp apitypes.ResetLinkResponse, intro string) {
	base := strings.TrimRight(override, "/")
	if base == "" {
		base = strings.TrimRight(c.BaseURL(), "/")
	}
	fmt.Printf("%s (single use, expires %s):\n\n  %s%s\n\n", intro, resp.ExpiresAt.Local().Format(time.DateTime), base, resp.Path)
}
