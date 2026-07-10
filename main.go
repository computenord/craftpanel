// ComputeBox Craftpanel is a small self-hosted panel for creating and
// managing Minecraft servers on a single host.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
	"github.com/computenord/craftpanel/internal/web"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
	addr := flag.String("addr", ":8420", "HTTP listen address")
	trustProxy := flag.Bool("behind-proxy", false, "trust X-Forwarded-For from a reverse proxy for login rate limiting")
	flag.Usage = usage
	flag.Parse()

	switch flag.Arg(0) {
	case "", "serve":
		if err := runServe(*dataDir, *addr, *trustProxy); err != nil {
			log.Fatal(err)
		}
	case "reset-password":
		if err := runResetPassword(*dataDir, flag.Arg(1)); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("craftpanel", version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `ComputeBox Craftpanel %s

Usage:
  craftpanel [flags] [command]

Commands:
  serve                       run the panel (default)
  reset-password <username>   set a new password, read from stdin
  version                     print the version

Flags:
`, version)
	flag.PrintDefaults()
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".craftpanel"
	}
	return filepath.Join(home, ".craftpanel")
}

func runServe(dataDir, addr string, trustProxy bool) error {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	authStore, err := auth.NewStore(dataDir)
	if err != nil {
		return err
	}
	versions := mc.NewVersions()
	manager, err := mc.NewManager(dataDir, versions)
	if err != nil {
		return err
	}

	// baseCtx is handed to every request. Cancelling it releases the long lived
	// SSE console streams, which would otherwise keep Shutdown waiting.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The self update endpoint triggers the same graceful shutdown as a signal
	// and then exits non-zero so systemd starts the freshly swapped binary.
	ctx, cancelForRestart := context.WithCancel(sigCtx)
	defer cancelForRestart()
	var restartRequested atomic.Bool
	requestRestart := func() {
		restartRequested.Store(true)
		cancelForRestart()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           web.New(authStore, manager, versions, version, trustProxy, requestRestart),
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go manager.StartAutostarts()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("ComputeBox Craftpanel %s listening on %s (data: %s)", version, addr, dataDir)
		if authStore.NeedsSetup() {
			log.Printf("no admin account yet: open the panel in a browser to create one")
		}
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down")
	cancelBase()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancelStop()
	manager.StopAll(stopCtx)
	if restartRequested.Load() {
		// Non-zero exit so both Restart=always and Restart=on-failure units
		// bring the new binary up.
		log.Printf("restarting into the updated binary")
		os.Exit(1)
	}
	log.Printf("bye")
	return nil
}

func runResetPassword(dataDir, username string) error {
	if username == "" {
		return errors.New("usage: craftpanel reset-password <username> (new password on stdin)")
	}
	store, err := auth.NewStore(dataDir)
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, "New password: ")
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	if err != nil && pw == "" {
		return err
	}
	pw = strings.TrimRight(pw, "\r\n")
	if err := store.SetPassword(username, pw); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password updated.")
	return nil
}
