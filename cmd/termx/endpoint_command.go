package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lozzow/termx/shared/connection"
	"github.com/spf13/cobra"
)

type endpointCommandRuntime struct {
	registryPath string
	socket       *string
	logFile      *string
}

type endpointView struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	Transport         string `json:"transport"`
	Enabled           bool   `json:"enabled"`
	Default           bool   `json:"default"`
	ConnectMode       string `json:"connect_mode"`
	Address           string `json:"address,omitempty"`
	Socket            string `json:"socket,omitempty"`
	RemoteSocket      string `json:"remote_socket,omitempty"`
	AuthRef           string `json:"auth_ref,omitempty"`
	HubDeviceID       string `json:"hub_device_id,omitempty"`
	DeviceFingerprint string `json:"device_fingerprint,omitempty"`
	GrantRef          string `json:"grant_ref,omitempty"`
	RelayMode         string `json:"relay_mode,omitempty"`
}

type endpointTestView struct {
	SchemaVersion        int    `json:"schema_version"`
	Kind                 string `json:"kind"`
	ID                   string `json:"id"`
	Transport            string `json:"transport"`
	State                string `json:"state"`
	ObservedPath         string `json:"observed_path,omitempty"`
	RouteSelectionReason string `json:"route_selection_reason,omitempty"`
}

func newEndpointCommand(socket, logFile *string) *cobra.Command {
	runtime := &endpointCommandRuntime{socket: socket, logFile: logFile}
	command := &cobra.Command{Use: "endpoint", Short: "Manage daemon endpoint connections"}
	command.PersistentFlags().StringVar(&runtime.registryPath, "registry", "", "connection registry path (default: XDG config dir connections.yaml)")
	command.AddCommand(
		newEndpointListCommand(runtime),
		newEndpointShowCommand(runtime),
		newEndpointAddCommand(runtime),
		newEndpointUpdateCommand(runtime),
		newEndpointRemoveCommand(runtime),
		newEndpointToggleCommand(runtime, true),
		newEndpointToggleCommand(runtime, false),
		newEndpointSetDefaultCommand(runtime),
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
				}{1, "endpoint_list", views})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ID\tLABEL\tTRANSPORT\tENABLED\tDEFAULT\tTARGET")
			for _, view := range views {
				target := view.Address
				if view.Transport == string(connection.TransportLocal) {
					target = view.Socket
				} else if view.Transport == string(connection.TransportHubP2P) {
					target = view.HubDeviceID
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%t\t%t\t%s\n", view.ID, view.Label, view.Transport, view.Enabled, view.Default, target)
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
		Use: "show ID", Short: "Show one configured endpoint", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			cfg, ok := registry.Connections[connection.EndpointID(args[0])]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", args[0])}
			}
			view := endpointConfigView(cfg, registry.Default == cfg.ID)
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion int          `json:"schema_version"`
					Kind          string       `json:"kind"`
					Item          endpointView `json:"item"`
				}{1, "endpoint", view})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nLabel: %s\nTransport: %s\nEnabled: %t\nDefault: %t\nConnect mode: %s\n", view.ID, view.Label, view.Transport, view.Enabled, view.Default, view.ConnectMode)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

type endpointEditFlags struct {
	label, address, authRef, socket, remoteSocket string
	hubDeviceID, deviceFingerprint, grantRef      string
	relayMode, connectMode                        string
}

func newEndpointAddCommand(runtime *endpointCommandRuntime) *cobra.Command {
	command := &cobra.Command{Use: "add", Short: "Add a local, SSH, or managed Cloud endpoint"}
	for _, transport := range []connection.TransportKind{connection.TransportLocal, connection.TransportSSH, connection.TransportHubP2P} {
		flags := &endpointEditFlags{}
		name := string(transport)
		if transport == connection.TransportHubP2P {
			name = "cloud"
		}
		child := &cobra.Command{
			Use: name + " ID", Short: "Add a " + name + " endpoint", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				registry, err := runtime.load()
				if err != nil {
					return err
				}
				id := connection.EndpointID(strings.TrimSpace(args[0]))
				if _, exists := registry.Connections[id]; exists {
					return &cliError{code: 4, message: fmt.Sprintf("endpoint %s already exists", id)}
				}
				cfg := endpointConfigFromFlags(id, transport, *flags)
				if err := cfg.Validate(); err != nil {
					return usageCLIError(err.Error())
				}
				registry.Connections[id] = cfg
				if err := runtime.save(registry); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tadded\n", id)
				return nil
			},
		}
		bindEndpointEditFlags(child, flags, transport)
		command.AddCommand(child)
	}
	return command
}

