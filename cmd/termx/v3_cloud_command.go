package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
	"github.com/lozzow/termx/shared/cloudcompanion/ipc"
	"github.com/lozzow/termx/shared/remoteauth"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	v3CloudNow                = func() time.Time { return time.Now().UTC() }
	readV3CloudEnrollmentCode = defaultReadV3CloudEnrollmentCode
)

func v3CloudCommand() *cobra.Command {
	command := &cobra.Command{Use: "cloud", Short: "Manage the optional TermX Cloud Companion"}
	command.AddCommand(v3CloudInstallCommand(false))
	command.AddCommand(v3CloudInstallCommand(true))
	command.AddCommand(v3CloudLoginCommand())
	command.AddCommand(v3CloudEnrollCommand())
	command.AddCommand(v3CloudStatusCommand())
	command.AddCommand(v3CloudDoctorCommand())
	command.AddCommand(v3CloudLogoutCommand())
	command.AddCommand(v3CloudUninstallCommand())
	return command
}

func v3CloudInstallCommand(update bool) *cobra.Command {
	channel := "stable"
	version := ""
	use := "install"
	short := "Install the signed Cloud Companion"
	if update {
		use = "update"
		short = "Update the signed Cloud Companion"
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cloudInstaller, err := newV3CloudInstallerForCommand()
			if err != nil {
				return err
			}
			installed, err := cloudInstaller.InstallRelease(cmd.Context(), installer.Request{Channel: channel, Version: version})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloud Companion %s active (%s)\n", installed.Version, installed.Channel)
			return nil
		},
	}
	command.Flags().StringVar(&channel, "channel", "stable", "release channel: stable or beta")
	if !update {
		command.Flags().StringVar(&version, "version", "", "exact canonical version, for example v1.2.3")
	}
	return command
}

func v3CloudLoginCommand() *cobra.Command {
	deviceCode := false
	command := &cobra.Command{
		Use:   "login",
		Short: "Log in through the Cloud Companion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
			if err != nil {
				return err
			}
			defer client.Close()
			method := cloudpb.LoginMethod_LOGIN_METHOD_BROWSER
			if deviceCode {
				method = cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE
			}
			flow, err := client.BeginLogin(cmd.Context(), &cloudpb.BeginLoginRequest{Method: method})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Open %s\n", flow.GetVerificationUri())
			if flow.GetUserCode() != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Code: %s\n", flow.GetUserCode())
			}
			response, err := completeV3CloudLogin(cmd.Context(), client, flow)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Logged in as %s\n", cloudAccountLabel(response.GetSession()))
			return nil
		},
	}
	command.Flags().BoolVar(&deviceCode, "device-code", false, "use the device-code login flow")
	return command
}

