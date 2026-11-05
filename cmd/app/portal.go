package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/guilherme-grimm/graph-go/internal/portal"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func newPortalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portal",
		Short: "Provision GoXIO accounts and servers.",
		Long:  "portal creates tenant accounts, servers, and loads a collector snapshot into one server. Passwords and ingest tokens are never written to the service log. The engine does not SSH to the server address.",
	}
	cmd.AddCommand(newPortalAccountCmd(), newPortalServerCmd(), newPortalSnapshotCmd())
	return cmd
}

func newPortalAccountCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "Manage portal accounts."}
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an account. Password comes from GOXIO_PORTAL_PASSWORD or a terminal prompt, not a flag.",
		RunE: func(cmd *cobra.Command, args []string) error {
			login, _ := cmd.Flags().GetString("login")
			password, err := readPortalPassword()
			if err != nil {
				return err
			}
			p, err := openPortal()
			if err != nil {
				return err
			}
			acct, err := p.CreateAccount(login, password)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created account %s\n", acct.Login)
			return nil
		},
	}
	create.Flags().String("login", "", "account login")
	_ = create.MarkFlagRequired("login")
	cmd.AddCommand(create)
	return cmd
}

func newPortalServerCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Manage an account's servers."}
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a server and print its ingest token once.",
		RunE: func(cmd *cobra.Command, args []string) error {
			login, _ := cmd.Flags().GetString("login")
			name, _ := cmd.Flags().GetString("name")
			address, _ := cmd.Flags().GetString("address")
			p, err := openPortal()
			if err != nil {
				return err
			}
			acct, err := p.FindAccount(login)
			if err != nil {
				return fmt.Errorf("account not found")
			}
			srv, token, err := p.AddServer(acct.ID, name, address)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "server %s (%s)\ningest token (shown once; store it in a mode 0600 file, not git):\n%s\n", srv.ID, srv.Name, token)
			return nil
		},
	}
	add.Flags().String("login", "", "owning account login")
	add.Flags().String("name", "", "display name")
	add.Flags().String("address", "", "host or IP label; not dialed")
	_ = add.MarkFlagRequired("login")
	_ = add.MarkFlagRequired("name")
	_ = add.MarkFlagRequired("address")
	cmd.AddCommand(add)
	return cmd
}

func newPortalSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot", Short: "Load a snapshot file into one server."}
	load := &cobra.Command{
		Use:   "load",
		Short: "Store a collector JSON file on one account's server. This does not scan the engine host.",
		RunE: func(cmd *cobra.Command, args []string) error {
			login, _ := cmd.Flags().GetString("login")
			serverName, _ := cmd.Flags().GetString("server")
			file, _ := cmd.Flags().GetString("file")
			p, err := openPortal()
			if err != nil {
				return err
			}
			acct, err := p.FindAccount(login)
			if err != nil {
				return fmt.Errorf("account not found")
			}
			list, err := p.ListServers(acct.ID)
			if err != nil {
				return err
			}
			var id string
			for _, srv := range list {
				if srv.ID == serverName || srv.Name == serverName {
					id = srv.ID
					break
				}
			}
			if id == "" {
				return fmt.Errorf("server not found for this account")
			}
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()
			body, err := io.ReadAll(io.LimitReader(f, 8<<20))
			if err != nil {
				return err
			}
			var snap space.Snapshot
			if err := json.Unmarshal(body, &snap); err != nil {
				return fmt.Errorf("invalid snapshot json")
			}
			snap.Source = "ingest"
			if err := p.SaveSnapshot(acct.ID, id, snap); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "loaded snapshot onto %s (%d nodes, partial=%v)\n", id, len(snap.Nodes), snap.Partial)
			return nil
		},
	}
	load.Flags().String("login", "", "account login")
	load.Flags().String("server", "", "server id or name")
	load.Flags().String("file", "", "snapshot json path on this machine")
	_ = load.MarkFlagRequired("login")
	_ = load.MarkFlagRequired("server")
	_ = load.MarkFlagRequired("file")
	cmd.AddCommand(load)
	return cmd
}

func newCollectCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Read-only local collect to a snapshot file. Does not attach it to a tenant.",
		RunE: func(cmd *cobra.Command, args []string) error {
			snap, err := space.Collect(cmd.Context(), nil)
			if err != nil && len(snap.Nodes) == 0 {
				return err
			}
			b, err := json.MarshalIndent(snap, "", "  ")
			if err != nil {
				return err
			}
			if outPath == "" {
				_, err = cmd.OutOrStdout().Write(append(b, '\n'))
				return err
			}
			return os.WriteFile(outPath, append(b, '\n'), 0o600)
		},
	}
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "write the snapshot to this file (mode 0600)")
	return cmd
}

func openPortal() (*portal.Store, error) {
	dir := os.Getenv("GOXIO_DATA_DIR")
	if dir == "" {
		dir = "data/portal"
	}
	return portal.Open(dir, portal.Options{SecureCookies: os.Getenv("GOXIO_COOKIE_SECURE") != "0"})
}

func readPortalPassword() (string, error) {
	if p := strings.TrimSpace(os.Getenv("GOXIO_PORTAL_PASSWORD")); p != "" {
		return p, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("set GOXIO_PORTAL_PASSWORD or run from a terminal; there is no --password flag")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
