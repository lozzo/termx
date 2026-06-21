package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

func remoteCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage TermX remote access",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.SetContext(remoteContextWithConfigPath(cmd.Context(), configPath))
		},
	}
	cmd.AddCommand(remoteLoginCommand(configPath))
	cmd.AddCommand(remoteStatusCommand(socket, logFile))
	cmd.AddCommand(remoteInfoCommand(socket, logFile))
	cmd.AddCommand(remoteEnableCommand(socket, logFile, configPath))
	cmd.AddCommand(remoteDisableCommand(socket, logFile))
	cmd.AddCommand(remotePairCommand(socket, logFile))
	cmd.AddCommand(remoteOpenCommand(socket, logFile))
	return cmd
}

func remoteStatusCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := remoteStatusClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			localStatus, err := remoteLocalStatusClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Remote *remoteprotocol.Status      `json:"remote"`
					Local  *remoteprotocol.LocalStatus `json:"local"`
				}{Remote: status, Local: localStatus})
			}

			printRemoteStatus(cmd.OutOrStdout(), status)
			printRemoteLocalStatus(cmd.OutOrStdout(), localStatus)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteInfoCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"show"},
		Short:   "Show remote runtime and local web details",
		RunE: func(cmd *cobra.Command, args []string) error {
			remoteStatus, err := remoteStatusClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			localStatus, err := remoteLocalStatusClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Remote *remoteprotocol.Status      `json:"remote"`
					Local  *remoteprotocol.LocalStatus `json:"local"`
				}{Remote: remoteStatus, Local: localStatus})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "[remote]")
			printRemoteStatus(cmd.OutOrStdout(), remoteStatus)
			fmt.Fprintln(cmd.OutOrStdout(), "[local]")
			printRemoteLocalStatus(cmd.OutOrStdout(), localStatus)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remoteEnableCommand(socket *string, logFile *string, configPath *string) *cobra.Command {
	var enableMode string
	var outputJSON bool
	var addr string
	var iceTCPAddr string
	var hubURL string
	var token string
	var tokenEnv string
	var tokenFile string
	var browser bool
	var browserTimeout time.Duration
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable remote access features",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(enableMode))
			if mode != "local" && mode != "hub" && mode != "both" {
				return fmt.Errorf("--mode must be local, hub, or both")
			}
			if mode == "local" && browser {
				return fmt.Errorf("--browser is only valid for hub or both mode")
			}
			hasTokenSource := strings.TrimSpace(token) != "" || strings.TrimSpace(tokenEnv) != "" || strings.TrimSpace(tokenFile) != ""
			resolvedToken, err := resolveSecretFlag(token, tokenEnv, tokenFile)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(resolvedToken)
			if browser && hasTokenSource {
				return fmt.Errorf("--browser cannot be used with --token, --token-env, or --token-file")
			}
			switch mode {
			case "both":
				hubRuntime, err := runRemoteHubEnable(cmd, configPath, "", hubURL, token, browser, noBrowser, browserTimeout, outputJSON, mode, addr, iceTCPAddr)
				if err != nil {
					return err
				}
				return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, hubRuntime, outputJSON)
			case "local":
				if err := ensureRemoteConfigBootstrap(*configPath, "", "", "", mode, addr, iceTCPAddr); err != nil {
					return err
				}
				return runRemoteLocalEnable(cmd, socket, logFile, addr, iceTCPAddr, remoteprotocol.Config{Enabled: true, Mode: mode}, outputJSON)
			case "hub":
				_, err := runRemoteHubEnable(cmd, configPath, "", hubURL, token, browser, noBrowser, browserTimeout, outputJSON, mode, "", "")
				return err
			default:
				return fmt.Errorf("--mode must be local, hub, or both")
			}
		},
	}
	cmd.Flags().StringVar(&enableMode, "mode", "both", "connection mode: local, hub, or both")
	cmd.Flags().StringVar(&addr, "addr", defaultRemoteLocalWebAddr, "local web listen address")
	cmd.Flags().StringVar(&iceTCPAddr, "ice-tcp-addr", defaultRemoteLocalICEAddr, "local ICE TCP listen address")
	cmd.Flags().StringVar(&hubURL, "hub-url", "", "optional explicit Hub URL; normally discovered from Web Control")
	cmd.Flags().StringVar(&token, "token", "", "Web Control access token for automation")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable containing the Web Control connection key for automation")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "file containing the Web Control connection key for automation")
	cmd.Flags().BoolVar(&browser, "browser", false, "force browser login instead of using a saved token")
	cmd.Flags().DurationVar(&browserTimeout, "timeout", 5*time.Minute, "browser login timeout")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the browser login URL without opening it")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	_ = cmd.Flags().MarkHidden("hub-url")
	_ = cmd.Flags().MarkHidden("token-env")
	_ = cmd.Flags().MarkHidden("token-file")
	return cmd
}

func remoteDisableCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable the local remote web/ICE TCP runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := remoteLocalDisableClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}
			printRemoteLocalStatus(cmd.OutOrStdout(), status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return cmd
}

func remotePairCommand(socket *string, logFile *string) *cobra.Command {
	var outputJSON bool
	var uriOnly bool
	var localURL string
	var ttl time.Duration
	var authTTL time.Duration
	var authTTLSet bool
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Create a TermX remote pairing QR",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputJSON && uriOnly {
				return fmt.Errorf("--json and --uri cannot be used together")
			}
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			if authTTL <= 0 {
				return fmt.Errorf("--auth-ttl must be greater than zero")
			}
			authTTLSeconds := 0
			if authTTLSet {
				authTTLSeconds = int(authTTL.Seconds())
			}
			localURL = strings.TrimSpace(localURL)
			if localURL == "" {
				localStatus, err := remoteLocalStatusClient(cmd.Context(), *socket, *logFile)
				if err != nil {
					return err
				}
				if localStatus == nil || !localStatus.Enabled || strings.TrimSpace(localStatus.LocalPairURL) == "" {
					localURL = ""
				} else {
					localURL = localStatus.LocalPairURL
				}
			}
			result, err := pairStartClient(context.Background(), *socket, *logFile, remoteprotocol.PairStartParams{
				LocalPairURL:   localURL,
				TTLSeconds:     int(ttl.Seconds()),
				AuthTTLSeconds: authTTLSeconds,
			})
			if err != nil {
				return err
			}
			payload := buildRemotePairPayload(result)
			uri, err := termxPairURI(payload)
			if err != nil {
				return err
			}
			if uriOnly {
				fmt.Fprintln(cmd.OutOrStdout(), uri)
				return nil
			}
			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					URI     string         `json:"uri"`
					Payload map[string]any `json:"payload"`
				}{
					URI:     uri,
					Payload: payload,
				})
			}
			qr, err := qrcode.New(uri, qrcode.Medium)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), qr.ToSmallString(false))
			fmt.Fprintf(cmd.OutOrStdout(), "uri:\t%s\n", uri)
			fmt.Fprintf(cmd.OutOrStdout(), "expires_at:\t%s\n", result.ExpiresAt.Format(time.RFC3339))
			if authTTLSet {
				fmt.Fprintf(cmd.OutOrStdout(), "authorization_ttl:\t%s\n", authTTL.String())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&uriOnly, "uri", false, "print only the termx://pair URI")
	cmd.Flags().StringVar(&localURL, "local-url", "", "local pair URL; defaults to running local remote")
	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "pair session TTL")
	cmd.Flags().DurationVar(&authTTL, "auth-ttl", 24*time.Hour, "authorization session token TTL")
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		authTTLSet = cmd.Flags().Changed("auth-ttl")
	}
	_ = cmd.Flags().MarkHidden("local-url")
	return cmd
}

func remoteOpenCommand(socket *string, logFile *string) *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the local remote web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			localStatus, err := remoteLocalStatusClient(cmd.Context(), *socket, *logFile)
			if err != nil {
				return err
			}
			if localStatus == nil || !localStatus.Enabled || strings.TrimSpace(localStatus.HTTPURL) == "" {
				return fmt.Errorf("local remote is not enabled; run `termx remote enable --mode local` first")
			}
			if printOnly {
				fmt.Fprintln(cmd.OutOrStdout(), localStatus.HTTPURL)
				return nil
			}
			if err := openBrowser(localStatus.HTTPURL); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), localStatus.HTTPURL)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print URL without opening a browser")
	return cmd
}

func runRemoteLocalEnable(cmd *cobra.Command, socket *string, logFile *string, addr string, iceTCPAddr string, hub remoteprotocol.Config, outputJSON bool) error {
	status, err := remoteLocalEnableClient(cmd.Context(), *socket, *logFile, remoteprotocol.LocalEnableParams{
		LocalWebAddr: addr,
		ICETCPAddr:   iceTCPAddr,
		HubURLs:      compactStringList(hub.HubURLs),
		ControlURL:   hub.ControlURL,
		AccessToken:  hub.AccessToken,
		Region:       hub.Region,
	})
	if err != nil {
		return err
	}
	if outputJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	printRemoteLocalStatus(cmd.OutOrStdout(), status)
	return nil
}

