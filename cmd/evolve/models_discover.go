// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/tui"
)

var modelsDiscoverFlags struct {
	NoTUI bool
}

var modelsDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "List vendor-served models and add new ones to the .evolve config",
	Long: `Discover queries each vendor's model-listing API (using the same credentials
as token counting; vendors without a set key are skipped) and marks every model
already in the effective registry with where it lives (builtin or the config
file).

Interactively, the models render in a fuzzy-find picker: type to filter, tab to
select any number of new models, enter to write them into the repository's
.evolve config (created as .evolve.yaml when missing). Because a
providers.<id>.models override replaces that provider's builtin list, the first
injected entry for a provider also seeds the list with its builtin models, so
the effective matrix only ever grows.

Vendor listing APIs publish no pricing, so injected entries carry none until
edited by hand; their costs render as unpublished in reports.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		items, err := discoverModels(cmd)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errors.New("no models discovered: no vendor credentials are set (see the skip warnings above)")
		}
		if !interactiveTUI(cmd, modelsDiscoverFlags.NoTUI) {
			return printDiscovered(cmd, items)
		}

		dir := opts.Root
		if dir == "" {
			dir = "."
		}
		dest, err := cli.FindConfigFile(dir)
		if err != nil {
			return err
		}
		destName := ".evolve.yaml"
		if dest != "" {
			destName = filepath.Base(dest)
		}

		chosen, ok, err := tui.RunDiscover(items, destName)
		if err != nil || !ok || len(chosen) == 0 {
			return err // a cancelled picker is a clean exit
		}
		selected := make([]model.Model, 0, len(chosen))
		for _, it := range chosen {
			selected = append(selected, model.Model{
				ID: it.ProviderID + "/" + it.ID, ProviderID: it.ProviderID, Name: it.Name,
			})
		}
		path, added, err := opts.InjectModels(selected)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "added %d model(s) to %s:\n", len(added), path)
		for _, id := range added {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", id)
		}
		if restrict := opts.Viper.GetStringSlice("models"); len(restrict) > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"note: this repo restricts runs via the `models` config (%s); extend it if the new models should run\n",
				strings.Join(restrict, ", "))
		}
		return nil
	},
}

func init() {
	modelsDiscoverCmd.Flags().BoolVar(&modelsDiscoverFlags.NoTUI, "no-tui", false,
		"print the discovered models instead of opening the interactive picker")
	modelsCmd.AddCommand(modelsDiscoverCmd)
}

// discoverModels queries every vendor with a listing API concurrently and
// tags each result with where it is already registered ("" = new). Vendors
// without credentials warn and drop out; any other listing failure aborts —
// a silently partial catalog would read as "vendor has nothing new".
func discoverModels(cmd *cobra.Command) ([]tui.DiscoverItem, error) {
	avail, err := opts.AvailableModels()
	if err != nil {
		return nil, err
	}
	_, overridden, err := opts.ModelOverrides()
	if err != nil {
		return nil, err
	}
	source := func(providerID, bareID string) string {
		if _, ok := model.ModelByID(avail, providerID+"/"+bareID); !ok {
			return ""
		}
		if overridden[providerID] {
			if name := opts.ConfigFileName(); name != "" {
				return name
			}
			return "config"
		}
		return "builtin"
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()
	provs := model.Providers()
	listings := make([][]model.DiscoveredModel, len(provs))
	var g errgroup.Group
	for i, p := range provs {
		lister, ok := model.ListerFor(p.ID)
		if !ok {
			continue
		}
		g.Go(func() error {
			ms, err := lister.ListModels(ctx)
			if errors.Is(err, model.ErrNoCredential) {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipping %s: %v (set %s)\n",
					p.Name, err, strings.Join(model.CounterEnvKeys(p.ID), " or "))
				return nil
			}
			if err != nil {
				return err
			}
			listings[i] = ms
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var items []tui.DiscoverItem
	for i, p := range provs {
		for _, d := range listings[i] {
			items = append(items, tui.DiscoverItem{
				ProviderID:   p.ID,
				ProviderName: p.Name,
				ID:           d.ID,
				Name:         d.Name,
				Source:       source(p.ID, d.ID),
			})
		}
	}
	return items, nil
}

// printDiscovered is the non-interactive fallback: the same rows as the
// picker, as a table.
func printDiscovered(cmd *cobra.Command, items []tui.DiscoverItem) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tNAME\tSTATUS")
	for _, it := range items {
		status := "new"
		if it.Source != "" {
			status = "already in " + it.Source
		}
		fmt.Fprintf(w, "%s/%s\t%s\t%s\n", it.ProviderID, it.ID, it.Name, status)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(),
		"run interactively (a terminal, without --no-tui) to pick models and add them to the .evolve config")
	return nil
}
