package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lozzow/termx/shared/connection"
	"github.com/spf13/cobra"
)

type endpointCommandRuntime struct {
	registryPath string
	socket       *string
	logFile      *string
}

type endpointRouteView struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Enabled             bool     `json:"enabled"`
	ManualOnly          bool     `json:"manual_only"`
	Priority            *int     `json:"priority,omitempty"`
	CredentialRef       string   `json:"credential_ref,omitempty"`
	Socket              string   `json:"socket,omitempty"`
	Host                string   `json:"host,omitempty"`
	Port                uint16   `json:"port,omitempty"`
	User                string   `json:"user,omitempty"`
	ProxyJump           string   `json:"proxy_jump,omitempty"`
	RemoteSocket        string   `json:"remote_socket,omitempty"`
	HostKeyFingerprints []string `json:"host_key_fingerprints,omitempty"`
	Addresses           []string `json:"addresses,omitempty"`
	ServerName          string   `json:"server_name,omitempty"`
	TargetDeviceID      string   `json:"target_device_id,omitempty"`
	AccountProfile      string   `json:"account_profile,omitempty"`
	RelayMode           string   `json:"relay_mode,omitempty"`
}

type endpointView struct {
	ID                string              `json:"id"`
	Label             string              `json:"label"`
	DeviceID          string              `json:"device_id,omitempty"`
	DeviceFingerprint string              `json:"device_fingerprint,omitempty"`
	Enabled           bool                `json:"enabled"`
	Default           bool                `json:"default"`
	ConnectMode       string              `json:"connect_mode"`
	HedgeDelay        string              `json:"hedge_delay,omitempty"`
	Routes            []endpointRouteView `json:"routes"`
}

type endpointTestView struct {
	SchemaVersion        int    `json:"schema_version"`
	Kind                 string `json:"kind"`
	ID                   string `json:"id"`
	RouteID              string `json:"route_id"`
	RouteKind            string `json:"route_kind"`
	State                string `json:"state"`
	ObservedPath         string `json:"observed_path,omitempty"`
	RouteSelectionReason string `json:"route_selection_reason,omitempty"`
}

func newEndpointCommand(socket, logFile *string) *cobra.Command {
	runtime := &endpointCommandRuntime{socket: socket, logFile: logFile}
	command := &cobra.Command{Use: "endpoint", Short: "Manage daemon endpoints and their routes"}
	command.PersistentFlags().StringVar(&runtime.registryPath, "registry", "", "endpoint registry path (default: XDG config dir connections.yaml)")
	command.AddCommand(
		newEndpointListCommand(runtime),
		newEndpointShowCommand(runtime),
		newEndpointAddCommand(runtime),
		newEndpointUpdateCommand(runtime),
		newEndpointRemoveCommand(runtime),
		newEndpointToggleCommand(runtime, true),
		newEndpointToggleCommand(runtime, false),
		newEndpointSetDefaultCommand(runtime),
		newEndpointRouteCommand(runtime),
		newEndpointTestCommand(runtime),
	)
	return command
}

func (runtime *endpointCommandRuntime) load() (connection.Registry, error) {
	registry, err := connection.Load(runtime.registryPath)
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	registry, err = registry.Normalize()
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	return registry, nil
}

func (runtime *endpointCommandRuntime) loadForCreate() (connection.Registry, error) {
	registry, err := connection.Load(runtime.registryPath)
	if err != nil && strings.TrimSpace(runtime.registryPath) != "" && errors.Is(err, os.ErrNotExist) {
		return connection.Registry{Version: connection.RegistryVersion, Endpoints: map[connection.EndpointID]connection.Endpoint{}}, nil
	}
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	registry, err = registry.Normalize()
	if err != nil {
		return connection.Registry{}, &cliError{code: 2, message: err.Error(), cause: err}
	}
	return registry, nil
}

func (runtime *endpointCommandRuntime) save(registry connection.Registry) error {
	if err := connection.Save(runtime.registryPath, registry); err != nil {
		return &cliError{code: 2, message: err.Error(), cause: err}
	}
	return nil
}

