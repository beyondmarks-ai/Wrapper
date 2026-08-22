//go:build windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
)

const (
	agentRunKey     = `Software\Microsoft\Windows\CurrentVersion\Run`
	agentRunValue   = "WrapperAgent"
	detachedProcess = 0x00000008
	newProcessGroup = 0x00000200
)

func installAgent() error {
	agentPath, err := installedAgentPath()
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, agentRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open per-user startup settings: %w", err)
	}
	defer key.Close()
	if err = key.SetStringValue(agentRunValue, quoteWindowsArgument(agentPath)); err != nil {
		return fmt.Errorf("install Wrapper Agent autostart: %w", err)
	}
	return nil
}

func startAgent() error {
	if _, err := runningAgentInfo(); err == nil {
		return nil
	}
	agentPath, err := installedAgentPath()
	if err != nil {
		return err
	}
	command := exec.Command(agentPath)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | newProcessGroup,
	}
	if err = command.Start(); err != nil {
		return fmt.Errorf("start Wrapper Agent: %w", err)
	}
	_ = command.Process.Release()

	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, lastErr = runningAgentInfo(); lastErr == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Wrapper Agent did not become ready: %w", lastErr)
}

func stopAgent() error {
	info, err := runningAgentInfo()
	if err != nil {
		return nil
	}
	if info.PID <= 0 {
		return errors.New("Wrapper Agent returned an invalid process ID")
	}
	process, err := os.FindProcess(info.PID)
	if err != nil {
		return fmt.Errorf("find Wrapper Agent process: %w", err)
	}
	if err = process.Kill(); err != nil {
		return fmt.Errorf("stop Wrapper Agent: %w", err)
	}
	return nil
}

func uninstallAgent() error {
	stopErr := stopAgent()
	key, err := registry.OpenKey(registry.CURRENT_USER, agentRunKey, registry.SET_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return stopErr
	}
	if err != nil {
		return fmt.Errorf("open per-user startup settings: %w", err)
	}
	defer key.Close()
	if err = key.DeleteValue(agentRunValue); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return fmt.Errorf("remove Wrapper Agent autostart: %w", err)
	}
	return stopErr
}

func agentStatus() (string, error) {
	installed := false
	key, err := registry.OpenKey(registry.CURRENT_USER, agentRunKey, registry.QUERY_VALUE)
	if err == nil {
		_, _, valueErr := key.GetStringValue(agentRunValue)
		installed = valueErr == nil
		key.Close()
	} else if !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", fmt.Errorf("read per-user startup settings: %w", err)
	}
	info, runErr := runningAgentInfo()
	if runErr == nil {
		return fmt.Sprintf("Wrapper Agent is running (PID %d). Autostart installed: %t.", info.PID, installed), nil
	}
	if installed {
		return "Wrapper Agent is installed for this user but is not running.", nil
	}
	return "Wrapper Agent is not installed and is not running.", nil
}

func runningAgentInfo() (localagent.StatusInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	return localagent.NewClient().StatusInfo(ctx)
}

func installedAgentPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	agentPath := filepath.Join(filepath.Dir(executable), "wrapper-agent.exe")
	if _, err = os.Stat(agentPath); err != nil {
		return "", fmt.Errorf("wrapper-agent.exe must be installed beside wrap.exe: %w", err)
	}
	return agentPath, nil
}

func quoteWindowsArgument(value string) string {
	return `"` + value + `"`
}
