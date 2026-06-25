// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/version"
	"github.com/bitwise-media-group/evolve/internal/web"
)

// ViewFlags holds the flags for `evolve view`.
type ViewFlags struct {
	Out    string // write a self-contained snapshot to this path and exit
	Port   int    // localhost port to bind (0 = pick a free one)
	NoOpen bool   // do not open the browser
}

var viewFlags = ViewFlags{}

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "Browse the stored results in a web browser (filter, sort, snapshot)",
	Long: "Serve the committed results as an interactive web report: filter by provider, model, " +
		"plugin, skill, type, and pass/fail; sort and toggle between per-case and rollup views; and " +
		"save a self-contained HTML snapshot of the current view.\n\n" +
		"The server is read-only and binds to localhost. While it runs it watches the results files, " +
		"so a concurrent `evolve run` (or any process that rewrites them) refreshes an open browser. " +
		"With --out it writes a snapshot file and exits without serving.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, err := opts.Repo()
		if err != nil {
			return err
		}
		srv := web.NewServer(repo, version.Version, opts.Log)

		if viewFlags.Out != "" {
			html, err := srv.Snapshot()
			if err != nil {
				return fmt.Errorf("view: %w", err)
			}
			if err := os.WriteFile(viewFlags.Out, html, 0o644); err != nil {
				return fmt.Errorf("view: write snapshot: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "view: wrote snapshot to %s\n", viewFlags.Out)
			return nil
		}

		return serveView(cmd, srv)
	},
}

// serveView starts the localhost HTTP server and the results watcher, opens the
// browser, and blocks until the command's context is cancelled (Ctrl-C).
func serveView(cmd *cobra.Command, srv *web.Server) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(viewFlags.Port)))
	if err != nil {
		return fmt.Errorf("view: listen: %w", err)
	}
	url := "http://" + ln.Addr().String()

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	ctx := cmd.Context()
	go srv.Watch(ctx, 0)

	fmt.Fprintf(cmd.OutOrStdout(), "view: serving %s (press Ctrl-C to stop)\n", url)
	if !viewFlags.NoOpen {
		if err := web.OpenBrowser(url); err != nil {
			opts.Log.LogAttrs(ctx, slog.LevelWarn, "view: could not open browser", slog.Any("error", err))
		}
	}

	select {
	case err := <-serveErr:
		return fmt.Errorf("view: serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		fmt.Fprintln(cmd.OutOrStdout(), "view: stopped")
		return nil
	}
}

func init() {
	viewCmd.Flags().StringVar(&viewFlags.Out, "out", "",
		"write a self-contained HTML snapshot to this path and exit (no server)")
	viewCmd.Flags().IntVar(&viewFlags.Port, "port", 0,
		"localhost port to serve on (default: pick a free port)")
	viewCmd.Flags().BoolVar(&viewFlags.NoOpen, "no-open", false,
		"do not open the report in a browser")
	rootCmd.AddCommand(viewCmd)
}
