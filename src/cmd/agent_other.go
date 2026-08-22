//go:build !windows

package cmd

func installAgent() error          { return errAgentUnsupported }
func startAgent() error            { return errAgentUnsupported }
func stopAgent() error             { return errAgentUnsupported }
func uninstallAgent() error        { return errAgentUnsupported }
func agentStatus() (string, error) { return "", errAgentUnsupported }
