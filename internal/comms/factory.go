package comms

import (
	"time"

	"github.com/qf-studio/pilot/internal/executor"
	"github.com/qf-studio/pilot/internal/intent"
	"github.com/qf-studio/pilot/internal/memory"
)

// ClassifierConfig is the adapter-agnostic config for LLM intent classification.
// Each adapter maps its own per-adapter YAML config into this struct before
// calling BuildHandler, so the bootstrap logic lives in exactly one place.
type ClassifierConfig struct {
	Enabled     bool
	APIKey      string
	HistorySize int
	HistoryTTL  time.Duration
}

// BuildClassifier returns no classifier. Conversational classification previously
// bypassed the executor through a provider HTTP client; OMP is now the only
// model runtime and task execution uses its RPC transport instead.
func BuildClassifier(cfg *ClassifierConfig, executorBackend *executor.BackendConfig) (intent.Classifier, *intent.ConversationStore) {
	_ = cfg
	_ = executorBackend
	return nil, nil
}

// BotConfig holds the per-deployment bot configuration threaded from the root
// YAML config into the comms layer. It mirrors the fields of config.BotConfig;
// the caller (cmd/pilot/main.go) maps between the two to avoid an import cycle
// (config imports adapters, which import comms).
type BotConfig struct {
	Enabled     bool
	Model       string
	AnswerModel string
	APIKey      string
	Persona     string
	Retrieval   RetrievalConfig
}

// BuildResponder returns no responder. The former direct provider-backed bot
// responder is intentionally disabled in the OMP-only runtime.
func BuildResponder(cfg *BotConfig) *Responder {
	_ = cfg
	return nil
}

// HandlerDeps holds the per-adapter inputs needed to build a comms.Handler.
// Pass this to BuildHandler — the only place HandlerConfig is assembled.
type HandlerDeps struct {
	Messenger      Messenger
	Runner         *executor.Runner
	Projects       ProjectSource
	ProjectPath    string
	RateLimit      *RateLimitConfig
	Classifier     *ClassifierConfig
	Bot            *BotConfig
	MemberResolver MemberResolver
	Store          *memory.Store
	IssueCreator   IssueCreator
	TaskIDPrefix   string
	// ExecutorBackend is retained for adapter configuration compatibility.
	ExecutorBackend *executor.BackendConfig
}

// BuildHandler creates a Handler from adapter deps.
// This is the single assembly point for HandlerConfig; all adapter call sites
// route through here so no field can be silently omitted per-adapter.
func BuildHandler(deps HandlerDeps) *Handler {
	classifier, convStore := BuildClassifier(deps.Classifier, deps.ExecutorBackend)
	responder := BuildResponder(deps.Bot)
	return NewHandler(&HandlerConfig{
		Messenger:      deps.Messenger,
		Runner:         deps.Runner,
		Projects:       deps.Projects,
		ProjectPath:    deps.ProjectPath,
		RateLimit:      deps.RateLimit,
		LLMClassifier:  classifier,
		ConvStore:      convStore,
		Responder:      responder,
		MemberResolver: deps.MemberResolver,
		Store:          deps.Store,
		IssueCreator:   deps.IssueCreator,
		TaskIDPrefix:   deps.TaskIDPrefix,
	})
}