func runRemoteHubEnable(cmd *cobra.Command, configPath *string, controlURL string, hubURL string, token string, forceBrowser bool, noBrowser bool, browserTimeout time.Duration, outputJSON bool, mode string, localWebAddr string, iceTCPAddr string) (remoteprotocol.Config, error) {
	return runRemoteHubEnableWithToken(cmd, configPath, controlURL, hubURL, token, forceBrowser, noBrowser, browserTimeout, outputJSON, mode, localWebAddr, iceTCPAddr)
}

func runRemoteHubEnableWithToken(cmd *cobra.Command, configPath *string, controlURL string, hubURL string, token string, forceBrowser bool, noBrowser bool, browserTimeout time.Duration, outputJSON bool, mode string, localWebAddr string, iceTCPAddr string) (remoteprotocol.Config, error) {
	controlURL = strings.TrimSpace(controlURL)
	hubURL = strings.TrimSpace(hubURL)
	token = strings.TrimSpace(token)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "hub"
	}
	if controlURL == "" || hubURL == "" || (!forceBrowser && token == "") {
		cfg, cfgErr := remoteConfigFromFileAndEnv(*configPath)
		if cfgErr != nil {
			return remoteprotocol.Config{}, cfgErr
		}
		if controlURL == "" {
			controlURL = cfg.ControlURL
		}
		if hubURL == "" {
			hubURL = cfg.HubURL
		}
		if !forceBrowser && token == "" {
			token = cfg.AccessToken
		}
	}
	if controlURL == "" {
		controlURL = defaultRemoteControlURL
	}
	if controlURL == "" {
		return remoteprotocol.Config{}, fmt.Errorf("Web Control URL is not configured")
	}
	var refreshToken string
	if forceBrowser || token == "" {
		created, err := remoteLoginHTTPClient.CreateBrowserLogin(cmd.Context(), controlURL, "termx local service")
		if err != nil {
			return remoteprotocol.Config{}, err
		}
		var progress io.Writer = cmd.OutOrStdout()
		if outputJSON {
			progress = cmd.ErrOrStderr()
		}
		auth, err := completeRemoteBrowserLogin(cmd, controlURL, created, browserTimeout, !noBrowser, progress)
		if err != nil {
			return remoteprotocol.Config{}, err
		}
		token = strings.TrimSpace(auth.AccessToken)
		refreshToken = strings.TrimSpace(auth.RefreshToken)
		if token == "" {
			return remoteprotocol.Config{}, fmt.Errorf("control did not return a connection key")
		}
	}
	if _, err := remoteLoginHTTPClient.Me(cmd.Context(), controlURL, token); err != nil {
		return remoteprotocol.Config{}, err
	}
	authStorePath, err := remoteAuthStorePath(*configPath)
	if err != nil {
		return remoteprotocol.Config{}, err
	}
	if err := saveRemoteAuthRecord(authStorePath, remoteAuthRecord{
		ControlURL:   controlURL,
		HubURL:       hubURL,
		AccessToken:  token,
		RefreshToken: refreshToken,
	}); err != nil {
		return remoteprotocol.Config{}, err
	}
	if err := ensureRemoteConfigBootstrap(*configPath, controlURL, hubURL, authStorePath, mode, localWebAddr, iceTCPAddr); err != nil {
		return remoteprotocol.Config{}, err
	}
	hubURLs := compactStringList([]string{hubURL})
	if hubURL == "" {
		hubURLs = nil
	}
	hub := remoteprotocol.Config{
		Enabled:      true,
		ControlURL:   controlURL,
		HubURL:       hubURL,
		HubURLs:      hubURLs,
		AccessToken:  token,
		Mode:         mode,
		LocalWebAddr: strings.TrimSpace(localWebAddr),
		ICETCPAddr:   strings.TrimSpace(iceTCPAddr),
	}
	if outputJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return hub, enc.Encode(struct {
			Enabled    bool   `json:"enabled"`
			ControlURL string `json:"control_url"`
			HubURL     string `json:"hub_url,omitempty"`
			Mode       string `json:"mode"`
			AuthStore  string `json:"auth_store"`
		}{
			Enabled:    true,
			ControlURL: controlURL,
			HubURL:     hubURL,
			Mode:       mode,
			AuthStore:  authStorePath,
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), "hub_enabled:\ttrue")
	fmt.Fprintf(cmd.OutOrStdout(), "control_url:\t%s\n", controlURL)
	if hubURL != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "hub_url:\t%s\n", hubURL)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "mode:\t%s\n", mode)
	fmt.Fprintf(cmd.OutOrStdout(), "auth_store:\t%s\n", authStorePath)
	fmt.Fprintln(cmd.OutOrStdout(), "next:\trun `termx daemon` to keep hub remote connected")
	return hub, nil
}
