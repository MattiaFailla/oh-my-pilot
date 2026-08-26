package executor

import "testing"

// TestResolveSelectedModel_GH2450 covers the regression where setting
// `default_model` clobbered `model_routing` for the OMP backend,
// causing the routed model to be wiped to "" before reaching the backend.
//
// Cases:
//   - (a) model_routing.simple set + OMP backend → router model wins
//   - (b) model_routing unset + OMP backend → default_model
func TestResolveSelectedModel_GH2450(t *testing.T) {
	// Force ComplexitySimple via a short description (word count < 10).
	task := &Task{
		ID:          "GH-2450-test",
		Title:       "tiny",
		Description: "one liner",
	}

	tests := []struct {
		name    string
		routing *ModelRoutingConfig
		config  *BackendConfig
		want    string
	}{
		{
			name: "model_routing wins for OMP backend",
			routing: &ModelRoutingConfig{
				Enabled: true,
				Simple:  "claude-sonnet-4-6",
				Medium:  "claude-sonnet-4-6",
				Complex: "claude-opus-4-7",
				Trivial: "claude-haiku-4-5",
			},
			config: &BackendConfig{
				Type:         BackendTypeOMP,
				DefaultModel: "claude-opus-4-7",
			},
			want: "claude-sonnet-4-6",
		},
		{
			// GH-2807: use explicit Enabled:false to represent "user disabled routing".
			name:    "model_routing unset + OMP backend → default_model",
			routing: &ModelRoutingConfig{Enabled: false},
			config: &BackendConfig{
				Type:         BackendTypeOMP,
				DefaultModel: "claude-opus-4-7",
			},
			want: "claude-opus-4-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Runner{
				config:      tt.config,
				modelRouter: NewModelRouter(tt.routing, nil),
			}

			got := r.resolveSelectedModel(task)
			if got != tt.want {
				t.Errorf("resolveSelectedModel() = %q, want %q", got, tt.want)
			}
		})
	}
}
