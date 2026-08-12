// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// resolveRemoteURL resolves the patchy service URL: the --remote-url flag
// wins, then the `remote.url` config key (env EVOLVE_REMOTE_URL via
// AutomaticEnv). Empty means no remote is configured.
func resolveRemoteURL(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("remote-url"); f != nil && f.Changed {
		return f.Value.String()
	}
	if opts.Viper != nil {
		return opts.Viper.GetString("remote.url")
	}
	return ""
}

// requireRemoteURL is resolveRemoteURL for commands that cannot proceed
// without one (login/logout).
func requireRemoteURL(cmd *cobra.Command) (string, error) {
	u := resolveRemoteURL(cmd)
	if u == "" {
		return "", errors.New("no remote configured: pass --remote-url or set remote.url (EVOLVE_REMOTE_URL)")
	}
	return u, nil
}

// remoteMode decides whether a run executes remotely: the --remote/--local
// force flags win; otherwise `remote.default` (with a configured remote.url)
// opts the repository in.
func remoteMode(cmd *cobra.Command) (bool, error) {
	forceRemote, _ := cmd.Flags().GetBool("remote")
	forceLocal, _ := cmd.Flags().GetBool("local")
	switch {
	case forceRemote:
		if resolveRemoteURL(cmd) == "" {
			return false, errors.New("--remote needs a remote: pass --remote-url or set remote.url (EVOLVE_REMOTE_URL)")
		}
		return true, nil
	case forceLocal:
		return false, nil
	}
	if opts.Viper != nil && opts.Viper.GetBool("remote.default") && resolveRemoteURL(cmd) != "" {
		return true, nil
	}
	return false, nil
}
