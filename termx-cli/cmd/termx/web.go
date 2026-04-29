package main

import (
	"github.com/lozzow/termx/termx-cli/internal/webshell"
	"github.com/spf13/cobra"
)

func webCommand(socket *string, logFile *string) *cobra.Command {
	return webshell.NewCommand(socket, logFile, webshell.Dependencies{
		OpenLogger:        openLogFileLogger,
		ResolveSocket:     resolveSocket,
		DialOrStartClient: dialOrStartClient,
		CurrentSize:       currentSize,
	})
}
