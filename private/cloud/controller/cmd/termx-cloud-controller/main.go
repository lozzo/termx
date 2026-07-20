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

	"github.com/lozzow/termx/private/cloud/controller"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	flags := flag.NewFlagSet("termx-cloud-controller", flag.ExitOnError)
	configPath := flags.String("config", "", "Controller JSON config path")
	manifestPath := flags.String("manifest", "", "Controller runtime manifest path")
	_ = flags.Parse(os.Args[1:])
	if *configPath == "" || *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "--config and --manifest are required")
		os.Exit(2)
	}
	config, err := controller.LoadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	runtime, err := controller.Start(config)
	if err != nil {
		fatal(err)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = runtime.Close(shutdown)
	}()
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
