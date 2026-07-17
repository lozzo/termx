package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	endpointdomain "github.com/lozzow/termx/client/endpoint"
	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/proto/apipb"
	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/remoteauth"
	unixtransport "github.com/lozzow/termx/shared/transport/unix"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const pairingBootstrapURIPrefix = "termx://bootstrap?payload="

var (
	updateV3ConnectionRegistry = endpointdomain.UpdateContext
	dialV3PairingTransport     = unixtransport.DialContext
	v3PairHostname             = os.Hostname
	v3PairOutputIsTerminal     = func(output io.Writer) bool {
		file, ok := output.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	}
)

func v3PairCommand(socket *string, logFile *string) *cobra.Command {
	command := &cobra.Command{Use: "pair", Short: "Create or redeem a client-bound daemon pairing ticket"}
	command.AddCommand(v3PairCreateCommand(socket, logFile))
	command.AddCommand(v3PairImportCommand(socket, logFile))
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
	TicketID          string           `json:"ticket_id"`
	ScopeCeiling      remoteauth.Scope `json:"scope_ceiling"`
	IssuedAt          string           `json:"issued_at"`
	NotBefore         string           `json:"not_before"`
	ExpiresAt         string           `json:"expires_at"`
	GrantLifetime     int64            `json:"grant_lifetime_seconds"`
}

func v3PairInspectCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "inspect <BUNDLE_FILE|->", Short: "Verify one-time pairing ticket metadata", Args: cobra.ExactArgs(1),
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
				SchemaVersion: 1, Kind: "pairing_bundle", Version: bundle.GetSchemaVersion(), Label: bundle.GetSuggestedLabel(),
				DeviceID: bundle.GetIdentity().GetDeviceId(), DeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint(),
				TicketID: claims.TicketID, ScopeCeiling: claims.ScopeCeiling, GrantLifetime: claims.GrantLifetimeSeconds,
				IssuedAt: formatTerminalTime(claims.IssuedAt), NotBefore: formatTerminalTime(claims.NotBefore), ExpiresAt: formatTerminalTime(claims.ExpiresAt),
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Device: %s\nFingerprint: %s\nTicket ID: %s\nExpires: %s\n", view.DeviceID, view.DeviceFingerprint, view.TicketID, view.ExpiresAt)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print redacted machine-readable JSON")
	return command
}

func v3PairCreateCommand(socket *string, logFile *string) *cobra.Command {
	var outputPath string
	var rawOutput bool
	var label string
	var terminalID string
	var ticketTTL time.Duration
	var grantLifetime time.Duration
	command := &cobra.Command{
		Use:   "create",
		Short: "Issue a short-lived one-time pairing ticket from the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if rawOutput && strings.TrimSpace(outputPath) != "" {
				return usageCLIError("pair create --raw and --out cannot be used together")
			}
			client, err := dialOrStartV3ClientContext(cmd.Context(), resolveV3Socket(*socket), resolveV3LogFilePath(*logFile), nil)
			if err != nil {
				return err
			}
			defer client.Close()
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
			if ticketTTL <= 0 || grantLifetime <= 0 {
				return usageCLIError("pair create ticket and grant ttl must be positive")
			}
			application, err := newLocalApplicationSession(client)
			if err != nil {
				return err
			}
			response, err := application.ClientAccessTicketCreate(cmd.Context(), &apipb.ClientAccessTicketCreateCommand{Request: &remoteauthpb.ClientAccessTicketCreateRequest{Label: label, Scope: clientAccessScopeToProto(scope), TicketTtlSeconds: int64(ticketTTL / time.Second), GrantLifetimeSeconds: int64(grantLifetime / time.Second)}})
			if err != nil {
				return err
			}
			result := response.GetTicket()
			payload := result.GetBundle()
			if rawOutput {
				_, err = cmd.OutOrStdout().Write(payload)
				return err
			}
			if strings.TrimSpace(outputPath) == "" {
				if !v3PairOutputIsTerminal(cmd.OutOrStdout()) {
					return usageCLIError("pair create requires an interactive terminal; use --raw for stdout or --out FILE")
				}
				return renderV3PairingQR(cmd.OutOrStdout(), payload, time.Unix(0, result.GetExpiresAtUnixNano()).UTC())
			}
			if err := writeV3PrivateFile(outputPath, payload); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pairing bundle written to %s\n", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "out", "", "write the one-time pairing bundle to an owner-only file")
	command.Flags().BoolVar(&rawOutput, "raw", false, "write the one-time pairing bundle to stdout for explicit scripting")
	command.Flags().StringVar(&label, "label", "", "daemon display label (defaults to this host name)")
	command.Flags().StringVar(&terminalID, "terminal", "", "limit the capability to one terminal instead of daemon-wide access")
	command.Flags().DurationVar(&ticketTTL, "ttl", 10*time.Minute, "one-time ticket lifetime")
	command.Flags().DurationVar(&grantLifetime, "grant-ttl", 90*24*time.Hour, "bound capability lifetime")
	return command
}

