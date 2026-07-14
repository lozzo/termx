package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/shared/connection"
	"github.com/lozzow/termx/shared/remoteauth"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const maxPairingBundleBytes = 1 << 20

var (
	saveV3ConnectionRegistry = connection.Save
	v3PairHostname           = os.Hostname
	v3PairOutputIsTerminal   = func(output io.Writer) bool {
		file, ok := output.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	}
)

func v3PairCommand() *cobra.Command {
	command := &cobra.Command{Use: "pair", Short: "Create or import a direct daemon capability"}
	command.AddCommand(v3PairCreateCommand())
	command.AddCommand(v3PairImportCommand())
	command.AddCommand(v3PairInspectCommand())
	return command
}

type pairInspectView struct {
	SchemaVersion     int              `json:"schema_version"`
	Kind              string           `json:"kind"`
	Version           uint32           `json:"version"`
	Label             string           `json:"label,omitempty"`
	DeviceID          string           `json:"device_id"`
	DeviceFingerprint string           `json:"device_fingerprint"`
	GrantID           string           `json:"grant_id"`
	RevocationID      string           `json:"revocation_id"`
	Scope             remoteauth.Scope `json:"scope"`
	IssuedAt          string           `json:"issued_at"`
	NotBefore         string           `json:"not_before"`
	ExpiresAt         string           `json:"expires_at"`
}

func v3PairInspectCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "inspect <BUNDLE_FILE|->", Short: "Verify pairing metadata without printing its bearer grant", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := readV3PairingBundle(cmd.Context(), cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			bundle, claims, err := remoteauth.ParsePairingBundle(payload, time.Time{})
			clear(payload)
			if err != nil {
				return err
			}
			view := pairInspectView{
				SchemaVersion: 1, Kind: "pairing_bundle", Version: bundle.Version, Label: bundle.Label,
				DeviceID: bundle.DeviceID, DeviceFingerprint: bundle.DeviceFingerprint,
				GrantID: claims.GrantID, RevocationID: claims.RevocationID, Scope: claims.Scope,
				IssuedAt: formatTerminalTime(claims.IssuedAt), NotBefore: formatTerminalTime(claims.NotBefore), ExpiresAt: formatTerminalTime(claims.ExpiresAt),
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Device: %s\nFingerprint: %s\nGrant ID: %s\nExpires: %s\n", view.DeviceID, view.DeviceFingerprint, view.GrantID, view.ExpiresAt)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print redacted machine-readable JSON")
	return command
}

func v3PairCreateCommand() *cobra.Command {
	var outputPath string
	var rawOutput bool
	var label string
	var terminalID string
	var lifetime time.Duration
	command := &cobra.Command{
		Use:   "create",
		Short: "Issue a pairing bundle from this daemon identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if rawOutput && strings.TrimSpace(outputPath) != "" {
				return usageCLIError("pair create --raw and --out cannot be used together")
			}
			identity, err := remoteauth.LoadOrCreateLocalIdentity(v3RemoteIdentityDir())
			if err != nil {
				return err
			}
			label = strings.TrimSpace(label)
			if label == "" {
				if hostname, hostnameErr := v3PairHostname(); hostnameErr == nil {
					label = strings.TrimSpace(hostname)
				}
			}
			scope := remoteauth.FullDaemonScope()
			if terminalID = strings.TrimSpace(terminalID); terminalID != "" {
				terminalID, err = localPairTerminalID(terminalID)
				if err != nil {
					return err
				}
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
			_, claims, err := remoteauth.ParsePairingBundle(payload, time.Time{})
			if err != nil {
				return err
			}
			if rawOutput {
				_, err = cmd.OutOrStdout().Write(payload)
				return err
			}
			if strings.TrimSpace(outputPath) == "" {
				if !v3PairOutputIsTerminal(cmd.OutOrStdout()) {
					return usageCLIError("pair create requires an interactive terminal; use --raw for stdout or --out FILE")
				}
				return renderV3PairingQR(cmd.OutOrStdout(), payload, claims.ExpiresAt)
			}
			if err := writeV3PrivateFile(outputPath, payload); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pairing bundle written to %s\n", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "out", "", "write the bearer pairing bundle to an owner-only file")
	command.Flags().BoolVar(&rawOutput, "raw", false, "write the bearer pairing bundle to stdout for explicit scripting")
	command.Flags().StringVar(&label, "label", "", "daemon display label (defaults to this host name)")
	command.Flags().StringVar(&terminalID, "terminal", "", "limit the capability to one terminal instead of daemon-wide access")
	command.Flags().DurationVar(&lifetime, "ttl", 24*time.Hour, "capability lifetime")
	return command
}

// renderV3PairingQR 把 bearer bundle 只编码进高对比度终端二维码，不回显原始 grant。
// 二维码等价于短期凭据；调用方应在可信终端展示并在扫描后清屏。
func renderV3PairingQR(output io.Writer, payload []byte, expiresAt time.Time) error {
	code, err := qrcode.New(string(payload), qrcode.Medium)
	if err != nil {
		return fmt.Errorf("encode pairing QR: %w", err)
	}
	bitmap := code.Bitmap()
	if len(bitmap) == 0 {
		return fmt.Errorf("encode pairing QR: empty bitmap")
	}
	if _, err = fmt.Fprintf(output, "Scan with the TermX App\nExpires: %s\n", formatTerminalTime(expiresAt)); err != nil {
		return err
	}
	for row := 0; row < len(bitmap); row += 2 {
		for column := range bitmap[row] {
			top := bitmap[row][column]
			bottom := false
			if row+1 < len(bitmap) {
				bottom = bitmap[row+1][column]
			}
			switch {
			case top && bottom:
				_, err = io.WriteString(output, "\x1b[40m  ")
			case !top && !bottom:
				_, err = io.WriteString(output, "\x1b[47m  ")
			case top:
				_, err = io.WriteString(output, "\x1b[30;47m▀▀")
			default:
				_, err = io.WriteString(output, "\x1b[30;47m▄▄")
			}
			if err != nil {
				return err
			}
		}
		if _, err = io.WriteString(output, "\x1b[0m\n"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(output, "\x1b[0mThis QR grants daemon access. Clear the terminal after scanning.\n")
	return err
}

func localPairTerminalID(target string) (string, error) {
	target = strings.TrimSpace(target)
	if endpointID, terminalID, found := strings.Cut(target, ":"); found {
		if endpointID != string(connection.DefaultEndpointID) || terminalID == "" || strings.Contains(terminalID, ":") {
			return "", usageCLIError("pair create can only scope the local daemon target local:TERMINAL_ID")
		}
		return terminalID, nil
	}
	if target == "" {
		return "", usageCLIError("terminal target cannot be empty")
	}
	return target, nil
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
