package executor

import (
	"testing"
)

func TestNewBackend(t *testing.T) {
	tests := []struct {
		name        string
		config      *BackendConfig
		expectType  string
		expectError bool
	}{
		{
			name:       "nil config defaults to OMP",
			config:     nil,
			expectType: BackendTypeOMP,
		},
		{
			name:       "empty type defaults to OMP",
			config:     &BackendConfig{Type: ""},
			expectType: BackendTypeOMP,
		},
		{
			name:       "OMP type",
			config:     &BackendConfig{Type: BackendTypeOMP},
			expectType: BackendTypeOMP,
		},
		{
			name:        "unknown type",
			config:      &BackendConfig{Type: "unknown-backend"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewBackend(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if backend == nil {
				t.Fatal("backend is nil")
			}
			if backend.Name() != tt.expectType {
				t.Errorf("Name() = %q, want %q", backend.Name(), tt.expectType)
			}
		})
	}
}

func TestNewBackendFromType(t *testing.T) {
	tests := []struct {
		name        string
		backendType string
		expectType  string
		expectError bool
	}{
		{
			name:        "OMP",
			backendType: BackendTypeOMP,
			expectType:  BackendTypeOMP,
		},
		{
			name:        "unknown",
			backendType: "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewBackendFromType(tt.backendType)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if backend.Name() != tt.expectType {
				t.Errorf("Name() = %q, want %q", backend.Name(), tt.expectType)
			}
		})
	}
}

func TestNewBackendRejectsLegacyConfig(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeClaudeCode,
		ClaudeCode: &ClaudeCodeConfig{
			Command:   "/custom/claude",
			ExtraArgs: []string{"--verbose"},
		},
	}

	if _, err := NewBackend(config); err == nil {
		t.Fatal("expected legacy executor config to be rejected")
	}
}

func TestNewBackendWithOMPConfig(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeOMP,
		OMP:  &OMPConfig{Command: "/custom/omp"},
	}

	backend, err := NewBackend(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if backend.Name() != BackendTypeOMP {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeOMP)
	}

	ompBackend, ok := backend.(*OMPBackend)
	if !ok {
		t.Fatal("backend is not *OMPBackend")
	}
	if ompBackend.config.Command != "/custom/omp" {
		t.Errorf("Command = %q, want /custom/omp", ompBackend.config.Command)
	}
}
