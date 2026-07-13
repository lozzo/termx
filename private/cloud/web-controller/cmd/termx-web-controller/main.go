// Package main 启动 SSH-only staging Web Controller 运维 API。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:41000", "loopback Web Controller listen address")
	control := flag.String("control-plane", "http://127.0.0.1:41001", "loopback Control Plane origin")
	hub := flag.String("hub", "http://127.0.0.1:41002", "loopback Hub origin")
	relay := flag.String("relay", "", "operator-visible Relay URL")
	catalogPath := flag.String("catalog", "config/plans.json", "user-visible plan catalog configuration")
	stagingPayments := flag.Bool("staging-payments", false, "enable explicit non-production browser login and payment provider")
	flag.Parse()
	catalog, err := webcontroller.LoadCatalog(*catalogPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var commerce *webcontroller.CommerceService
	if *stagingPayments {
		commerce, err = webcontroller.NewCommerceService([]byte("termx-staging-payment-secret-v1-32-bytes"), webcontroller.HTTPEntitlementPublisher{Origin: *control}, time.Now)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	handler, err := webcontroller.StatusHandler(webcontroller.StatusConfig{ControlPlaneURL: *control, HubURL: *hub, RelayURL: *relay, Catalog: &catalog, Commerce: commerce})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
