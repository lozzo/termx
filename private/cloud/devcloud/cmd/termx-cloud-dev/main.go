// Package main 启动显式 dev-local 单区域 TermX Cloud supervisor。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected termx-cloud-dev arguments")
	}
	runtime, err := devcloud.Start(devcloud.Config{})
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
		"hub": manifest.HubURL, "manifest": *manifestPath, "readiness": "ready",
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
