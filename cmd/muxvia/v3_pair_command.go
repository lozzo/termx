package main

import (
	"context"
	"crypto/ed25519"
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

	endpointdomain "github.com/muxvia/muxvia/client/endpoint"
	"github.com/muxvia/muxvia/proto/apipb"
	"github.com/muxvia/muxvia/proto/remoteauthpb"
	"github.com/muxvia/muxvia/shared/filepublish"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"github.com/muxvia/muxvia/shared/securefs"
	unixtransport "github.com/muxvia/muxvia/shared/transport/unix"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const pairingBootstrapURIPrefix = "muxvia://bootstrap?payload="

var (
	updateV3ConnectionRegistry = endpointdomain.UpdateContext
	dialV3PairingTransport     = unixtransport.DialContext
	v3PairHostname             = os.Hostname
	v3PairOutputIsTerminal     = func(output io.Writer) bool {
		file, ok := output.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	}
	v3PairTerminalSize = func(output io.Writer) (columns int, rows int, ok bool) {
		file, ok := output.(*os.File)
		if !ok {
			return 0, 0, false
		}
		columns, rows, err := term.GetSize(int(file.Fd()))
		return columns, rows, err == nil
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
	Routes            []pairRouteView  `json:"routes"`
}

type pairRouteView struct {
	RouteID             string   `json:"route_id"`
	Kind                string   `json:"kind"`
	SignalingAddresses  []string `json:"signaling_addresses,omitempty"`
	ICETCPAddresses     []string `json:"ice_tcp_addresses,omitempty"`
	AdvertisedAddresses []string `json:"advertised_addresses,omitempty"`
	ServerName          string   `json:"server_name,omitempty"`
	TargetDeviceID      string   `json:"target_device_id,omitempty"`
	RelayMode           string   `json:"relay_mode,omitempty"`
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
				Routes: pairRouteViews(bundle),
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
	var qrOutputPath string
	var rawOutput bool
	var textOutput bool
	var label string
	var terminalID string
	var ticketTTL time.Duration
	var grantLifetime time.Duration
	var routeSpecs []string
	var directID string
	var directName string
	var signalingAddresses []string
	var iceTCPAddresses []string
	var serverName string
	var sshID string
	var sshName string
	var sshHost string
	var sshPort uint16
	var sshUser string
	var sshHostKeys []string
	command := &cobra.Command{
		Use:   "create",
		Short: "Issue a short-lived one-time pairing ticket from the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			socketPath := resolveV3Socket(*socket)
			selectedOutputs := 0
			for _, selected := range []bool{rawOutput, textOutput, strings.TrimSpace(outputPath) != "", strings.TrimSpace(qrOutputPath) != ""} {
				if selected {
					selectedOutputs++
				}
			}
			if selectedOutputs > 1 {
				return usageCLIError("pair create --raw, --text, --out, and --qr-file are mutually exclusive")
			}
			client, err := dialOrStartV3ClientContext(cmd.Context(), socketPath, resolveV3LogFilePath(*logFile), nil)
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
			routes, err := v3PairingRoutes(v3PairRouteFlags{
				Routes: routeSpecs, DirectID: directID, DirectName: directName, SignalingAddresses: signalingAddresses,
				ICETCPAddresses: iceTCPAddresses, ServerName: serverName, SSHID: sshID, SSHName: sshName,
				SSHHost: sshHost, SSHPort: sshPort, SSHUser: sshUser, SSHHostKeys: sshHostKeys,
			})
			if err != nil {
				return usageCLIError(err.Error())
			}
			response, err := application.ClientAccessTicketCreate(cmd.Context(), &apipb.ClientAccessTicketCreateCommand{Request: &remoteauthpb.ClientAccessTicketCreateRequest{
				Label: label, Scope: clientAccessScopeToProto(scope), TicketTtlSeconds: int64(ticketTTL / time.Second), GrantLifetimeSeconds: int64(grantLifetime / time.Second),
				Routes: routes,
			}})
			if err != nil {
				return err
			}
			result := response.GetTicket()
			payload := result.GetClaimOffer()
			if len(payload) == 0 {
				return fmt.Errorf("daemon did not return a pairing claim offer")
			}
			if rawOutput {
				_, err = cmd.OutOrStdout().Write(result.GetBundle())
				return err
			}
			portablePayload := v3PairingBootstrapURI(payload)
			if textOutput {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), portablePayload)
				return err
			}
			if strings.TrimSpace(qrOutputPath) != "" {
				png, pngErr := renderV3PairingPNG(portablePayload)
				if pngErr != nil {
					return pngErr
				}
				if err := renderV3PairingPreview(cmd.OutOrStdout(), payload, time.Unix(0, result.GetExpiresAtUnixNano()).UTC()); err != nil {
					return err
				}
				if err := writeV3PrivateFile(qrOutputPath, png); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Pairing QR PNG written to %s\n", qrOutputPath)
				return nil
			}
			if strings.TrimSpace(outputPath) == "" {
				if !v3PairOutputIsTerminal(cmd.OutOrStdout()) {
					return usageCLIError("pair create requires an interactive terminal; use --text, --qr-file FILE, --raw, or --out FILE")
				}
				return renderV3PairingQR(cmd.OutOrStdout(), payload, time.Unix(0, result.GetExpiresAtUnixNano()).UTC())
			}
			if err := renderV3PairingPreview(cmd.OutOrStdout(), payload, time.Unix(0, result.GetExpiresAtUnixNano()).UTC()); err != nil {
				return err
			}
			if err := writeV3PrivateFile(outputPath, result.GetBundle()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pairing bundle written to %s\n", outputPath)
			return nil
		},
	}
	command.Flags().StringVar(&outputPath, "out", "", "write the one-time pairing bundle to an owner-only file")
	command.Flags().StringVar(&qrOutputPath, "qr-file", "", "write a square pairing QR PNG to an owner-only file")
	command.Flags().BoolVar(&rawOutput, "raw", false, "write the one-time pairing bundle to stdout for explicit owner scripting")
	command.Flags().BoolVar(&textOutput, "text", false, "write the portable pairing URI to stdout for copying")
	command.Flags().StringVar(&label, "label", "", "daemon display label (defaults to this host name)")
	command.Flags().StringVar(&terminalID, "terminal", "", "limit the capability to one terminal instead of daemon-wide access")
	command.Flags().DurationVar(&ticketTTL, "ttl", 10*time.Minute, "one-time ticket lifetime")
	command.Flags().DurationVar(&grantLifetime, "grant-ttl", 90*24*time.Hour, "bound capability lifetime")
	command.Flags().StringArrayVar(&routeSpecs, "route", nil, "pairing Route: direct, ssh, or a strict Route URI (repeatable)")
	command.Flags().StringVar(&directID, "direct-id", "", "stable ID suffix for the parameterized Direct Route")
	command.Flags().StringVar(&directName, "direct-name", "", "display name for the parameterized Direct Route")
	command.Flags().StringArrayVar(&signalingAddresses, "signaling-address", nil, "published Direct signaling HOST:PORT (repeatable; requires --ice-tcp-address)")
	command.Flags().StringArrayVar(&iceTCPAddresses, "ice-tcp-address", nil, "published Direct ICE-TCP HOST:PORT (repeatable; requires --signaling-address)")
	command.Flags().StringVar(&serverName, "server-name", "", "optional Direct server name bound into the signed Route hint")
	command.Flags().StringVar(&sshID, "ssh-id", "", "stable ID suffix for the parameterized SSH Route")
	command.Flags().StringVar(&sshName, "ssh-name", "", "display name for the parameterized SSH Route")
	command.Flags().StringVar(&sshHost, "ssh-host", "", "SSH server host for the parameterized SSH Route")
	command.Flags().Uint16Var(&sshPort, "ssh-port", 0, "SSH server port (defaults to 22)")
	command.Flags().StringVar(&sshUser, "ssh-user", "", "SSH user for the parameterized SSH Route")
	command.Flags().StringArrayVar(&sshHostKeys, "ssh-host-key", nil, "pinned SSH SHA256 host-key fingerprint (repeatable)")
	return command
}