func newEndpointListCommand(runtime *endpointCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "list", Short: "List configured endpoints without dialing them", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			views := endpointViews(registry)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int            `json:"schema_version"`
					Kind          string         `json:"kind"`
					Items         []endpointView `json:"items"`
				}{2, "endpoint_list", views})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ID\tLABEL\tROUTES\tENABLED\tDEFAULT")
			for _, view := range views {
				kinds := make([]string, 0, len(view.Routes))
				for _, route := range view.Routes {
					kinds = append(kinds, route.ID+":"+route.Kind)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%t\t%t\n", view.ID, view.Label, strings.Join(kinds, ","), view.Enabled, view.Default)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func newEndpointShowCommand(runtime *endpointCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "show ID", Short: "Show one configured endpoint and all routes", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			endpoint, ok := registry.Endpoints[connection.EndpointID(args[0])]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", args[0])}
			}
			view := endpointConfigView(endpoint, registry.Default == endpoint.ID)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int          `json:"schema_version"`
					Kind          string       `json:"kind"`
					Item          endpointView `json:"item"`
				}{2, "endpoint", view})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nLabel: %s\nEnabled: %t\nDefault: %t\nConnect mode: %s\n", view.ID, view.Label, view.Enabled, view.Default, view.ConnectMode)
			if view.DeviceFingerprint != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Device: %s\nFingerprint: %s\n", view.DeviceID, view.DeviceFingerprint)
			}
			for _, route := range view.Routes {
				fmt.Fprintf(cmd.OutOrStdout(), "Route %s: %s enabled=%t manual_only=%t\n", route.ID, route.Kind, route.Enabled, route.ManualOnly)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

type endpointEditFlags struct {
	label, connectMode, deviceID, deviceFingerprint string
	hedgeDelay                                      time.Duration
}

type routeEditFlags struct {
	routeID, credentialRef, socket, host, user, proxyJump, remoteSocket string
	serverName, targetDeviceID, accountProfile, relayMode               string
	port                                                                uint16
	priority                                                            int
	manualOnly                                                          bool
	hostKeyFingerprints, addresses                                      []string
}

func newEndpointAddCommand(runtime *endpointCommandRuntime) *cobra.Command {
	command := &cobra.Command{Use: "add", Short: "Add an endpoint with its first route"}
	for _, kind := range []connection.RouteKind{connection.RouteLocalUnix, connection.RouteSSHStdio, connection.RouteDirectTLS, connection.RouteManagedWebRTC} {
		endpointFlags := &endpointEditFlags{}
		routeFlags := &routeEditFlags{}
		name := routeCommandName(kind)
		child := &cobra.Command{
			Use: name + " ID", Short: "Add an endpoint with a " + string(kind) + " route", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				registry, err := runtime.loadForCreate()
				if err != nil {
					return err
				}
				id := connection.EndpointID(strings.TrimSpace(args[0]))
				if _, exists := registry.Endpoints[id]; exists {
					return &cliError{code: 4, message: fmt.Sprintf("endpoint %s already exists", id)}
				}
				route := routeFromFlags(kind, *routeFlags)
				endpoint := endpointFromFlags(id, *endpointFlags, route)
				if cmd.Flags().Changed("hedge-delay") {
					endpoint.SelectionPolicy = connection.SelectionPolicy{HedgeDelay: endpointFlags.hedgeDelay, HedgeDelayConfigured: true}
				}
				if _, err := (connection.Registry{Version: connection.RegistryVersion, Default: id, Endpoints: map[connection.EndpointID]connection.Endpoint{id: endpoint}}).Normalize(); err != nil {
					return usageCLIError(err.Error())
				}
				registry.Endpoints[id] = endpoint
				if registry.Default == "" {
					registry.Default = id
				}
				if err := runtime.save(registry); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tadded\t%s\n", id, route.ID)
				return nil
			},
		}
		bindEndpointPolicyFlags(child, endpointFlags)
		bindEndpointIdentityFlags(child, endpointFlags)
		bindRouteEditFlags(child, routeFlags, kind, true)
		command.AddCommand(child)
	}
	return command
}

func newEndpointUpdateCommand(runtime *endpointCommandRuntime) *cobra.Command {
	flags := &endpointEditFlags{}
	command := &cobra.Command{
		Use: "update ID", Short: "Update endpoint identity, label, or selection policy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			endpoint, ok := registry.Endpoints[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if cmd.Flags().Changed("label") {
				endpoint.Label, endpoint.LabelSource = strings.TrimSpace(flags.label), connection.SourceUser
			}
			if cmd.Flags().Changed("connect-mode") {
				endpoint.ConnectMode = connection.ConnectMode(flags.connectMode)
			}
			if cmd.Flags().Changed("hedge-delay") {
				endpoint.SelectionPolicy = connection.SelectionPolicy{HedgeDelay: flags.hedgeDelay, HedgeDelayConfigured: true}
			}
			registry.Endpoints[id] = endpoint
			if err := runtime.save(registry); err != nil {
				return usageCLIError(err.Error())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tupdated\n", id)
			return nil
		},
	}
	bindEndpointPolicyFlags(command, flags)
	return command
}

