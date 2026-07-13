// Package main 启动显式 dev-local 单区域 TermX Cloud supervisor。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lozzow/termx/private/cloud/devcloud"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("termx-cloud-dev", flag.ContinueOnError)
	manifestPath := flags.String("manifest", ".artifacts/cloud-dev/runtime.json", "dev-local runtime manifest path")
	profile := flags.String("profile", "dev-local", "runtime profile: dev-local or staging-ssh")
	controlListen := flags.String("control-listen", "127.0.0.1:0", "loopback Control Plane listen address")
	hubListen := flags.String("hub-listen", "127.0.0.1:0", "loopback Hub listen address")
	relayListen := flags.String("relay-listen", "127.0.0.1:0", "UDP TURN listen address")
	relayPublicIP := flags.String("relay-public-ip", "", "public TURN address for staging-ssh")
	webAccountDB := flags.String("web-account-db", "", "SQLite web account database path")
	webCatalog := flags.String("web-catalog", "", "web plan catalog path")
	webStaging := flags.Bool("web-staging", false, "enable explicit staging browser login and checkout")
	webSecureCookie := flags.Bool("web-secure-cookie", false, "require HTTPS for browser session cookies")
	webPublicURL := flags.String("web-public-url", "", "public Web Controller origin used by device login")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected termx-cloud-dev arguments")
	}
	controlListener, err := net.Listen("tcp", *controlListen)
	if err != nil {
		return fmt.Errorf("listen Control Plane: %w", err)
	}
	hubListener, err := net.Listen("tcp", *hubListen)
	if err != nil {
		_ = controlListener.Close()
		return fmt.Errorf("listen Hub: %w", err)
	}
	runtime, err := devcloud.Start(devcloud.Config{
		ControlPlaneListener: controlListener, HubListener: hubListener,
		RelayListenAddr: *relayListen, RelayPublicIP: *relayPublicIP, Profile: *profile,
		WebAccountDBPath: *webAccountDB, WebCatalogPath: *webCatalog, WebStaging: *webStaging, WebSecureCookie: *webSecureCookie, WebPublicURL: *webPublicURL,
	})
	if err != nil {
		return err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = runtime.Close(shutdownContext)
	}()
	if err := runtime.WriteManifest(*manifestPath); err != nil {
		return err
	}
	manifest := runtime.Manifest()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{
		"profile": manifest.Profile, "control_plane": manifest.ControlPlaneURL,
		"hub": manifest.HubURL, "relay": manifest.RelayURL, "manifest": *manifestPath, "readiness": "ready",
	}); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- runtime.Wait() }()
	select {
	case <-ctx.Done():
	case err := <-wait:
		if err != nil {
			return err
		}
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return runtime.Close(shutdownContext)
}
