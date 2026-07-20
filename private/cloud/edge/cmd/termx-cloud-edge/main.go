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

	"github.com/lozzow/termx/private/cloud/edge"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	flags := flag.NewFlagSet("termx-cloud-edge", flag.ExitOnError)
	configPath := flags.String("config", "", "Edge JSON config path")
	manifestPath := flags.String("manifest", "", "Edge runtime manifest path")
	_ = flags.Parse(os.Args[1:])
	if *configPath == "" || *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "--config and --manifest are required")
		os.Exit(2)
	}
	config, err := edge.LoadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	runtime, err := edge.Start(config)
	if err != nil {
		fatal(err)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = runtime.Close(shutdown)
	}()
	readyContext, cancelReady := context.WithTimeout(ctx, 15*time.Second)
	if err := runtime.WaitReady(readyContext); err != nil {
		cancelReady()
		fatal(err)
	}
	cancelReady()
	if err := runtime.WriteManifest(*manifestPath); err != nil {
		fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(runtime.Manifest())
	wait := make(chan error, 1)
	go func() { wait <- runtime.Wait() }()
	select {
	case <-ctx.Done():
	case err := <-wait:
		if err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
