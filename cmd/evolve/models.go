// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/version"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Print the effective provider/model matrix with pricing and provenance",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		providers, overridden, err := opts.Providers()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "MODEL\tDISPLAY\tINPUT $/MTOK\tOUTPUT $/MTOK\tCAPABILITIES\tSOURCE")
		for _, p := range providers {
			source := "builtin@" + version.Version
			if overridden[p.Name()] {
				if source = opts.ConfigFileName(); source == "" {
					source = "config"
				}
			}
			caps := "triggers"
			if _, ok := p.(provider.EvalRunner); ok {
				caps += "+evals"
			}
			if _, ok := p.(provider.TokenCounter); ok {
				caps += "+counting"
			}
			for _, m := range p.Models() {
				fmt.Fprintf(w, "%s/%s\t%s\t%s\t%s\t%s\t%s\n",
					p.Name(), m.ID, m.Display, fmtPrice(m.InputUSD), fmtPrice(m.OutputUSD), caps, source)
			}
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(modelsCmd)
}

func fmtPrice(p *float64) string {
	if p == nil {
		return "unpublished"
	}
	return fmt.Sprintf("%.2f", *p)
}