func clientAccessScopeToProto(scope remoteauth.Scope) *remoteauthpb.ClientAccessScope {
	return &remoteauthpb.ClientAccessScope{AllowDaemon: scope.AllowDaemon, TerminalId: scope.TerminalID, MachineEventsOnly: scope.MachineEventsOnly, FileReadMetadata: scope.FileReadMetadata, FileReadContent: scope.FileReadContent, FileWriteContent: scope.FileWriteContent, FileMutate: scope.FileMutate, ManageClientAccess: scope.ManageClientAccess}
}

// renderV3PairingQR 把 daemon 内存持有的短期一次性 claim 编码进高对比度终端二维码。
// 二维码不包含 ticket、scope 或 grant；调用方仍应在扫描后清屏，避免 claim 在有效期内被旁观者使用。
func renderV3PairingQR(output io.Writer, payload []byte, expiresAt time.Time) error {
	portablePayload := v3PairingBootstrapURI(payload)
	code, err := qrcode.New(portablePayload, qrcode.Low)
	if err != nil {
		return fmt.Errorf("encode pairing QR: %w", err)
	}
	bitmap := code.Bitmap()
	if len(bitmap) == 0 {
		return fmt.Errorf("encode pairing QR: empty bitmap")
	}
	var preview strings.Builder
	if err := renderV3PairingPreview(&preview, payload, expiresAt); err != nil {
		return err
	}
	requiredColumns := len(bitmap)
	for _, line := range strings.Split(preview.String(), "\n") {
		if len(line) > requiredColumns {
			requiredColumns = len(line)
		}
	}
	for _, line := range []string{
		"Scan with the Muxvia App",
		"This QR contains a one-time pairing claim. Clear the terminal after scanning.",
	} {
		if len(line) > requiredColumns {
			requiredColumns = len(line)
		}
	}
	requiredRows := strings.Count(preview.String(), "\n") + 1 + (len(bitmap)+1)/2 + 1
	if columns, rows, ok := v3PairTerminalSize(output); ok && (columns < requiredColumns || rows < requiredRows) {
		return usageCLIError(fmt.Sprintf(
			"pairing QR requires at least %dx%d terminal cells (current %dx%d); enlarge the terminal or use --text or --qr-file FILE",
			requiredColumns, requiredRows, columns, rows,
		))
	}
	if _, err := io.WriteString(output, preview.String()); err != nil {
		return err
	}
	if _, err = io.WriteString(output, "Scan with the Muxvia App\n"); err != nil {
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
				_, err = io.WriteString(output, "\x1b[40m ")
			case !top && !bottom:
				_, err = io.WriteString(output, "\x1b[47m ")
			case top:
				_, err = io.WriteString(output, "\x1b[30;47m▀")
			default:
				_, err = io.WriteString(output, "\x1b[30;47m▄")
			}
			if err != nil {
				return err
			}
		}
		if _, err = io.WriteString(output, "\x1b[0m\n"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(output, "\x1b[0mThis QR contains a one-time pairing claim. Clear the terminal after scanning.\n")
	return err
}

