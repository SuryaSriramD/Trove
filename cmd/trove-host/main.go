// trove-host operates the same workspace services without a desktop shell.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"trove.local/phase0/internal/hosting"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workers"
	"trove.local/phase0/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	workspacePath := flag.String("workspace", "", "workspace directory")
	listen := flag.String("listen", "127.0.0.1:7337", "listener; non-loopback requires TLS")
	tokenFile := flag.String("token-file", "", "private file holding the owner token")
	initToken := flag.Bool("init-token", false, "create a new private owner token file and exit")
	create := flag.Bool("create", false, "create workspace if absent")
	assets := flag.String("assets", "frontend/dist", "compiled frontend directory")
	cert := flag.String("tls-cert", "", "TLS certificate")
	key := flag.String("tls-key", "", "TLS private key")
	transfer := flag.String("transfer-to", "", "with the previous owner stopped, move via verified snapshot and fence old path")
	bundle := flag.String("native-bundle", "build/native", "verified native worker bundle")
	modelRoot := flag.String("models", "", "installed local model pack directory (optional)")
	flag.Parse()
	if *initToken {
		if *tokenFile == "" {
			return fmt.Errorf("--token-file required")
		}
		bytes := make([]byte, 32)
		if _, e := rand.Read(bytes); e != nil {
			return e
		}
		f, e := os.OpenFile(*tokenFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if _, e = f.WriteString(hex.EncodeToString(bytes) + "\n"); e != nil {
			return e
		}
		return f.Sync()
	}
	if *workspacePath == "" {
		return fmt.Errorf("--workspace required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *transfer != "" {
		result, e := workspace.TransferWorkspace(ctx, *workspacePath, *transfer)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if err := hosting.ValidateListen(*listen, *cert, *key); err != nil {
		return err
	}
	info, err := os.Lstat(*tokenFile)
	if err != nil {
		return fmt.Errorf("read private --token-file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 257 {
		return fmt.Errorf("token file must be a regular private 0600 file of at most 257 bytes")
	}
	raw, err := os.ReadFile(*tokenFile)
	if err != nil {
		return err
	}
	var k *workspace.Kernel
	if *create {
		k, err = workspace.Create(*workspacePath)
	} else {
		k, err = workspace.Open(*workspacePath)
	}
	if err != nil {
		return err
	}
	defer k.Close()
	absoluteAssets, err := filepath.Abs(*assets)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(absoluteAssets, "index.html")); err != nil {
		return fmt.Errorf("compiled UI required (--assets): %w", err)
	}
	host, err := hosting.New(k, strings.TrimSpace(string(raw)), os.DirFS(absoluteAssets), *cert != "")
	if err != nil {
		return err
	}
	defer host.Close()
	var supervisor *native.Supervisor
	if *modelRoot != "" {
		manager, e := native.NewManager(*modelRoot)
		if e != nil {
			return e
		}
		supervisor, e = native.NewSupervisor(*bundle, manager)
		if e != nil {
			return e
		}
	}
	workers.ConfigureSearch(k, supervisor)
	workers.ConfigureOCR(k, *bundle)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if e := workers.Run(ctx, k, k.OwnerContext(), supervisor); e != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "jobs:", e)
		}
	}()
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		owner := k.OwnerContext()
		defer k.RevokeGrant(string(owner.ScopeGrantId))
		tick := func(at time.Time) {
			for _, e := range []error{k.RunAutomationTick(ctx, owner, at), k.RunPersonalReviewTick(ctx, owner, at), k.QueueSemanticUpdates(ctx, owner)} {
				if e != nil && ctx.Err() == nil {
					fmt.Fprintln(os.Stderr, "maintenance:", e)
				}
			}
		}
		tick(time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case at := <-ticker.C:
				tick(at)
			}
		}
	}()
	defer func() { cancel(); <-done; <-maintenanceDone }()
	server := &http.Server{Addr: *listen, Handler: host, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	failures := make(chan error, 1)
	go func() {
		if *cert != "" {
			failures <- server.ListenAndServeTLS(*cert, *key)
		} else {
			failures <- server.ListenAndServe()
		}
	}()
	fmt.Fprintf(os.Stderr, "Trove host owns %s at %s; authenticate with the private token file.\n", k.ID(), *listen)
	select {
	case <-ctx.Done():
	case err = <-failures:
	}
	stop, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	shutdownErr := server.Shutdown(stop)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return shutdownErr
}