func newEndpointRemoveCommand(runtime *endpointCommandRuntime) *cobra.Command {
	return &cobra.Command{
		Use: "remove ID", Short: "Remove an endpoint from the registry", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			if _, ok := registry.Endpoints[id]; !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if len(registry.Endpoints) == 1 {
				return &cliError{code: 4, message: "cannot remove the last endpoint"}
			}
			delete(registry.Endpoints, id)
			if registry.Default == id {
				if err := reassignEndpointDefault(&registry); err != nil {
					return err
				}
			}
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tremoved\n", id)
			return nil
		},
	}
}

func newEndpointToggleCommand(runtime *endpointCommandRuntime, enabled bool) *cobra.Command {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	return &cobra.Command{
		Use: verb + " ID", Short: endpointVerbTitle(verb) + " an endpoint", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			endpoint, ok := registry.Endpoints[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			endpoint.Enabled = enabled
			registry.Endpoints[id] = endpoint
			if !enabled && registry.Default == id {
				if err := reassignEndpointDefault(&registry); err != nil {
					return err
				}
			}
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%sd\n", id, verb)
			return nil
		},
	}
}

func reassignEndpointDefault(registry *connection.Registry) error {
	var firstEnabled connection.EndpointID
	for _, endpoint := range registry.List() {
		if !endpoint.Enabled {
			continue
		}
		if firstEnabled == "" {
			firstEnabled = endpoint.ID
		}
		for _, route := range endpoint.RouteList() {
			if route.Enabled && route.Kind == connection.RouteLocalUnix {
				registry.Default = endpoint.ID
				return nil
			}
		}
	}
	if firstEnabled == "" {
		return &cliError{code: 4, message: "endpoint registry requires at least one enabled default endpoint"}
	}
	registry.Default = firstEnabled
	return nil
}

func endpointVerbTitle(verb string) string {
	if verb == "" {
		return ""
	}
	return strings.ToUpper(verb[:1]) + verb[1:]
}

func newEndpointSetDefaultCommand(runtime *endpointCommandRuntime) *cobra.Command {
	return &cobra.Command{
		Use: "set-default ID", Short: "Set the default endpoint", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			endpoint, ok := registry.Endpoints[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if !endpoint.Enabled {
				return &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", id)}
			}
			registry.Default = id
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tdefault\n", id)
			return nil
		},
	}
}

func newEndpointRouteCommand(runtime *endpointCommandRuntime) *cobra.Command {
	command := &cobra.Command{Use: "route", Short: "Manage routes within an endpoint"}
	command.AddCommand(newEndpointRouteAddCommand(runtime), newEndpointRouteUpdateCommand(runtime), newEndpointRouteRemoveCommand(runtime), newEndpointRouteToggleCommand(runtime, true), newEndpointRouteToggleCommand(runtime, false))
	return command
}

func newEndpointRouteAddCommand(runtime *endpointCommandRuntime) *cobra.Command {
	command := &cobra.Command{Use: "add", Short: "Add a route to an existing endpoint"}
	for _, kind := range []connection.RouteKind{connection.RouteLocalUnix, connection.RouteSSHStdio, connection.RouteDirectTLS, connection.RouteManagedWebRTC} {
		flags := &routeEditFlags{}
		child := &cobra.Command{
			Use: routeCommandName(kind) + " ENDPOINT_ID ROUTE_ID", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				registry, err := runtime.load()
				if err != nil {
					return err
				}
				endpointID, routeID := connection.EndpointID(args[0]), connection.RouteID(args[1])
				endpoint, ok := registry.Endpoints[endpointID]
				if !ok {
					return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", endpointID)}
				}
				if _, exists := endpoint.Routes[routeID]; exists {
					return &cliError{code: 4, message: fmt.Sprintf("endpoint %s route %s already exists", endpointID, routeID)}
				}
				flags.routeID = string(routeID)
				route := routeFromFlags(kind, *flags)
				endpoint.Routes[routeID] = route
				registry.Endpoints[endpointID] = endpoint
				if err := runtime.save(registry); err != nil {
					return usageCLIError(err.Error())
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tadded\n", endpointID, routeID)
				return nil
			},
		}
		bindRouteEditFlags(child, flags, kind, false)
		command.AddCommand(child)
	}
	return command
}