func v3PairingBootstrapURI(payload []byte) string {
	return remoteauth.EncodePairingClaimCode(payload)
}

// renderV3PairingPNG 生成带 quiet zone 的正方形位图，供无法完整显示终端二维码时离线展示。
func renderV3PairingPNG(portablePayload string) ([]byte, error) {
	code, err := qrcode.New(portablePayload, qrcode.Low)
	if err != nil {
		return nil, fmt.Errorf("encode pairing QR: %w", err)
	}
	png, err := code.PNG(1024)
	if err != nil {
		return nil, fmt.Errorf("encode pairing QR PNG: %w", err)
	}
	return png, nil
}

func renderV3PairingPreview(output io.Writer, payload []byte, expiresAt time.Time) error {
	offer, err := remoteauth.ParsePairingClaimOffer(payload, time.Now().UTC())
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Device: %s\nFingerprint: %s\nExpires: %s\n",
		offer.GetDeviceId(), remoteauth.Fingerprint(ed25519.PublicKey(offer.GetDevicePublicKey())), formatTerminalTime(expiresAt)); err != nil {
		return err
	}
	for _, route := range offer.GetRoutes() {
		if direct := route.GetDirectWebrtcTcp(); direct != nil {
			if _, err = fmt.Fprintf(output, "Route %s: direct signaling=%s ice-tcp=%s\n", route.GetRouteId(), direct.GetSignalingAddress(), direct.GetIceTcpAddress()); err != nil {
				return err
			}
			continue
		}
		if managed := route.GetManagedWebrtc(); managed != nil {
			if _, err = fmt.Fprintf(output, "Route %s: cloud target=%s\n", route.GetRouteId(), managed.GetTargetDeviceId()); err != nil {
				return err
			}
			continue
		}
		if ssh := route.GetSshWebrtcTcp(); ssh != nil {
			if _, err = fmt.Fprintf(output, "Route %s: ssh %s@%s:%d\n", route.GetRouteId(), ssh.GetUser(), ssh.GetHost(), ssh.GetPort()); err != nil {
				return err
			}
		}
	}
	return nil
}

