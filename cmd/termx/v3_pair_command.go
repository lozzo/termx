package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/spf13/cobra"
)

const maxPairingBundleBytes = 1 << 20

var saveV3ConnectionRegistry = connection.Save

func v3PairCommand() *cobra.Command {
	command := &cobra.Command{Use: "pair", Short: "Create or import a direct daemon capability"}
	command.AddCommand(v3PairCreateCommand())
	command.AddCommand(v3PairImportCommand())
	return command
}

func v3PairCreateCommand() *cobra.Command {
	var outputPath string
	var label string
	var terminalID string
	var lifetime time.Duration
	command := &cobra.Command{
		Use:   "create",
		Short: "Issue a pairing bundle from this daemon identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			identity, err := remoteauth.LoadOrCreateLocalIdentity(v3RemoteIdentityDir())
			if err != nil {
				return err
			}
			scope := remoteauth.FullDaemonScope()
			if terminalID = strings.TrimSpace(terminalID); terminalID != "" {
				scope = remoteauth.Scope{TerminalID: terminalID}
			}
			bundle, err := remoteauth.IssuePairingBundle(identity, remoteauth.PairingIssueOptions{
				Label: label, Scope: scope, Lifetime: lifetime,
			})
			if err != nil {
				return err
			}
			payload, err := remoteauth.EncodePairingBundle(bundle)
			if err != nil {
				return err
			}
			if strings.TrimSpace(outputPath) == "" {
				_, err = cmd.OutOrStdout().Write(payload)
				return err
			}
			if err := writeV3PrivateFile(outputPath, payload); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pairing bundle written to %s\n", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "out", "", "write the bearer pairing bundle to an owner-only file")
	command.Flags().StringVar(&label, "label", "", "client display label")
	command.Flags().StringVar(&terminalID, "terminal", "", "limit the capability to one terminal instead of daemon-wide access")
	command.Flags().DurationVar(&lifetime, "ttl", 24*time.Hour, "capability lifetime")
	return command
}

func v3PairImportCommand() *cobra.Command {
	var endpointID string
	var label string
	var relayMode string
	var registryPath string
	command := &cobra.Command{
		Use:   "import <BUNDLE_FILE|->",
		Short: "Import a pairing bundle into credentials and endpoint registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := readV3PairingBundle(cmd.Context(), cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			bundle, _, err := remoteauth.ParsePairingBundle(payload, time.Time{})
			clear(payload)
			if err != nil {
				return err
			}
			id := connection.EndpointID(strings.TrimSpace(endpointID))
			endpointLabel := strings.TrimSpace(label)
			if endpointLabel == "" {
				endpointLabel = bundle.Label
			}
			if endpointLabel == "" {
				endpointLabel = string(id)
			}
			cfg := connection.Config{
				ID: id, Label: endpointLabel, Transport: connection.TransportHubP2P,
				ConnectMode: connection.ConnectOnDemand, Enabled: true, HubDeviceID: bundle.DeviceID,
				DeviceFingerprint: bundle.DeviceFingerprint, GrantRef: v3PairingGrantRef(id, bundle.DeviceID),
				RelayMode: connection.RelayMode(strings.TrimSpace(relayMode)),
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			registry, err := connection.Load(registryPath)
			if errors.Is(err, os.ErrNotExist) {
				registry = connection.DefaultRegistry()
				err = nil
			}
			if err != nil {
				return err
			}
			if registry.Connections == nil {
				registry.Connections = make(map[connection.EndpointID]connection.Config)
			}
			registry.Connections[id] = cfg
			if _, err := registry.Normalize(); err != nil {
				return err
			}
			credentials := remoteauth.NewCredentialStore(v3RemoteCredentialDir())
			previousGrant, previousGrantErr := credentials.Resolve(cfg.GrantRef)
			if previousGrantErr != nil && !errors.Is(previousGrantErr, os.ErrNotExist) {
				return previousGrantErr
			}
			if err := credentials.Put(cfg.GrantRef, bundle.CapabilityGrant); err != nil {
				return err
			}
			if err := saveV3ConnectionRegistry(registryPath, registry); err != nil {
				var rollbackErr error
				if previousGrantErr == nil {
					rollbackErr = credentials.Put(cfg.GrantRef, previousGrant)
				} else {
					rollbackErr = credentials.Delete(cfg.GrantRef)
				}
				if rollbackErr != nil {
					return errors.Join(err, fmt.Errorf("restore pairing credential: %w", rollbackErr))
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported managed endpoint %s for device %s\n", cfg.ID, cfg.HubDeviceID)
			return nil
		},
	}
	command.Flags().StringVar(&endpointID, "id", "", "client-local endpoint id")
	command.Flags().StringVar(&label, "label", "", "override the bundle display label")
	command.Flags().StringVar(&relayMode, "relay", string(connection.RelayAuto), "route policy: auto, direct, relay_only, or smart_route")
	command.Flags().StringVar(&registryPath, "registry", "", "connections.yaml path")
	_ = command.MarkFlagRequired("id")
	return command
}

func readV3PairingBundle(ctx context.Context, stdin io.Reader, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var reader io.Reader
	var file *os.File
	if strings.TrimSpace(path) == "-" {
		reader = stdin
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open pairing bundle: %w", err)
		}
		defer file.Close()
		reader = file
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxPairingBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read pairing bundle: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxPairingBundleBytes {
		clear(payload)
		return nil, fmt.Errorf("pairing bundle size is invalid")
	}
	return payload, nil
}

func v3PairingGrantRef(endpointID connection.EndpointID, deviceID string) string {
	digest := sha256.Sum256([]byte(string(endpointID) + "\x00" + strings.TrimSpace(deviceID)))
	return "managed-" + hex.EncodeToString(digest[:12])
}

func writeV3PrivateFile(path string, payload []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pairing-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	committed = true
	return nil
}
