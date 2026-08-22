package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	variable "github.com/beyondmarks-ai/Wrapper/src/config"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/cloudauth"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/cloudstate"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remoteclient"
)

func authCommand() *cli.Command {
	return &cli.Command{
		Name: "auth", Usage: "Sign in to Wrapper Cloud",
		Commands: []*cli.Command{
			{Name: "login", Usage: "Sign in with Google in your browser", Action: func(ctx context.Context, _ *cli.Command) error {
				manager, err := cloudAuthManager()
				if err != nil {
					return err
				}
				loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				tokens, err := manager.Login(loginCtx)
				if err != nil {
					return err
				}
				fmt.Printf("Signed in as %s.\n", tokens.Email)
				return nil
			}},
			{Name: "status", Usage: "Show the current sign-in", Action: func(_ context.Context, _ *cli.Command) error {
				manager, err := cloudAuthManager()
				if err != nil {
					return err
				}
				status := manager.Status()
				if status.UserID == "" {
					fmt.Println("Not signed in.")
					return nil
				}
				fmt.Printf("Signed in as %s (UID: %s).\n", status.Email, status.UserID)
				return nil
			}},
			{Name: "logout", Usage: "Remove protected cloud credentials", Action: func(_ context.Context, _ *cli.Command) error {
				manager, err := cloudAuthManager()
				if err != nil {
					return err
				}
				if err = manager.Logout(); err != nil {
					return err
				}
				fmt.Println("Signed out. Paired-device keys remain protected on this PC until the device is revoked.")
				return nil
			}},
		},
	}
}

func deviceCommand() *cli.Command {
	return &cli.Command{
		Name: "device", Usage: "Register, pair, and revoke computers",
		Commands: []*cli.Command{
			{
				Name: "register", Usage: "Register this Windows PC after signing in",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Usage: "Friendly device name", Required: true},
					&cli.StringFlag{Name: "api-url", Usage: "Wrapper Cloud HTTPS API URL"},
				},
				Action: registerCloudDevice,
			},
			{Name: "list", Usage: "List registered devices", Action: func(ctx context.Context, _ *cli.Command) error {
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				devices, err := client.ListDevices(ctx)
				if err != nil {
					return err
				}
				for _, device := range devices {
					status := "offline"
					if device.Online && time.Since(device.LastSeen) < 90*time.Second {
						status = "online"
					}
					fmt.Printf("%-36s  %-24s  %s\n", device.ID, device.Name, status)
				}
				return nil
			}},
			{Name: "code", Usage: "Create a 10-minute pairing code", Action: func(ctx context.Context, _ *cli.Command) error {
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				code, expires, err := client.CreatePairingCode(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("Pairing code: %s\nExpires: %s\n", code, expires.Local().Format(time.Kitchen))
				return nil
			}},
			{Name: "pair", Usage: "Claim a pairing code on this PC", ArgsUsage: "<code>", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("provide the pairing code shown on the other PC")
				}
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				pairing, err := client.ClaimPairing(ctx, command.Args().First())
				if err != nil {
					return err
				}
				fmt.Printf("Pairing claimed. On the source PC run:\n  wrap device confirm %s\n", pairing.ID)
				return nil
			}},
			{Name: "confirm", Usage: "Approve a claimed pairing", ArgsUsage: "<pairing-id>", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("provide the pairing ID")
				}
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				if _, err = client.ConfirmPairing(ctx, command.Args().First()); err != nil {
					return err
				}
				fmt.Println("Devices paired. Transfers inside shared roots are now allowed.")
				return nil
			}},
			{Name: "revoke", Usage: "Revoke a registered device", ArgsUsage: "<device-id>", Action: func(ctx context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("provide the device ID to revoke")
				}
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				if err = client.RevokeDevice(ctx, command.Args().First()); err != nil {
					return err
				}
				fmt.Println("Device revoked.")
				return nil
			}},
		},
	}
}