func clientAccessScopeToProto(scope remoteauth.Scope) *remoteauthpb.ClientAccessScope {
	return &remoteauthpb.ClientAccessScope{AllowDaemon: scope.AllowDaemon, TerminalId: scope.TerminalID, MachineEventsOnly: scope.MachineEventsOnly, FileReadMetadata: scope.FileReadMetadata, FileReadContent: scope.FileReadContent, FileWriteContent: scope.FileWriteContent, FileMutate: scope.FileMutate, ManageClientAccess: scope.ManageClientAccess}
}

// renderV3PairingQR 把短期一次性 PairingTicket bundle 编码进高对比度终端二维码。
// 二维码不包含长期 grant，但仍允许在有效期内发起一次 key-bound 兑换；调用方应在扫描后清屏。
func renderV3PairingQR(output io.Writer, payload []byte, expiresAt time.Time) error {
	portablePayload := pairingBootstrapURIPrefix + base64.RawURLEncoding.EncodeToString(payload)
	code, err := qrcode.New(portablePayload, qrcode.Medium)
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
	_, err = io.WriteString(output, "\x1b[0mThis QR contains a one-time pairing ticket. Clear the terminal after scanning.\n")
	return err
}

func localPairTerminalID(target string) (string, error) {
	target = strings.TrimSpace(target)
	if endpointID, terminalID, found := strings.Cut(target, ":"); found {
		if endpointID != string(endpointdomain.DefaultEndpointID) || terminalID == "" || strings.Contains(terminalID, ":") {
			return "", usageCLIError("pair create can only scope the local daemon target local:TERMINAL_ID")
		}
		return terminalID, nil
	}
	if target == "" {
		return "", usageCLIError("terminal target cannot be empty")
	}
	return target, nil
}

