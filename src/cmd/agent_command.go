package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"
)

func agentCommand() *cli.Command {
	return &cli.Command{
		Name: "agent", Usage: "Manage the background transfer agent",
		Commands: []*cli.Command{
			{Name: "install", Usage: "Start Wrapper Agent automatically at sign-in", Action: func(_ context.Context, _ *cli.Command) error {
				if err := installAgent(); err != nil {
					return err
				}
				if err := startAgent(); err != nil {
					return err
				}
				fmt.Println("Wrapper Agent installed and started.")
				return nil
			}},
			{Name: "start", Usage: "Start the installed agent", Action: func(_ context.Context, _ *cli.Command) error { return startAgent() }},
			{Name: "stop", Usage: "Stop the installed agent", Action: func(_ context.Context, _ *cli.Command) error { return stopAgent() }},
			{Name: "status", Usage: "Show the installed agent status", Action: func(_ context.Context, _ *cli.Command) error {
				status, err := agentStatus()
				if status != "" {
					fmt.Println(status)
				}
				return err
			}},
			{Name: "uninstall", Usage: "Remove agent autostart without deleting cloud identity", Action: func(_ context.Context, _ *cli.Command) error {
				if err := uninstallAgent(); err != nil {
					return err
				}
				fmt.Println("Wrapper Agent autostart removed.")
				return nil
			}},
		},
	}
}

var errAgentUnsupported = errors.New("Wrapper Agent installation is currently supported only on Windows")
