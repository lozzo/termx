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

	"github.com/muxvia/muxvia/private/cloud/controller"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	flags := flag.NewFlagSet("muxvia-cloud-controller", flag.ExitOnError)
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
	// Creem secret 只从部署环境进入进程内配置，不落入 JSON、manifest 或日志。
	config.CreemAPIKey = os.Getenv("MUXVIA_CREEM_API_KEY")
	config.CreemWebhookSecret = os.Getenv("MUXVIA_CREEM_WEBHOOK_SECRET")
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
