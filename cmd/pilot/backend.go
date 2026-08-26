package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qf-studio/pilot/internal/config"
	"github.com/qf-studio/pilot/internal/executor"
)

// backendInfo holds information about a supported backend
type backendInfo struct {
	Name       string
	Command    string
	ConfigKey  string
	getVersion func(cmd string) string
}

var supportedBackends = []backendInfo{
	{
		Name:      executor.BackendTypeOMP,
		Command:   "omp",
		ConfigKey: "omp",
		getVersion: func(cmd string) string {
			out, err := exec.Command(cmd, "--version").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	},
}

func newBackendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "Manage execution backends",
		Long: `Manage the Oh My Pi execution runtime.

List supported backends, check their status, and switch the active backend.`,
	}

	cmd.AddCommand(
		newBackendListCmd(),
		newBackendStatusCmd(),
		newBackendSetCmd(),
	)

	return cmd
}

func newBackendListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all supported backends",
		Long: `Show all supported backends and whether their CLI is installed.

Example output:
  Backend        Status      Command    Config
  omp            ✓ installed omp        (default)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config to check which is default
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				// Config doesn't exist yet, use defaults
				cfg = config.DefaultConfig()
			}

			activeBackend := executor.BackendTypeOMP
			if cfg.Executor != nil && cfg.Executor.Type != "" {
				activeBackend = cfg.Executor.Type
			}

			// Print header
			fmt.Printf("%-14s %-12s %-10s %s\n", "Backend", "Status", "Command", "Config")

			for _, backend := range supportedBackends {
				// Get the actual command from config or use default
				command := backend.Command
				if cfg.Executor != nil {
					if cfg.Executor.OMP != nil && cfg.Executor.OMP.Command != "" {
						command = cfg.Executor.OMP.Command
					}
				}

				// Check if installed
				_, err := exec.LookPath(command)
				installed := err == nil

				status := "✗ missing"
				if installed {
					status = "✓ installed"
				}

				configNote := ""
				if backend.Name == activeBackend {
					configNote = "(default)"
				}

				fmt.Printf("%-14s %-12s %-10s %s\n", backend.Name, status, command, configNote)
			}

			return nil
		},
	}
}

func newBackendStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current backend configuration",
		Long: `Show current backend configuration and health.

Example output:
  Active backend: omp
  Command: omp
  Version: 18.0.5
  Status: ✓ ready`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				cfg = config.DefaultConfig()
			}

			activeBackend := executor.BackendTypeOMP
			if cfg.Executor != nil && cfg.Executor.Type != "" {
				activeBackend = cfg.Executor.Type
			}

			// Find backend info
			var info *backendInfo
			for i := range supportedBackends {
				if supportedBackends[i].Name == activeBackend {
					info = &supportedBackends[i]
					break
				}
			}

			if info == nil {
				return fmt.Errorf("unknown backend type: %s", activeBackend)
			}

			// Get the actual command from config
			command := info.Command
			if cfg.Executor != nil {
				if cfg.Executor.OMP != nil && cfg.Executor.OMP.Command != "" {
					command = cfg.Executor.OMP.Command
				}
			}

			// Check installation and version
			_, lookErr := exec.LookPath(command)
			installed := lookErr == nil

			version := ""
			if installed {
				version = info.getVersion(command)
			}

			status := "✗ not ready (CLI not found)"
			if installed {
				status = "✓ ready"
			}

			fmt.Printf("Active backend: %s\n", activeBackend)
			fmt.Printf("Command: %s\n", command)
			if version != "" {
				fmt.Printf("Version: %s\n", version)
			}
			fmt.Printf("Status: %s\n", status)
			fmt.Println()
			fmt.Printf("Config: %s\n", configPath)
			fmt.Printf("  executor.type: %s\n", activeBackend)

			// Show backend-specific config
			if cfg.Executor != nil {
				switch activeBackend {
				case executor.BackendTypeOMP:
					if cfg.Executor.OMP != nil {
						fmt.Printf("  executor.omp.command: %s\n", command)
					}
				}
			}

			return nil
		},
	}
}

func newBackendSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <type>",
		Short: "Set active backend",
		Long: `OMP is the only supported execution runtime.

Valid type: omp

Example:
  pilot backend set omp
  → Updated executor.type to "omp" in ~/.pilot/config.yaml
  → Verified: omp CLI found in PATH`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backendType := args[0]

			// Validate backend type
			validTypes := []string{executor.BackendTypeOMP}
			isValid := false
			for _, t := range validTypes {
				if backendType == t {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("invalid backend type: %s\nValid types: %s", backendType, strings.Join(validTypes, ", "))
			}

			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			// Load existing config
			cfg, err := config.Load(configPath)
			if err != nil {
				// If config doesn't exist, create a default one
				cfg = config.DefaultConfig()
			}

			// Update the backend type
			if cfg.Executor == nil {
				cfg.Executor = executor.DefaultBackendConfig()
			}
			cfg.Executor.Type = backendType

			// Write config back
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			fmt.Printf("Updated executor.type to %q in %s\n", backendType, configPath)

			// Verify CLI is installed
			var info *backendInfo
			for i := range supportedBackends {
				if supportedBackends[i].Name == backendType {
					info = &supportedBackends[i]
					break
				}
			}

			if info != nil {
				command := info.Command
				// Get custom command from config if set
				if cfg.Executor.OMP != nil && cfg.Executor.OMP.Command != "" {
					command = cfg.Executor.OMP.Command
				}

				path, err := exec.LookPath(command)
				if err != nil {
					fmt.Printf("Warning: %s CLI not found in PATH\n", command)
				} else {
					fmt.Printf("Verified: %s CLI found at %s\n", command, path)
				}
			}

			return nil
		},
	}
}