func newEndpointRouteUpdateCommand(runtime *endpointCommandRuntime) *cobra.Command {
	flags := &routeEditFlags{}
	command := &cobra.Command{
		Use: "update ENDPOINT_ID ROUTE_ID", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			endpointID, routeID := connection.EndpointID(args[0]), connection.RouteID(args[1])
			endpoint, ok := registry.Endpoints[endpointID]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", endpointID)}
			}
			route, ok := endpoint.Routes[routeID]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s route %s was not found", endpointID, routeID)}
			}
			applyRouteFlagChanges(cmd, &route, *flags)
			route.PolicySource = connection.SourceUser
			endpoint.Routes[routeID] = route
			registry.Endpoints[endpointID] = endpoint
			if err := runtime.save(registry); err != nil {
				return usageCLIError(err.Error())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tupdated\n", endpointID, routeID)
			return nil
		},
	}
	bindRouteEditFlags(command, flags, "", false)
	return command
}

func newEndpointRouteRemoveCommand(runtime *endpointCommandRuntime) *cobra.Command {
	return &cobra.Command{
		Use: "remove ENDPOINT_ID ROUTE_ID", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			endpointID, routeID := connection.EndpointID(args[0]), connection.RouteID(args[1])
			endpoint, ok := registry.Endpoints[endpointID]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", endpointID)}
			}
			if _, ok := endpoint.Routes[routeID]; !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s route %s was not found", endpointID, routeID)}
			}
			if len(endpoint.Routes) == 1 {
				return &cliError{code: 4, message: "cannot remove the last route from an endpoint"}
			}
			delete(endpoint.Routes, routeID)
			registry.Endpoints[endpointID] = endpoint
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\tremoved\n", endpointID, routeID)
			return nil
		},
	}
}

func newEndpointRouteToggleCommand(runtime *endpointCommandRuntime, enabled bool) *cobra.Command {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	return &cobra.Command{
		Use: verb + " ENDPOINT_ID ROUTE_ID", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			endpointID, routeID := connection.EndpointID(args[0]), connection.RouteID(args[1])
			endpoint, ok := registry.Endpoints[endpointID]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", endpointID)}
			}
			route, ok := endpoint.Routes[routeID]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s route %s was not found", endpointID, routeID)}
			}
			route.Enabled, route.PolicySource = enabled, connection.SourceUser
			endpoint.Routes[routeID] = route
			registry.Endpoints[endpointID] = endpoint
			if err := runtime.save(registry); err != nil {
				return usageCLIError(err.Error())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%sd\n", endpointID, routeID, verb)
			return nil
		},
	}
}