func shareCommand() *cli.Command {
	return &cli.Command{
		Name: "share", Usage: "Choose folders available to paired devices",
		Commands: []*cli.Command{
			{Name: "add", ArgsUsage: "<folder>", Action: func(_ context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("provide one folder to share")
				}
				state, err := cloudstate.Load(variable.CloudStateFile)
				if err != nil {
					return err
				}
				if err = cloudstate.AddSharedRoot(&state, command.Args().First()); err != nil {
					return err
				}
				if err = cloudstate.Save(variable.CloudStateFile, state); err != nil {
					return err
				}
				fmt.Println("Shared root added. Restart the Wrapper agent to apply it.")
				return nil
			}},
			{Name: "remove", ArgsUsage: "<folder>", Action: func(_ context.Context, command *cli.Command) error {
				if command.Args().Len() != 1 {
					return errors.New("provide one shared folder to remove")
				}
				state, err := cloudstate.Load(variable.CloudStateFile)
				if err != nil {
					return err
				}
				if err = cloudstate.RemoveSharedRoot(&state, command.Args().First()); err != nil {
					return err
				}
				return cloudstate.Save(variable.CloudStateFile, state)
			}},
			{Name: "list", Action: func(_ context.Context, _ *cli.Command) error {
				state, err := cloudstate.Load(variable.CloudStateFile)
				if err != nil {
					return err
				}
				if len(state.SharedRoots) == 0 {
					fmt.Println("No folders are shared. Use 'wrap share add <folder>'.")
				}
				for _, root := range state.SharedRoots {
					fmt.Println(root)
				}
				return nil
			}},
		},
	}
}

func transferCommand() *cli.Command {
	return &cli.Command{
		Name: "transfer", Usage: "Inspect cloud transfer state",
		Commands: []*cli.Command{
			{Name: "list", Action: func(ctx context.Context, _ *cli.Command) error {
				_, client, _, err := cloudRuntime()
				if err != nil {
					return err
				}
				transfers, err := client.ListTransfers(ctx)
				if err != nil {
					return err
				}
				for _, transfer := range transfers {
					fmt.Printf("%-36s  %-12s  %s\n", transfer.ID, transfer.State, transfer.ExpiresAt.Local().Format(time.RFC822))
				}
				return nil
			}},
		},
	}
}

func registerCloudDevice(ctx context.Context, command *cli.Command) error {
	manager, err := cloudAuthManager()
	if err != nil {
		return err
	}
	identity, err := remote.LoadIdentity(variable.CloudIdentityFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		identity, err = remote.NewIdentity()
		if err != nil {
			return err
		}
		if err = remote.SaveIdentity(variable.CloudIdentityFile, identity); err != nil {
			return err
		}
	}
	apiURL := command.String("api-url")
	if apiURL == "" {
		apiURL = variable.CloudAPIURL()
	}
	if apiURL == "" {
		return errors.New("Wrapper Cloud API URL is not configured")
	}
	client, err := remoteclient.New(apiURL, &http.Client{Timeout: 35 * time.Second}, manager, "", identity)
	if err != nil {
		return err
	}
	recipient, _ := identity.Recipient()
	signingKey, _ := identity.SigningPublicKey()
	device, err := client.RegisterDevice(ctx, command.String("name"), recipient, signingKey)
	if err != nil {
		return err
	}
	downloadDir := filepath.Join(variable.HomeDir, "Downloads", "Wrapper")
	state := cloudstate.State{
		APIURL: apiURL, Device: device, DownloadDir: downloadDir, IdentityPath: variable.CloudIdentityFile,
	}
	if err = cloudstate.Save(variable.CloudStateFile, state); err != nil {
		return err
	}
	if agentErr := installAgent(); agentErr == nil {
		if agentErr = startAgent(); agentErr != nil {
			return fmt.Errorf("start Wrapper Agent: %w", agentErr)
		}
	} else if !errors.Is(agentErr, errAgentUnsupported) {
		return fmt.Errorf("install Wrapper Agent: %w", agentErr)
	}
	fmt.Printf("Registered %s (%s).\nWrapper Agent is ready. Add a shared root with: wrap share add <folder>\n", device.Name, device.ID)
	return nil
}

func cloudAuthManager() (*cloudauth.Manager, error) {
	apiURL := strings.TrimRight(variable.CloudAPIURL(), "/")
	return cloudauth.NewManager(cloudauth.Config{
		GoogleClientID: variable.GoogleClientID(), GoogleExchangeURL: apiURL + "/v1/auth/google/token",
		FirebaseAPIKey: variable.FirebaseAPIKey(), TokenPath: variable.CloudTokensFile,
	})
}

func cloudRuntime() (cloudstate.State, *remoteclient.Client, remote.Identity, error) {
	state, err := cloudstate.Load(variable.CloudStateFile)
	if err != nil {
		return state, nil, remote.Identity{}, err
	}
	identity, err := remote.LoadIdentity(state.IdentityPath)
	if err != nil {
		return state, nil, identity, err
	}
	manager, err := cloudAuthManager()
	if err != nil {
		return state, nil, identity, err
	}
	client, err := remoteclient.New(state.APIURL, &http.Client{Timeout: 35 * time.Second}, manager, state.Device.ID, identity)
	return state, client, identity, err
}
