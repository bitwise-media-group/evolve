// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/remote"
)

// loginFlags holds the login command's flags.
var loginFlags struct {
	RemoteURL string
	NoBrowser bool
}

// loginCmd signs in to a patchy remote-evaluation service: OIDC
// authorization-code + PKCE against the issuer the service advertises, with
// a localhost ephemeral-port redirect. The credential lands user-level
// (UserConfigDir), keyed by remote URL — deliberately repo-independent, like
// the command itself.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in to a patchy remote-evaluation service.",
	Long: `Sign in to a patchy remote-evaluation service.

The service advertises its OIDC issuer and client id (GET /api/v1/auth/info),
so the only configuration is the service URL itself: --remote-url, or the
remote.url config key (env EVOLVE_REMOTE_URL). The flow is authorization-code
with PKCE against a public client — no client secret anywhere — redirecting to
an ephemeral localhost listener; a browser opens best-effort and the URL is
always printed for the times it cannot.

The credential is stored per remote URL in the user configuration directory
(evolve/credentials.json), not in any repository.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		url, err := requireRemoteURL(cmd)
		if err != nil {
			return err
		}
		store, err := remote.NewStore()
		if err != nil {
			return err
		}
		return remote.Login(cmd.Context(), store, url, loginFlags.NoBrowser, cmd.OutOrStdout())
	},
}

// logoutCmd forgets the stored credential for a remote.
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Forget the stored credential for a patchy remote-evaluation service.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		url, err := requireRemoteURL(cmd)
		if err != nil {
			return err
		}
		store, err := remote.NewStore()
		if err != nil {
			return err
		}
		if err := store.Delete(url); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Logged out of %s\n", url)
		return nil
	},
}

func init() {
	for _, cmd := range []*cobra.Command{loginCmd, logoutCmd} {
		cmd.Flags().StringVar(&loginFlags.RemoteURL, "remote-url", "",
			"patchy remote-evaluation service URL (default: the remote.url config key)")
	}
	loginCmd.Flags().BoolVar(&loginFlags.NoBrowser, "no-browser", false,
		"print the sign-in URL only; never launch a browser")
	rootCmd.AddCommand(loginCmd, logoutCmd)
}