func newEndpointTestCommand(runtime *endpointCommandRuntime) *cobra.Command {
	var jsonOutput bool
	var routeValue string
	command := &cobra.Command{
		Use: "test ID", Short: "Dial one route and verify termx protocol reachability", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			endpoint, ok := registry.Endpoints[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if !endpoint.Enabled {
				return &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", id)}
			}
			cmd.Root().SilenceUsage = true
			routeID, observedPath, selectionReason, closeClient, err := probeEndpointProtocolClient(cmd.Context(), endpoint, connection.RouteID(routeValue), *runtime.socket, *runtime.logFile)
			if err != nil {
				return classifyCLIError(err)
			}
			defer closeClient()
			route, _ := endpoint.Route(routeID)
			view := endpointTestView{SchemaVersion: 2, Kind: "endpoint_test", ID: string(id), RouteID: string(routeID), RouteKind: string(route.Kind), State: "reachable", ObservedPath: observedPath, RouteSelectionReason: selectionReason}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\treachable\t%s\t%s", id, routeID, route.Kind)
			if observedPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\t%s", observedPath)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	command.Flags().StringVar(&routeValue, "route", "", "explicit route ID (required when multiple routes are eligible before CONN003)")
	return command
}

func bindEndpointPolicyFlags(command *cobra.Command, flags *endpointEditFlags) {
	command.Flags().StringVar(&flags.label, "label", "", "display label")
	command.Flags().StringVar(&flags.connectMode, "connect-mode", string(connection.ConnectOnDemand), "auto, on_demand, or manual")
	command.Flags().DurationVar(&flags.hedgeDelay, "hedge-delay", 300*time.Millisecond, "delay before starting the next priority group")
}

func bindEndpointIdentityFlags(command *cobra.Command, flags *endpointEditFlags) {
	command.Flags().StringVar(&flags.deviceID, "device-id", "", "daemon directory identity")
	command.Flags().StringVar(&flags.deviceFingerprint, "device-fingerprint", "", "pinned daemon identity fingerprint")
}

func bindRouteEditFlags(command *cobra.Command, flags *routeEditFlags, kind connection.RouteKind, includeRouteID bool) {
	if includeRouteID {
		command.Flags().StringVar(&flags.routeID, "route", routeCommandName(kind), "route ID")
	}
	command.Flags().BoolVar(&flags.manualOnly, "manual-only", false, "exclude this route from automatic selection")
	command.Flags().IntVar(&flags.priority, "priority", -1, "route priority; lower starts earlier, -1 means unset")
	if kind == "" || kind == connection.RouteSSHStdio || kind == connection.RouteDirectTLS || kind == connection.RouteManagedWebRTC {
		command.Flags().StringVar(&flags.credentialRef, "credential-ref", "", "local secure credential reference")
	}
	if kind == "" || kind == connection.RouteLocalUnix {
		command.Flags().StringVar(&flags.socket, "socket", "auto", "local daemon socket")
	}
	if kind == "" || kind == connection.RouteSSHStdio {
		command.Flags().StringVar(&flags.host, "host", "", "OpenSSH host or alias")
		command.Flags().Uint16Var(&flags.port, "port", 22, "SSH port")
		command.Flags().StringVar(&flags.user, "user", "", "SSH user hint")
		command.Flags().StringVar(&flags.proxyJump, "proxy-jump", "", "OpenSSH ProxyJump target")
		command.Flags().StringVar(&flags.remoteSocket, "remote-socket", "auto", "remote daemon socket")
		command.Flags().StringSliceVar(&flags.hostKeyFingerprints, "host-key", nil, "accepted SSH host-key fingerprint")
	}
	if kind == "" || kind == connection.RouteDirectTLS {
		command.Flags().StringSliceVar(&flags.addresses, "address", nil, "direct TLS address (repeatable)")
		command.Flags().StringVar(&flags.serverName, "server-name", "", "TLS routing server name; not a trust anchor")
	}
	if kind == "" || kind == connection.RouteManagedWebRTC {
		command.Flags().StringVar(&flags.targetDeviceID, "target-device-id", "", "managed target device ID")
		command.Flags().StringVar(&flags.accountProfile, "account-profile", "", "local Cloud account profile hint")
		command.Flags().StringVar(&flags.relayMode, "relay", string(connection.RelayAuto), "auto, direct, relay_only, or smart_route")
	}
}

func endpointFromFlags(id connection.EndpointID, flags endpointEditFlags, route connection.AccessRoute) connection.Endpoint {
	label := strings.TrimSpace(flags.label)
	if label == "" {
		label = string(id)
	}
	endpoint := connection.Endpoint{
		ID: id, Label: label, LabelSource: connection.SourceUser, Enabled: true, ConnectMode: connection.ConnectMode(flags.connectMode),
		DaemonIdentity: connection.DaemonIdentity{DeviceID: strings.TrimSpace(flags.deviceID), DeviceFingerprint: strings.TrimSpace(flags.deviceFingerprint)},
		Routes:         map[connection.RouteID]connection.AccessRoute{route.ID: route},
	}
	if flags.hedgeDelay != 300*time.Millisecond {
		endpoint.SelectionPolicy = connection.SelectionPolicy{HedgeDelay: flags.hedgeDelay, HedgeDelayConfigured: true}
	}
	return endpoint
}

func routeFromFlags(kind connection.RouteKind, flags routeEditFlags) connection.AccessRoute {
	routeID := connection.RouteID(strings.TrimSpace(flags.routeID))
	if routeID == "" {
		routeID = connection.RouteID(routeCommandName(kind))
	}
	route := connection.AccessRoute{
		ID: routeID, Kind: kind, Enabled: true, ManualOnly: flags.manualOnly, CredentialRef: strings.TrimSpace(flags.credentialRef),
		Source: connection.SourceManual, PolicySource: connection.SourceUser, Socket: strings.TrimSpace(flags.socket),
		Host: strings.TrimSpace(flags.host), Port: flags.port, User: strings.TrimSpace(flags.user), ProxyJump: strings.TrimSpace(flags.proxyJump),
		RemoteSocket: strings.TrimSpace(flags.remoteSocket), HostKeyFingerprints: append([]string(nil), flags.hostKeyFingerprints...),
		Addresses: append([]string(nil), flags.addresses...), ServerName: strings.TrimSpace(flags.serverName),
		TargetDeviceID: strings.TrimSpace(flags.targetDeviceID), AccountProfile: strings.TrimSpace(flags.accountProfile), RelayMode: connection.RelayMode(flags.relayMode),
	}
	if flags.priority >= 0 {
		priority := flags.priority
		route.Priority = &priority
	}
	return route
}

func applyRouteFlagChanges(command *cobra.Command, route *connection.AccessRoute, flags routeEditFlags) {
	if command.Flags().Changed("manual-only") {
		route.ManualOnly = flags.manualOnly
	}
	if command.Flags().Changed("priority") {
		if flags.priority < 0 {
			route.Priority = nil
		} else {
			priority := flags.priority
			route.Priority = &priority
		}
	}
	if command.Flags().Changed("credential-ref") {
		route.CredentialRef = strings.TrimSpace(flags.credentialRef)
	}
	if command.Flags().Changed("socket") {
		route.Socket = strings.TrimSpace(flags.socket)
	}
	if command.Flags().Changed("host") {
		route.Host = strings.TrimSpace(flags.host)
	}
	if command.Flags().Changed("port") {
		route.Port = flags.port
	}
	if command.Flags().Changed("user") {
		route.User = strings.TrimSpace(flags.user)
	}
	if command.Flags().Changed("proxy-jump") {
		route.ProxyJump = strings.TrimSpace(flags.proxyJump)
	}
	if command.Flags().Changed("remote-socket") {
		route.RemoteSocket = strings.TrimSpace(flags.remoteSocket)
	}
	if command.Flags().Changed("host-key") {
		route.HostKeyFingerprints = append([]string(nil), flags.hostKeyFingerprints...)
	}
	if command.Flags().Changed("address") {
		route.Addresses = append([]string(nil), flags.addresses...)
	}
	if command.Flags().Changed("server-name") {
		route.ServerName = strings.TrimSpace(flags.serverName)
	}
	if command.Flags().Changed("target-device-id") {
		route.TargetDeviceID = strings.TrimSpace(flags.targetDeviceID)
	}
	if command.Flags().Changed("account-profile") {
		route.AccountProfile = strings.TrimSpace(flags.accountProfile)
	}
	if command.Flags().Changed("relay") {
		route.RelayMode = connection.RelayMode(flags.relayMode)
	}
}

func routeCommandName(kind connection.RouteKind) string {
	switch kind {
	case connection.RouteLocalUnix:
		return "local"
	case connection.RouteSSHStdio:
		return "ssh"
	case connection.RouteDirectTLS:
		return "direct"
	case connection.RouteManagedWebRTC:
		return "cloud"
	default:
		return string(kind)
	}
}

func endpointViews(registry connection.Registry) []endpointView {
	views := make([]endpointView, 0, len(registry.Endpoints))
	for _, endpoint := range registry.List() {
		views = append(views, endpointConfigView(endpoint, registry.Default == endpoint.ID))
	}
	return views
}

func endpointConfigView(endpoint connection.Endpoint, isDefault bool) endpointView {
	view := endpointView{
		ID: string(endpoint.ID), Label: endpoint.Label, DeviceID: endpoint.DaemonIdentity.DeviceID,
		DeviceFingerprint: endpoint.DaemonIdentity.DeviceFingerprint, Enabled: endpoint.Enabled, Default: isDefault,
		ConnectMode: string(endpoint.ConnectMode), Routes: []endpointRouteView{},
	}
	if endpoint.SelectionPolicy.HedgeDelayConfigured {
		view.HedgeDelay = endpoint.SelectionPolicy.HedgeDelay.String()
	}
	for _, route := range endpoint.RouteList() {
		view.Routes = append(view.Routes, endpointRouteView{
			ID: string(route.ID), Kind: string(route.Kind), Enabled: route.Enabled, ManualOnly: route.ManualOnly,
			Priority: cloneCLIInt(route.Priority), CredentialRef: route.CredentialRef, Socket: route.Socket,
			Host: route.Host, Port: route.Port, User: route.User, ProxyJump: route.ProxyJump, RemoteSocket: route.RemoteSocket,
			HostKeyFingerprints: append([]string(nil), route.HostKeyFingerprints...), Addresses: append([]string(nil), route.Addresses...), ServerName: route.ServerName,
			TargetDeviceID: route.TargetDeviceID, AccountProfile: route.AccountProfile, RelayMode: string(route.RelayMode),
		})
	}
	return view
}

func cloneCLIInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
