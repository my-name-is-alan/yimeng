package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/config"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

// resetPassword implements `yimeng-core reset-password [username]`: it sets
// a new password for the account (creating it as admin when missing) and
// revokes that account's sessions. The password comes from
// YIMENG_ADMIN_PASSWORD or is prompted twice on the terminal (not echoed).
func resetPassword(cfg config.Config, args []string) int {
	user := "admin"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		user = strings.TrimSpace(args[0])
	}
	pw := os.Getenv("YIMENG_ADMIN_PASSWORD")
	if pw == "" {
		read := func(prompt string) string {
			fmt.Fprint(os.Stderr, prompt)
			if term.IsTerminal(int(os.Stdin.Fd())) {
				b, _ := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				return string(b)
			}
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			return strings.TrimRight(line, "\r\n")
		}
		pw = read("New password for " + user + ": ")
		if term.IsTerminal(int(os.Stdin.Fd())) && read("Repeat: ") != pw {
			fmt.Fprintln(os.Stderr, "passwords do not match")
			return 1
		}
	}
	if err := auth.CheckStrength(user, pw); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx := context.Background()
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	if _, err := db.Migrate(ctx, database, migrations.FS); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	created, err := auth.New(database, 0, nil).ResetPassword(ctx, user, pw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if created {
		fmt.Fprintf(os.Stderr, "created admin account %q\n", user)
	} else {
		fmt.Fprintf(os.Stderr, "password for %q reset; its sessions were signed out\n", user)
	}
	return 0
}
