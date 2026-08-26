package executor

import (
	"fmt"
)

// NewBackend creates a Backend instance based on configuration.
func NewBackend(config *BackendConfig) (Backend, error) {
	if config == nil {
		config = DefaultBackendConfig()
	}
	if config.ClaudeCode != nil || config.OpenCode != nil || config.QwenCode != nil || config.OpenAI != nil || config.APIBaseURL != "" || config.APIAuthToken != "" {
		return nil, fmt.Errorf("legacy executor configuration is not supported: configure executor.omp and its OMP profile instead")
	}

	switch config.Type {
	case BackendTypeOMP, "":
		b := NewOMPBackend(config.OMP)
		b.SetHeartbeatTimeout(config.EffectiveHeartbeatTimeout())
		return b, nil

	default:
		return nil, fmt.Errorf("unsupported executor type %q: configure executor.omp instead", config.Type)
	}
}

// NewBackendFromType creates a Backend instance using default config for the type.
func NewBackendFromType(backendType string) (Backend, error) {
	config := DefaultBackendConfig()
	config.Type = backendType
	return NewBackend(config)
}