func v3PairImportCommand(socket *string, logFile *string) *cobra.Command {
	var endpointID string
	var label string
	var registryPath string
	var pairingSocket string
	var clientLabel string
	var allowScopeExpansion bool
	command := &cobra.Command{
		Use:   "import <BUNDLE_FILE|->",
		Short: "Redeem a one-time ticket into client-bound credentials and endpoint registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := readV3PairingBundle(cmd.Context(), cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			defer clear(payload)
			bundle, _, err := remoteauth.ParsePairingBundleForExchange(payload)
			if err != nil {
				return err
			}
			id := endpointdomain.EndpointID(strings.TrimSpace(endpointID))
			endpointLabel := strings.TrimSpace(label)
			if endpointLabel == "" {
				endpointLabel = bundle.GetSuggestedLabel()
			}
			if endpointLabel == "" {
				endpointLabel = string(id)
			}
			identity := endpointdomain.DaemonIdentity{DeviceID: bundle.GetIdentity().GetDeviceId(), DeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint()}
			var endpoint endpointdomain.Endpoint
			var credential remoteauth.ClientAccessCredential
			_, err = updateV3ConnectionRegistry(cmd.Context(), registryPath, true, func(registry endpointdomain.Registry) (endpointdomain.Registry, error) {
				updated, mergedEndpoint, grantRef, mergeErr := mergePairingEndpoint(registry, id, endpointLabel, bundle)
				if mergeErr != nil {
					return endpointdomain.Registry{}, mergeErr
				}
				credentials := remoteauth.NewCredentialStore(v3RemoteCredentialDir())
				bound, bindErr := credentials.PairAndBind(
					cmd.Context(), grantRef, string(mergedEndpoint.ID), payload, nil, nil,
					remoteauth.BindGrantOptions{AllowScopeExpansion: allowScopeExpansion},
					func(clientIdentity remoteauth.ClientAccessIdentity) (remoteauth.PairingExchangeResult, error) {
						resolvedPairingSocket := strings.TrimSpace(pairingSocket)
						if resolvedPairingSocket == "" {
							localSocket := resolveV3Socket(*socket)
							client, startErr := dialOrStartV3ClientContext(cmd.Context(), localSocket, resolveV3LogFilePath(*logFile), nil)
							if startErr != nil {
								return remoteauth.PairingExchangeResult{}, startErr
							}
							_ = client.Close()
							resolvedPairingSocket = v3PairingSocketPath(localSocket)
						}
						binding, bindingErr := remoteauth.LocalUnixChannelBinding(resolvedPairingSocket)
						if bindingErr != nil {
							return remoteauth.PairingExchangeResult{}, bindingErr
						}
						pairingTransport, dialErr := dialV3PairingTransport(cmd.Context(), resolvedPairingSocket)
						if dialErr != nil {
							return remoteauth.PairingExchangeResult{}, fmt.Errorf("connect PairingExchange socket: %w", dialErr)
						}
						defer pairingTransport.Close()
						labelForClient := strings.TrimSpace(clientLabel)
						if labelForClient == "" {
							labelForClient = "termx-cli"
							if hostname, hostnameErr := v3PairHostname(); hostnameErr == nil && strings.TrimSpace(hostname) != "" {
								labelForClient = "termx-cli@" + strings.TrimSpace(hostname)
							}
						}
						return (remoteauth.ClientPairingHandshake{}).Redeem(cmd.Context(), pairingTransport, remoteauth.ClientPairingRequest{
							ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.DeviceFingerprint,
							PairingBundle: payload, Identity: clientIdentity,
							ClientLabel: labelForClient, ChannelBinding: binding,
						})
					},
				)
				if bindErr != nil {
					return endpointdomain.Registry{}, bindErr
				}
				endpoint = mergedEndpoint
				credential = bound
				return updated, nil
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Paired endpoint %s with daemon %s using client key %s\n", endpoint.ID, endpoint.DaemonIdentity.DeviceID, credential.Identity.Fingerprint)
			return nil
		},
	}
	command.Flags().StringVar(&endpointID, "id", "", "client-local endpoint id")
	command.Flags().StringVar(&label, "label", "", "override the bundle display label")
	command.Flags().StringVar(&registryPath, "registry", "", "connections.yaml path")
	command.Flags().StringVar(&pairingSocket, "pair-socket", "", "owner-only PairingExchange Unix socket (defaults to local daemon)")
	command.Flags().StringVar(&clientLabel, "client-label", "", "label recorded for this client access key")
	command.Flags().BoolVar(&allowScopeExpansion, "allow-scope-expansion", false, "confirm replacing an existing credential with a broader capability scope")
	_ = command.MarkFlagRequired("id")
	return command
}

func mergePairingEndpoint(
	registry endpointdomain.Registry,
	preferredID endpointdomain.EndpointID,
	label string,
	bundle *remoteauth.PairingBundle,
) (endpointdomain.Registry, endpointdomain.Endpoint, string, error) {
	candidate, err := endpointdomain.EndpointCandidateFromBootstrapBundle(bundle)
	if err != nil {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", err
	}
	identity := candidate.Identity
	if strings.TrimSpace(label) != "" {
		candidate.SuggestedLabel = strings.TrimSpace(label)
	}
	normalized, err := registry.Normalize()
	if err != nil {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", err
	}
	registry = normalized
	actualID := preferredID
	target, targetExists := registry.Endpoints[preferredID]
	if targetExists && !target.DaemonIdentity.Empty() && target.DaemonIdentity != identity {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("endpoint %q is pinned to a different daemon identity", preferredID)
	}
	if !targetExists {
		for _, endpoint := range registry.List() {
			if endpoint.DaemonIdentity == identity {
				actualID = endpoint.ID
				target, targetExists = endpoint, true
				break
			}
		}
	}
	grantRef := v3PairingGrantRef(actualID, identity.DeviceID)
	if targetExists {
		for _, route := range target.Routes {
			if route.Kind != endpointdomain.RouteDirectTLS && route.Kind != endpointdomain.RouteManagedWebRTC || strings.TrimSpace(route.CredentialRef) == "" {
				continue
			}
			if grantRef != v3PairingGrantRef(actualID, identity.DeviceID) && grantRef != route.CredentialRef {
				return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("endpoint %q has conflicting capability credential refs", actualID)
			}
			grantRef = route.CredentialRef
		}
	}
	if !targetExists && len(candidate.Routes) == 0 {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("pairing bundle contains no portable route and no existing endpoint matches daemon %q", identity.DeviceID)
	}
	for index := range candidate.Routes {
		if candidate.Routes[index].Kind == endpointdomain.RouteDirectTLS || candidate.Routes[index].Kind == endpointdomain.RouteManagedWebRTC {
			candidate.Routes[index].CredentialRef = grantRef
		}
	}
	input := endpointdomain.EndpointAssemblerInput{Registry: registry, Candidates: []endpointdomain.EndpointCandidate{candidate}}
	if targetExists && target.DaemonIdentity.Empty() {
		input.ConfirmedIdentityBindings = []endpointdomain.ConfirmedIdentityBinding{{EndpointID: actualID, Identity: identity}}
	}
	result, err := endpointdomain.AssembleEndpoints(input)
	if err != nil {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", err
	}
	resolvedID := result.ResolvedEndpointIDs[0]
	if targetExists && resolvedID != actualID {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("pairing identity resolved to endpoint %q instead of confirmed endpoint %q", resolvedID, actualID)
	}
	if !targetExists && resolvedID != actualID {
		endpoint := result.Registry.Endpoints[resolvedID]
		delete(result.Registry.Endpoints, resolvedID)
		endpoint.ID = actualID
		result.Registry.Endpoints[actualID] = endpoint
		if result.Registry.Default == resolvedID {
			result.Registry.Default = actualID
		}
		result.Registry, err = result.Registry.Normalize()
		if err != nil {
			return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", err
		}
		resolvedID = actualID
	}
	endpoint := result.Registry.Endpoints[resolvedID]
	authRoutes := 0
	for routeID, route := range endpoint.Routes {
		if route.Kind != endpointdomain.RouteDirectTLS && route.Kind != endpointdomain.RouteManagedWebRTC {
			continue
		}
		if strings.TrimSpace(route.CredentialRef) != "" && route.CredentialRef != grantRef {
			return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("endpoint %q route %q uses a different capability credential ref", resolvedID, routeID)
		}
		route.CredentialRef = grantRef
		endpoint.Routes[routeID] = route
		authRoutes++
	}
	if authRoutes == 0 {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("endpoint %q has no direct-tls or managed-webrtc route that can use the paired capability", resolvedID)
	}
	result.Registry.Endpoints[resolvedID] = endpoint
	result.Registry, err = result.Registry.Normalize()
	if err != nil {
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", err
	}
	endpoint = result.Registry.Endpoints[resolvedID]
	return result.Registry, endpoint, grantRef, nil
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
	payload, err := io.ReadAll(io.LimitReader(reader, endpointdomain.MaxPortableContractBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read pairing bundle: %w", err)
	}
	if len(payload) == 0 || len(payload) > endpointdomain.MaxPortableContractBytes {
		clear(payload)
		return nil, fmt.Errorf("pairing bundle size is invalid")
	}
	if text := strings.TrimSpace(string(payload)); strings.HasPrefix(text, pairingBootstrapURIPrefix) {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(text, pairingBootstrapURIPrefix))
		clear(payload)
		if decodeErr != nil || len(decoded) == 0 || len(decoded) > endpointdomain.MaxPortableContractBytes {
			clear(decoded)
			return nil, fmt.Errorf("pairing bootstrap URI payload is invalid")
		}
		payload = decoded
	}
	return payload, nil
}

func v3PairingGrantRef(endpointID endpointdomain.EndpointID, deviceID string) string {
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
