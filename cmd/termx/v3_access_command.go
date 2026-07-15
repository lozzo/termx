package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/spf13/cobra"
)

func v3AccessCommand(socket *string, logFile *string) *cobra.Command {
	command := &cobra.Command{Use: "access", Short: "Inspect and revoke daemon client-bound access"}
	command.AddCommand(v3AccessIdentityCommand(socket, logFile))
	command.AddCommand(v3AccessListCommand(socket, logFile))
	command.AddCommand(v3AccessRevokeCommand(socket, logFile))
	return command
}

func v3AccessIdentityCommand(socket *string, logFile *string) *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{
		Use: "identity", Short: "Show the local daemon global DeviceIdentity", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := dialOrStartV3ClientContext(cmd.Context(), resolveV3Socket(*socket), resolveV3LogFilePath(*logFile), nil)
			if err != nil {
				return err
			}
			defer client.Close()
			var result protocol.ClientAccessIdentityResult
			if err := client.Call(cmd.Context(), "remote.access.identity", map[string]any{}, &result); err != nil {
				return err
			}
			view := struct {
				DeviceID          string `json:"device_id"`
				DeviceFingerprint string `json:"device_fingerprint"`
				DevicePublicKey   string `json:"device_public_key"`
			}{result.DeviceID, result.DeviceFingerprint, base64.RawURLEncoding.EncodeToString(result.DevicePublicKey)}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Device: %s\nFingerprint: %s\nPublic key: %s\n", view.DeviceID, view.DeviceFingerprint, view.DevicePublicKey)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func v3AccessListCommand(socket *string, logFile *string) *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{
		Use: "list", Short: "List daemon-local client access grants", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := dialOrStartV3ClientContext(cmd.Context(), resolveV3Socket(*socket), resolveV3LogFilePath(*logFile), nil)
			if err != nil {
				return err
			}
			defer client.Close()
			var result protocol.ClientAccessListResult
			if err := client.Call(cmd.Context(), "remote.access.list", map[string]any{}, &result); err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result.Records)
			}
			if len(result.Records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No client access grants")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "GRANT\tCLIENT\tSUBJECT\tEXPIRES\tSTATE")
			for _, record := range result.Records {
				state := "active"
				if !record.RevokedAt.IsZero() {
					state = "revoked"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", record.GrantID, record.ClientLabel, record.SubjectKeyFingerprint, formatTerminalTime(record.ExpiresAt), state)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func v3AccessRevokeCommand(socket *string, logFile *string) *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{
		Use: "revoke GRANT_ID", Short: "Revoke one daemon-local client access grant", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			grantID := strings.TrimSpace(args[0])
			if grantID == "" {
				return usageCLIError("grant id is required")
			}
			client, err := dialOrStartV3ClientContext(cmd.Context(), resolveV3Socket(*socket), resolveV3LogFilePath(*logFile), nil)
			if err != nil {
				return err
			}
			defer client.Close()
			var result protocol.ClientAccessRecord
			if err := client.Call(cmd.Context(), "remote.access.revoke", protocol.ClientAccessRevokeParams{GrantID: grantID}, &result); err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked client access grant %s for %s\n", result.GrantID, result.SubjectKeyFingerprint)
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}
