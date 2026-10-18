package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// serverHost is the server's --host. Empty means the default host, which
// is looked up only when the server starts: a flag default would look for
// (and create) the socket folder in every crush command.
var serverHost string

func init() {
	serverCmd.Flags().StringVarP(&serverHost, "host", "H", "", "Server host (TCP or Unix socket)")
	rootCmd.AddCommand(serverCmd)
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Crush server",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Join the memory box shared by every Crush process first, so all
		// of the server's work counts toward its limit.
		_ = joinMemoryBox(memlimit.Slice)
		_, crashDone := recordCrashes()
		defer crashDone()
		host := serverHost
		if host == "" {
			host = server.DefaultHost()
		}
		if host == "" {
			return errNoDefaultHost()
		}
		hostURL, err := server.ParseHostURL(host)
		if err != nil {
			return fmt.Errorf("invalid server host: %v", err)
		}
		dataDir, err := cmd.Flags().GetString("data-dir")
		if err != nil {
			return fmt.Errorf("failed to get data directory: %v", err)
		}
		debug, err := cmd.Flags().GetBool("debug")
		if err != nil {
			return fmt.Errorf("failed to get debug flag: %v", err)
		}

		cfg, err := config.Load(config.GlobalWorkspaceDir(), dataDir, debug)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %v", err)
		}

		logFile := serverctl.LogFile(hostURL)

		if term.IsTerminal(os.Stderr.Fd()) {
			crushlog.Setup(logFile, debug, os.Stderr)
		} else {
			crushlog.Setup(logFile, debug)
		}

		srv := server.NewServer(cfg, hostURL.Scheme, hostURL.Host)
		srv.SetLogger(slog.Default())
		slog.Info("Starting Crush server...", "addr", host)

		errch := make(chan error, 1)
		sigch := make(chan os.Signal, 1)
		sigs := []os.Signal{os.Interrupt}
		sigs = append(sigs, addSignals(sigs)...)
		signal.Notify(sigch, sigs...)

		go func() {
			errch <- srv.ListenAndServe()
		}()

		select {
		case <-sigch:
			slog.Info("Received interrupt signal...")
		case err = <-errch:
			if err != nil && !errors.Is(err, server.ErrServerClosed) {
				_ = srv.Close()
				slog.Error("Server error", "error", err)
				return fmt.Errorf("server error: %v", err)
			}
		}

		if errors.Is(err, server.ErrServerClosed) {
			return nil
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()

		slog.Info("Shutting down...")

		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("Failed to shutdown server", "error", err)
			return fmt.Errorf("failed to shutdown server: %v", err)
		}

		return nil
	},
}

// errNoDefaultHost explains why there is no host when --host was not
// given: server.DefaultHost found no private folder for the socket.
func errNoDefaultHost() error {
	_, err := server.PrivateSocketDir()
	if err == nil {
		err = server.ErrNoPrivateSocketDir
	}
	return fmt.Errorf("no server host: %w", err)
}