func completeV3CloudLogin(ctx context.Context, client v3CloudClient, flow *cloudpb.LoginFlow) (*cloudpb.CompleteLoginResponse, error) {
	interval := time.Duration(flow.GetPollIntervalMillis()) * time.Millisecond
	if interval <= 0 {
		interval = time.Second
	}
	expiresAt := time.Unix(int64(flow.GetExpiresAtUnix()), 0)
	if flow.GetExpiresAtUnix() == 0 {
		expiresAt = v3CloudNow().Add(5 * time.Minute)
	}
	for {
		response, err := client.CompleteLogin(ctx, &cloudpb.CompleteLoginRequest{FlowId: flow.GetFlowId()})
		if err == nil {
			return response, nil
		}
		var cloudErr *cloudcompanion.Error
		if !errors.As(err, &cloudErr) || !cloudErr.Retryable || cloudErr.Code != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY {
			return nil, err
		}
		if !v3CloudNow().Add(interval).Before(expiresAt) {
			return nil, fmt.Errorf("cloud login approval expired")
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func v3CloudEnrollCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enroll [ONE_TIME_CODE]",
		Short: "Enroll this daemon identity with managed cloud",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := []byte(nil)
			if len(args) == 1 {
				code = []byte(strings.TrimSpace(args[0]))
			} else {
				var err error
				code, err = readV3CloudEnrollmentCode(cmd)
				if err != nil {
					return err
				}
			}
			defer clear(code)
			if len(code) == 0 {
				return fmt.Errorf("one-time enrollment code is required")
			}
			identity, err := remoteauth.LoadOrCreateLocalIdentity(v3RemoteIdentityDir())
			if err != nil {
				return err
			}
			client, err := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT)
			if err != nil {
				return err
			}
			defer client.Close()
			hostname, _ := os.Hostname()
			challenge, err := client.BeginDeviceEnrollment(cmd.Context(), &cloudpb.BeginDeviceEnrollmentRequest{
				OneTimeCode: string(code), DevicePublicKey: append([]byte(nil), identity.PublicKey...),
				Metadata: &cloudpb.DeviceMetadata{DisplayName: hostname, Hostname: hostname, Platform: runtime.GOOS + "/" + runtime.GOARCH, TermxVersion: termxBuildVersion, SignalingVersions: []uint32{cloudcompanion.ProtocolVersionMax}},
			})
			if err != nil {
				return err
			}
			signedAt := v3CloudNow()
			signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{
				FlowId: challenge.GetFlowId(), ChallengeId: challenge.GetChallengeId(), Challenge: append([]byte(nil), challenge.GetChallenge()...),
				DeviceId: identity.DeviceID, DevicePublicKey: append([]byte(nil), identity.PublicKey...), SignedAtUnixNano: signedAt.UnixNano(),
			})
			if err != nil {
				return err
			}
			signature := ed25519.Sign(identity.PrivateKey, signingBytes)
			response, err := client.CompleteDeviceEnrollment(cmd.Context(), &cloudpb.CompleteDeviceEnrollmentRequest{
				FlowId: challenge.GetFlowId(), Proof: &cloudpb.DeviceProof{
					DeviceId: identity.DeviceID, DevicePublicKey: append([]byte(nil), identity.PublicKey...), ChallengeId: challenge.GetChallengeId(), Signature: signature, SignedAtUnixNano: signedAt.UnixNano(),
				},
			})
			clear(signature)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Enrolled device %s\n", response.GetSession().GetDeviceId())
			return nil
		},
	}
}

func v3CloudStatusCommand() *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{
		Use:   "status",
		Short: "Show signed installation and cloud session status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cloudInstaller, err := newV3CloudInstallerForCommand()
			if err != nil {
				return err
			}
			installed, err := cloudInstaller.Status()
			if err != nil {
				return err
			}
			client, err := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI,
				cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
				cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
			)
			if err != nil {
				return err
			}
			defer client.Close()
			status, err := client.Status(cmd.Context(), &cloudpb.StatusRequest{})
			if err != nil {
				return err
			}
			view := cloudStatusView{Installed: true, Version: installed.Version, Channel: installed.Channel, State: status.GetState().String(), AccountLabel: status.GetAccountLabel(), AccountID: status.GetAccountId(), DeviceID: status.GetDeviceId(), ExpiresAtUnix: status.GetSessionExpiresAtUnix()}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloud Companion %s (%s): %s\n", view.Version, view.Channel, view.State)
			if view.AccountLabel != "" || view.AccountID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Account: %s\n", cloudAccountLabel(statusToSession(status)))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func v3CloudDoctorCommand() *cobra.Command {
	jsonOutput := false
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Run redacted Cloud Companion diagnostics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI,
				cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION,
				cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT,
			)
			if err != nil {
				return err
			}
			defer client.Close()
			response, err := client.Doctor(cmd.Context(), &cloudpb.DoctorRequest{})
			if err != nil {
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
			}
			for _, item := range response.GetItems() {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.GetSeverity().String(), item.GetCode(), item.GetMessage())
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON")
	return command
}

func v3CloudLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the local account cloud session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI, cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION)
			if err != nil {
				return err
			}
			defer client.Close()
			if _, err := client.Logout(cmd.Context(), &cloudpb.LogoutRequest{AccountSession: true}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cloud account session removed")
			return nil
		},
	}
}

