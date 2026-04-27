package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lozzow/termx/remote/controlplane"
	"github.com/lozzow/termx/tuiv2/shared"
	"github.com/spf13/cobra"
)

func loginCommand(configPath *string) *cobra.Command {
	var serverURL string
	var username string
	var passwordStdin bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to the termx control plane",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadMutableConfig(*configPath)
			if err != nil {
				return err
			}
			resolvedServer := resolveServerURL(serverURL, cfg.Auth.ServerURL)
			if resolvedServer == "" {
				return fmt.Errorf("control plane server url is required")
			}
			if !passwordStdin {
				return fmt.Errorf("use --password-stdin to provide the login password securely")
			}
			password, err := readPassword(cmd.InOrStdin())
			if err != nil {
				return err
			}
			client := controlplane.NewClient(resolvedServer, nil)
			session, err := client.Login(context.Background(), username, password)
			if err != nil {
				return err
			}

			cfg.Auth = shared.AuthConfig{
				ServerURL:    client.BaseURL(),
				AccessToken:  session.Token,
				RefreshToken: session.RefreshToken,
				UserID:       session.User.ID,
				Username:     session.User.Username,
			}
			if err := shared.SaveConfig(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", session.User.Username, session.User.ID, client.BaseURL())
			return nil
		},
	}
	cmd.Flags().StringVar(&serverURL, "server", "", "control plane base URL")
	cmd.Flags().StringVar(&username, "username", "", "username")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the login password from stdin")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func logoutCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored control plane credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadMutableConfig(*configPath)
			if err != nil {
				return err
			}
			serverURL := cfg.Auth.ServerURL
			refreshToken := cfg.Auth.RefreshToken
			if serverURL != "" && refreshToken != "" {
				client := controlplane.NewClient(serverURL, nil)
				if err := client.Logout(context.Background(), refreshToken); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: remote logout failed: %v\n", err)
				}
			}
			cfg.Auth.AccessToken = ""
			cfg.Auth.RefreshToken = ""
			cfg.Auth.UserID = ""
			cfg.Auth.Username = ""
			return shared.SaveConfig(path, cfg)
		},
	}
}

func whoamiCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current control plane identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadMutableConfig(*configPath)
			if err != nil {
				return err
			}
			if cfg.Auth.ServerURL == "" || cfg.Auth.AccessToken == "" {
				return fmt.Errorf("not logged in")
			}
			client := controlplane.NewClient(cfg.Auth.ServerURL, nil)
			user, err := client.Me(context.Background(), cfg.Auth.AccessToken)
			if controlplane.IsUnauthorized(err) && cfg.Auth.RefreshToken != "" {
				session, refreshErr := client.Refresh(context.Background(), cfg.Auth.RefreshToken)
				if refreshErr != nil {
					return refreshErr
				}
				cfg.Auth.AccessToken = session.Token
				if session.RefreshToken != "" {
					cfg.Auth.RefreshToken = session.RefreshToken
				}
				if session.User.ID != "" {
					cfg.Auth.UserID = session.User.ID
				}
				if session.User.Username != "" {
					cfg.Auth.Username = session.User.Username
				}
				if err := shared.SaveConfig(path, cfg); err != nil {
					return err
				}
				user, err = client.Me(context.Background(), cfg.Auth.AccessToken)
			}
			if err != nil {
				return err
			}
			if user.ID != "" {
				cfg.Auth.UserID = user.ID
			}
			if user.Username != "" {
				cfg.Auth.Username = user.Username
			}
			if err := shared.SaveConfig(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", cfg.Auth.Username, cfg.Auth.UserID, cfg.Auth.ServerURL)
			return nil
		},
	}
}

func loadMutableConfig(configPath string) (shared.Config, string, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = shared.DefaultConfigPath()
	}
	cfg, err := shared.LoadConfig(configPath)
	if err != nil {
		return shared.Config{}, "", err
	}
	cfg.ConfigPath = configPath
	return cfg, configPath, nil
}

func resolveServerURL(input, fallback string) string {
	if trimmed := strings.TrimSpace(input); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func readPassword(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return "", fmt.Errorf("password is required")
	}
	return password, nil
}