func pairRouteViews(bundle *remoteauth.PairingBundle) []pairRouteView {
	views := make([]pairRouteView, 0, len(bundle.GetRoutes()))
	for _, route := range bundle.GetRoutes() {
		if direct := route.GetDirectWebrtcTcp(); direct != nil {
			views = append(views, pairRouteView{
				RouteID: route.GetRouteId(), Kind: string(endpointdomain.RouteDirectWebRTCTCP),
				SignalingAddresses:  append([]string(nil), direct.GetSignalingAddresses()...),
				ICETCPAddresses:     append([]string(nil), direct.GetIceTcpAddresses()...),
				AdvertisedAddresses: append([]string(nil), direct.GetAdvertisedAddresses()...), ServerName: direct.GetServerName(),
			})
		}
		if managed := route.GetManagedWebrtc(); managed != nil {
			views = append(views, pairRouteView{
				RouteID: route.GetRouteId(), Kind: string(endpointdomain.RouteManagedWebRTC),
				TargetDeviceID: managed.GetTargetDeviceId(), RelayMode: managed.GetRelayMode().String(),
			})
		}
	}
	return views
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
		Use:   "import <CLAIM_FILE|->",
		Short: "Redeem a one-time claim into client-bound credentials and endpoint registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := readV3PairingBundle(cmd.Context(), cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			defer clear(payload)
			bundle, _, bundleErr := remoteauth.ParsePairingBundleForExchange(payload)
			claimMode := bundleErr != nil
			var identity endpointdomain.DaemonIdentity
			if claimMode {
				offer, claimErr := remoteauth.ParsePairingClaimOfferForExchange(payload)
				if claimErr != nil {
					return bundleErr
				}
				identity = endpointdomain.DaemonIdentity{DeviceID: offer.GetDeviceId(), DeviceFingerprint: remoteauth.Fingerprint(ed25519.PublicKey(offer.GetDevicePublicKey()))}
			} else {
				identity = endpointdomain.DaemonIdentity{DeviceID: bundle.GetIdentity().GetDeviceId(), DeviceFingerprint: bundle.GetIdentity().GetDeviceFingerprint()}
			}
			id := endpointdomain.EndpointID(strings.TrimSpace(endpointID))
			endpointLabel := strings.TrimSpace(label)
			if endpointLabel == "" && !claimMode {
				endpointLabel = bundle.GetSuggestedLabel()
			}
			if endpointLabel == "" {
				endpointLabel = string(id)
			}
			var endpoint endpointdomain.Endpoint
			var credential remoteauth.ClientAccessCredential
			_, err = updateV3ConnectionRegistry(cmd.Context(), registryPath, true, func(registry endpointdomain.Registry) (endpointdomain.Registry, error) {
				normalized, normalizeErr := registry.Normalize()
				if normalizeErr != nil {
					return endpointdomain.Registry{}, normalizeErr
				}
				actualID := id
				if existing, exists := normalized.Endpoints[id]; exists && !existing.DaemonIdentity.Empty() && existing.DaemonIdentity != identity {
					return endpointdomain.Registry{}, fmt.Errorf("endpoint %q is pinned to a different daemon identity", id)
				} else if !exists {
					for _, existing := range normalized.List() {
						if existing.DaemonIdentity == identity {
							actualID = existing.ID
							break
						}
					}
				}
				grantRef := v3PairingGrantRef(actualID, identity.DeviceID)
				credentials := remoteauth.NewCredentialStore(v3RemoteCredentialDir())
				var exchangedBundle []byte
				if !claimMode {
					exchangedBundle = append([]byte(nil), payload...)
				}
				bound, bindErr := credentials.PairAndBind(
					cmd.Context(), grantRef, string(actualID), payload, nil, nil,
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
							labelForClient = "muxvia-cli"
							if hostname, hostnameErr := v3PairHostname(); hostnameErr == nil && strings.TrimSpace(hostname) != "" {
								labelForClient = "muxvia-cli@" + strings.TrimSpace(hostname)
							}
						}
						pairingRequest := remoteauth.ClientPairingRequest{
							ExpectedDeviceID: identity.DeviceID, ExpectedDeviceFingerprint: identity.DeviceFingerprint,
							Identity: clientIdentity, ClientLabel: labelForClient, ChannelBinding: binding,
						}
						if claimMode {
							pairingRequest.PairingClaimOffer = payload
						} else {
							pairingRequest.PairingBundle = payload
						}
						result, redeemErr := (remoteauth.ClientPairingHandshake{}).Redeem(cmd.Context(), pairingTransport, pairingRequest)
						exchangedBundle = append([]byte(nil), result.Bundle...)
						return result, redeemErr
					},
				)
				if bindErr != nil {
					return endpointdomain.Registry{}, bindErr
				}
				bundle, _, parseErr := remoteauth.ParsePairingBundleForExchange(exchangedBundle)
				if parseErr != nil {
					return endpointdomain.Registry{}, parseErr
				}
				updated, mergedEndpoint, mergedGrantRef, mergeErr := mergePairingEndpoint(normalized, actualID, endpointLabel, bundle)
				if mergeErr != nil || mergedGrantRef != grantRef {
					if mergeErr != nil {
						return endpointdomain.Registry{}, mergeErr
					}
					return endpointdomain.Registry{}, fmt.Errorf("pairing endpoint resolved a different credential reference")
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
	command.Flags().StringVar(&label, "label", "", "override the daemon display label")
	command.Flags().StringVar(&registryPath, "registry", "", "endpoint registry path (default: XDG config dir endpoints.yaml)")
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
			if route.Kind != endpointdomain.RouteDirectWebRTCTCP && route.Kind != endpointdomain.RouteSSHWebRTCTCP && route.Kind != endpointdomain.RouteManagedWebRTC || strings.TrimSpace(route.CredentialRef) == "" {
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
		if candidate.Routes[index].Kind == endpointdomain.RouteDirectWebRTCTCP || candidate.Routes[index].Kind == endpointdomain.RouteSSHWebRTCTCP || candidate.Routes[index].Kind == endpointdomain.RouteManagedWebRTC {
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
		if route.Kind != endpointdomain.RouteDirectWebRTCTCP && route.Kind != endpointdomain.RouteSSHWebRTCTCP && route.Kind != endpointdomain.RouteManagedWebRTC {
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
		return endpointdomain.Registry{}, endpointdomain.Endpoint{}, "", fmt.Errorf("endpoint %q has no WebRTC route that can use the paired capability", resolvedID)
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
	} else if strings.HasPrefix(text, remoteauth.PairingClaimCodePrefix) {
		decoded, decodeErr := remoteauth.DecodePairingClaimCode(text)
		clear(payload)
		if decodeErr != nil || len(decoded) == 0 || len(decoded) > endpointdomain.MaxPortableContractBytes {
			clear(decoded)
			return nil, fmt.Errorf("pairing claim code is invalid")
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
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := securefs.SecureDirectory(parent); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".pairing-*.json")
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
	if err := securefs.SecureFile(temporaryPath); err != nil {
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
	if err := filepublish.Rename(temporaryPath, path); err != nil {
		return err
	}
	if err := filepublish.SyncDirectory(parent); err != nil {
		return err
	}
	committed = true
	return nil
}
