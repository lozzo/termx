package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	clouddaemon "github.com/anytty/anytty/cloud/daemon"
	"github.com/spf13/cobra"
)

func cloudCommand() *cobra.Command {
	command := &cobra.Command{Use: "cloud", Short: "Manage AnyTTY Cloud enrollment", Args: cobra.NoArgs}
	command.AddCommand(cloudEnrollCommand())
	return command
}

func cloudEnrollCommand() *cobra.Command {
	var controllerOrigin, controllerAddress, controllerServerName string
	command := &cobra.Command{
		Use: "enroll CODE", Short: "Enroll this daemon DeviceIdentity into AnyTTY Cloud", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address, serverName, err := resolveController(controllerOrigin, controllerAddress, controllerServerName)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if _, ok := ctx.Deadline(); !ok {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
			}
			record, err := clouddaemon.EnrollLocal(ctx, address, serverName, args[0], v3RemoteIdentityDir(), v3CloudEnrollmentRecordPath())
			if err != nil {
				return fmt.Errorf("enroll daemon in AnyTTY Cloud: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloud enrollment complete: daemon=%s account=%s\n", record.DaemonID, record.AccountID)
			return nil
		},
	}
	command.Flags().StringVar(&controllerOrigin, "controller", "", "Controller HTTPS origin")
	command.Flags().StringVar(&controllerAddress, "controller-address", "", "Controller gRPC address override")
	command.Flags().StringVar(&controllerServerName, "controller-server-name", "", "Controller TLS server name override")
	_ = command.MarkFlagRequired("controller")
	return command
}

func resolveController(origin, addressOverride, serverOverride string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Path != "" {
		return "", "", fmt.Errorf("--controller must be an HTTPS origin")
	}
	serverName := strings.TrimSpace(serverOverride)
	if serverName == "" {
		serverName = parsed.Hostname()
	}
	address := strings.TrimSpace(addressOverride)
	if address == "" {
		port := parsed.Port()
		if port == "" {
			port = "443"
		}
		address = net.JoinHostPort(parsed.Hostname(), port)
	}
	return address, serverName, nil
}

func v3CloudEnrollmentRecordPath() string {
	return filepath.Join(v3RemoteIdentityDir(), "cloud_enrollment.json")
}