func newEndpointUpdateCommand(runtime *endpointCommandRuntime) *cobra.Command {
	flags := &endpointEditFlags{}
	command := &cobra.Command{
		Use: "update ID", Short: "Update fields of an existing endpoint", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			cfg, ok := registry.Connections[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if cmd.Flags().Changed("label") {
				cfg.Label = flags.label
			}
			if cmd.Flags().Changed("connect-mode") {
				cfg.ConnectMode = connection.ConnectMode(flags.connectMode)
			}
			if cmd.Flags().Changed("address") {
				cfg.Address = flags.address
			}
			if cmd.Flags().Changed("auth-ref") {
				cfg.AuthRef = flags.authRef
			}
			if cmd.Flags().Changed("socket") {
				cfg.Socket = flags.socket
			}
			if cmd.Flags().Changed("remote-socket") {
				cfg.RemoteSocket = flags.remoteSocket
			}
			if cmd.Flags().Changed("hub-device-id") {
				cfg.HubDeviceID = flags.hubDeviceID
			}
			if cmd.Flags().Changed("device-fingerprint") {
				cfg.DeviceFingerprint = flags.deviceFingerprint
			}
			if cmd.Flags().Changed("grant-ref") {
				cfg.GrantRef = flags.grantRef
			}
			if cmd.Flags().Changed("relay") {
				cfg.RelayMode = connection.RelayMode(flags.relayMode)
			}
			if err := cfg.Validate(); err != nil {
				return usageCLIError(err.Error())
			}
			registry.Connections[id] = cfg
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tupdated\n", id)
			return nil
		},
	}
	bindEndpointEditFlags(command, flags, "")
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
			if _, ok := registry.Connections[id]; !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if len(registry.Connections) == 1 {
				return &cliError{code: 4, message: "cannot remove the last endpoint"}
			}
			delete(registry.Connections, id)
			if registry.Default == id {
				registry.Default = ""
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
			cfg, ok := registry.Connections[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			cfg.Enabled = enabled
			registry.Connections[id] = cfg
			if !enabled && registry.Default == id {
				registry.Default = ""
			}
			if err := runtime.save(registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", id, verb+"d")
			return nil
		},
	}
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
			cfg, ok := registry.Connections[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if !cfg.Enabled {
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

func newEndpointTestCommand(runtime *endpointCommandRuntime) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "test ID", Short: "Dial an endpoint and verify daemon protocol identity", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := runtime.load()
			if err != nil {
				return err
			}
			id := connection.EndpointID(args[0])
			cfg, ok := registry.Connections[id]
			if !ok {
				return &cliError{code: 3, message: fmt.Sprintf("endpoint %s was not found", id)}
			}
			if !cfg.Enabled {
				return &cliError{code: 4, message: fmt.Sprintf("endpoint %s is disabled", id)}
			}
			cmd.Root().SilenceUsage = true
			observedPath, selectionReason, closeClient, err := probeEndpointProtocolClient(cmd.Context(), cfg, *runtime.socket, *runtime.logFile)
			if err != nil {
				return classifyCLIError(err)
			}
			defer closeClient()
			view := endpointTestView{
				SchemaVersion: 1, Kind: "endpoint_test", ID: string(id), Transport: string(cfg.Transport), State: "reachable",
				ObservedPath: observedPath, RouteSelectionReason: selectionReason,
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\treachable\t%s", id, cfg.Transport)
			if observedPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\t%s", observedPath)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func bindEndpointEditFlags(command *cobra.Command, flags *endpointEditFlags, transport connection.TransportKind) {
	command.Flags().StringVar(&flags.label, "label", "", "display label")
	command.Flags().StringVar(&flags.connectMode, "connect-mode", string(connection.ConnectOnDemand), "auto, on_demand, or manual")
	if transport == "" || transport == connection.TransportSSH {
		command.Flags().StringVar(&flags.address, "address", "", "SSH destination")
		command.Flags().StringVar(&flags.authRef, "auth-ref", "", "SSH authentication reference")
		command.Flags().StringVar(&flags.remoteSocket, "remote-socket", "auto", "remote daemon socket")
	}
	if transport == "" || transport == connection.TransportLocal {
		command.Flags().StringVar(&flags.socket, "socket", "auto", "local daemon socket")
	}
	if transport == "" || transport == connection.TransportHubP2P {
		command.Flags().StringVar(&flags.hubDeviceID, "hub-device-id", "", "managed target device ID")
		command.Flags().StringVar(&flags.deviceFingerprint, "device-fingerprint", "", "daemon identity fingerprint")
		command.Flags().StringVar(&flags.grantRef, "grant-ref", "", "local capability credential reference")
		command.Flags().StringVar(&flags.relayMode, "relay", string(connection.RelayAuto), "auto, direct, relay_only, or smart_route")
	}
}

func endpointConfigFromFlags(id connection.EndpointID, transport connection.TransportKind, flags endpointEditFlags) connection.Config {
	label := strings.TrimSpace(flags.label)
	if label == "" {
		label = string(id)
	}
	return connection.Config{
		ID: id, Label: label, Transport: transport, Enabled: true, ConnectMode: connection.ConnectMode(flags.connectMode),
		Address: flags.address, AuthRef: flags.authRef, Socket: flags.socket, RemoteSocket: flags.remoteSocket,
		HubDeviceID: flags.hubDeviceID, DeviceFingerprint: flags.deviceFingerprint, GrantRef: flags.grantRef, RelayMode: connection.RelayMode(flags.relayMode),
	}
}

func endpointViews(registry connection.Registry) []endpointView {
	views := make([]endpointView, 0, len(registry.Connections))
	for _, cfg := range registry.List() {
		views = append(views, endpointConfigView(cfg, registry.Default == cfg.ID))
	}
	return views
}

func endpointConfigView(cfg connection.Config, isDefault bool) endpointView {
	return endpointView{
		ID: string(cfg.ID), Label: cfg.Label, Transport: string(cfg.Transport), Enabled: cfg.Enabled, Default: isDefault,
		ConnectMode: string(cfg.ConnectMode), Address: cfg.Address, Socket: cfg.Socket, RemoteSocket: cfg.RemoteSocket,
		AuthRef: cfg.AuthRef, HubDeviceID: cfg.HubDeviceID, DeviceFingerprint: cfg.DeviceFingerprint, GrantRef: cfg.GrantRef, RelayMode: string(cfg.RelayMode),
	}
}