func v3CloudUninstallCommand() *cobra.Command {
	purge := false
	command := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Cloud Companion artifact",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cloudInstaller, err := newV3CloudInstallerForCommand()
			if err != nil {
				return err
			}
			if _, err := cloudInstaller.Status(); err != nil && !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) && !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
				return err
			}
			capabilities := []cloudpb.CompanionCapability(nil)
			if purge {
				capabilities = []cloudpb.CompanionCapability{cloudpb.CompanionCapability_COMPANION_CAPABILITY_ACCOUNT_SESSION, cloudpb.CompanionCapability_COMPANION_CAPABILITY_DEVICE_ENROLLMENT}
			}
			client, openErr := openV3CloudLifecycleClient(cmd.Context(), cloudpb.CallerRole_CALLER_ROLE_CLI, capabilities...)
			if openErr == nil {
				if purge {
					if _, err := client.Logout(cmd.Context(), &cloudpb.LogoutRequest{AccountSession: true, DeviceSession: true}); err != nil {
						client.Close()
						return err
					}
				}
				_, shutdownErr := client.Shutdown(cmd.Context(), &cloudpb.ShutdownRequest{Reason: "explicit_uninstall"})
				_ = client.Close()
				if shutdownErr != nil {
					return shutdownErr
				}
				waitV3CloudCompanionStopped(cmd.Context())
			} else if purge {
				return openErr
			} else if !cloudcompanion.IsCode(openErr, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) && !cloudcompanion.IsCode(openErr, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING) && !cloudcompanion.IsCode(openErr, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED) {
				return openErr
			}
			if err := cloudInstaller.Uninstall(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cloud Companion uninstalled")
			return nil
		},
	}
	command.Flags().BoolVar(&purge, "purge", false, "also delete account and device cloud sessions")
	return command
}

type cloudStatusView struct {
	Installed     bool   `json:"installed"`
	Version       string `json:"version"`
	Channel       string `json:"channel"`
	State         string `json:"state"`
	AccountLabel  string `json:"account_label,omitempty"`
	AccountID     string `json:"account_id,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	ExpiresAtUnix uint64 `json:"expires_at_unix,omitempty"`
}

func defaultReadV3CloudEnrollmentCode(cmd *cobra.Command) ([]byte, error) {
	if input, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(input.Fd())) {
		fmt.Fprint(cmd.ErrOrStderr(), "One-time enrollment code: ")
		value, err := term.ReadPassword(int(input.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimSpace(string(value))), nil
	}
	value, err := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), 4097)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		return nil, fmt.Errorf("one-time enrollment code is too long")
	}
	return []byte(value), nil
}

func v3RemoteIdentityDir() string {
	return filepath.Join(filepath.Dir(v3RemoteCredentialDir()), "identity")
}

func cloudAccountLabel(session *cloudpb.CloudSessionSummary) string {
	if session == nil {
		return "unknown account"
	}
	if session.GetAccountLabel() != "" {
		return session.GetAccountLabel()
	}
	if session.GetAccountId() != "" {
		return session.GetAccountId()
	}
	return "unknown account"
}

func statusToSession(status *cloudpb.StatusResponse) *cloudpb.CloudSessionSummary {
	if status == nil {
		return nil
	}
	return &cloudpb.CloudSessionSummary{AccountLabel: status.GetAccountLabel(), AccountId: status.GetAccountId(), DeviceId: status.GetDeviceId(), ExpiresAtUnix: status.GetSessionExpiresAtUnix()}
}

func waitV3CloudCompanionStopped(ctx context.Context) {
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	endpoint := strings.TrimSpace(os.Getenv(v3CloudCompanionSocketEnv))
	if endpoint == "" {
		endpoint = ipc.DefaultEndpoint()
	}
	for {
		client, err := ipc.Dial(deadline, endpoint)
		if err != nil {
			return
		}
		_ = client.Close()
		select {
		case <-deadline.Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
	}
}
