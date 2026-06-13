// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/bitwise-media-group/evolve/internal/configdoc"
)

// DocsFlags holds the flags for the hidden docs command.
type DocsFlags struct {
	Out    string
	Format string
}

var docsFlags = DocsFlags{}

// docsCmd regenerates the committed CLI reference and configuration docs. It
// lives as a hidden command (rather than the usual standalone docgen helper)
// because the flat cmd/ layout makes this package main, which nothing can
// import.
var docsCmd = &cobra.Command{
	Use:    "docs",
	Short:  "Generate the CLI reference and configuration docs.",
	Hidden: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rootCmd.DisableAutoGenTag = true // keep the output reproducible
		if err := os.MkdirAll(docsFlags.Out, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", docsFlags.Out, err)
		}
		switch docsFlags.Format {
		case "markdown":
			return doc.GenMarkdownTree(rootCmd, docsFlags.Out)
		case "man":
			return doc.GenManTree(rootCmd, &doc.GenManHeader{Title: "EVOLVE", Section: "1"}, docsFlags.Out)
		case "rest":
			return doc.GenReSTTree(rootCmd, docsFlags.Out)
		case "config":
			return writeConfigDocs(docsFlags.Out)
		default:
			return fmt.Errorf("unknown format %q (expected markdown, man, rest, or config)", docsFlags.Format)
		}
	},
}

// writeConfigDocs renders the configuration reference plus the annotated
// example config files the loader accepts.
func writeConfigDocs(dir string) error {
	files := []struct {
		name string
		data []byte
	}{
		{"configuration.md", configdoc.Markdown()},
		{".evolve.yaml", configdoc.ExampleYAML()},
		{".evolve.jsonc", configdoc.ExampleJSONC()},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, f.data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func init() {
	docsCmd.Flags().StringVar(&docsFlags.Out, "out", "docs/cli", "directory to write the reference into")
	docsCmd.Flags().StringVar(&docsFlags.Format, "format", "markdown", "output format: markdown, man, rest, or config")
	rootCmd.AddCommand(docsCmd)
}
