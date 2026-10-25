package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ronishpaudel/dada-router/internal/cache"
	"github.com/ronishpaudel/dada-router/internal/config"
	"github.com/ronishpaudel/dada-router/internal/keyring"
	"github.com/ronishpaudel/dada-router/internal/logger"
	"github.com/ronishpaudel/dada-router/internal/provider"
	"github.com/ronishpaudel/dada-router/internal/quota"
	"github.com/ronishpaudel/dada-router/internal/redact"
	"github.com/ronishpaudel/dada-router/internal/route"
)

var (
	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	noticeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFA500"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))
)

// Session represents an interactive or one-shot multi-provider chat session
type Session struct {
	ActiveProvider  string
	ActiveModel     string
	ActiveRoute     route.Route
	History         []provider.ChatMessage
	Registry        *provider.Registry
	KeyStore        keyring.Store
	QuotaTracker    *quota.Tracker
	ResponseCache   *cache.ResponseCache
	Logger          *logger.Logger
	Settings        *config.Settings
	OutputWriter    io.Writer
	SessionSkipped  map[string]bool // Providers skipped for the rest of session (e.g. 401 auth error)
}

// NewSession initializes a new chat session with default route and providers
func NewSession(
	settings *config.Settings,
	registry *provider.Registry,
	keyStore keyring.Store,
	quotaTracker *quota.Tracker,
	responseCache *cache.ResponseCache,
	log *logger.Logger,
	routeName string,
) (*Session, error) {
	if routeName == "" {
		routeName = settings.DefaultRoute
		if routeName == "" {
			routeName = config.DefaultRouteName
		}
	}

	rc, ok := settings.Routes[routeName]
	if !ok {
		return nil, fmt.Errorf("route %q not found in settings", routeName)
	}

	activeRoute := route.FromConfig(routeName, rc)

	// Pick first available provider from route
	var firstProvider, firstModel string
	for _, p := range activeRoute.OrderedProviders {
		if activeRoute.IsProviderAllowed(p) {
			firstProvider = p
			firstModel = activeRoute.ResolveModel(p, "")
			break
		}
	}

	if firstProvider == "" {
		firstProvider = config.ProviderGemini
		firstModel = "gemini-1.5-flash"
	}

	return &Session{
		ActiveProvider: firstProvider,
		ActiveModel:    firstModel,
		ActiveRoute:    activeRoute,
		History:        make([]provider.ChatMessage, 0),
		Registry:       registry,
		KeyStore:       keyStore,
		QuotaTracker:   quotaTracker,
		ResponseCache:  responseCache,
		Logger:         log,
		Settings:       settings,
		OutputWriter:   os.Stdout,
		SessionSkipped: make(map[string]bool),
	}, nil
}

// Prompt returns the formatted prompt string, e.g. "[gemini · flash] > "
func (s *Session) Prompt() string {
	modelDisplay := s.ActiveModel
	// Shorten model name if too long for prompt readability
	if strings.HasPrefix(modelDisplay, "gemini-1.5-") {
		modelDisplay = strings.TrimPrefix(modelDisplay, "gemini-1.5-")
	} else if strings.HasPrefix(modelDisplay, "claude-3-5-") {
		modelDisplay = strings.TrimPrefix(modelDisplay, "claude-3-5-")
		if idx := strings.Index(modelDisplay, "-20"); idx != -1 {
			modelDisplay = modelDisplay[:idx]
		}
	}

	label := fmt.Sprintf("[%s · %s] > ", s.ActiveProvider, modelDisplay)
	return promptStyle.Render(label)
}

// SwitchProvider changes the active provider and re-serializes conversation context
func (s *Session) SwitchProvider(newProvider string) error {
	newProvider = strings.ToLower(strings.TrimSpace(newProvider))
	p, err := s.Registry.Get(newProvider)
	if err != nil {
		return err
	}

	model := s.ActiveRoute.ResolveModel(newProvider, p.DefaultModel())
	if model == "" {
		model = p.DefaultModel()
	}

	// Prepare handoff and budget check (e.g. 100,000 tokens safe default ceiling)
	updatedHistory, notices := PrepareHandoff(s.History, newProvider, 100000)
	s.History = updatedHistory

	s.ActiveProvider = newProvider
	s.ActiveModel = model

	for _, n := range notices {
		fmt.Fprintln(s.OutputWriter, noticeStyle.Render(n))
	}

	fmt.Fprintln(s.OutputWriter, successStyle.Render(fmt.Sprintf("Switched to %s (%s). Context preserved (%d turns).", newProvider, model, len(s.History))))
	return nil
}

// SwitchModel changes the active model for a provider
func (s *Session) SwitchModel(provName string, modelName string) error {
	provName = strings.ToLower(strings.TrimSpace(provName))
	modelName = strings.TrimSpace(modelName)

	if provName == "" {
		provName = s.ActiveProvider
	}

	if _, err := s.Registry.Get(provName); err != nil {
		return err
	}

	if provName == s.ActiveProvider {
		s.ActiveModel = modelName
	}

	if s.ActiveRoute.ModelPerProvider == nil {
		s.ActiveRoute.ModelPerProvider = make(map[string]string)
	}
	s.ActiveRoute.ModelPerProvider[provName] = modelName

	fmt.Fprintln(s.OutputWriter, successStyle.Render(fmt.Sprintf("Model for %s set to %s.", provName, modelName)))
	return nil
}

// SwitchRoute changes the active route
func (s *Session) SwitchRoute(routeName string) error {
	routeName = strings.TrimSpace(routeName)
	rc, ok := s.Settings.Routes[routeName]
	if !ok {
		return fmt.Errorf("route %q not found in settings", routeName)
	}

	s.ActiveRoute = route.FromConfig(routeName, rc)

	// Update active provider if not allowed on new route
	if !s.ActiveRoute.IsProviderAllowed(s.ActiveProvider) {
		for _, p := range s.ActiveRoute.OrderedProviders {
			if s.ActiveRoute.IsProviderAllowed(p) {
				return s.SwitchProvider(p)
			}
		}
	}

	fmt.Fprintln(s.OutputWriter, successStyle.Render(fmt.Sprintf("Active route set to %q.", routeName)))
	return nil
}

// HandleSlashCommand processes in-chat slash commands
func (s *Session) HandleSlashCommand(cmdLine string) (bool, error) {
	parts := strings.Fields(strings.TrimPrefix(cmdLine, "/"))
	if len(parts) == 0 {
		return true, nil
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "help":
		fmt.Fprintln(s.OutputWriter, "\nAvailable commands:")
		fmt.Fprintln(s.OutputWriter, "  /switch <provider>        - Switch active provider (context carried over)")
		fmt.Fprintln(s.OutputWriter, "  /model <provider> <name>  - Change model for a provider")
		fmt.Fprintln(s.OutputWriter, "  /route <name>             - Change active route")
		fmt.Fprintln(s.OutputWriter, "  /usage                    - Display quota and usage bars")
		fmt.Fprintln(s.OutputWriter, "  /clear                    - Clear in-memory conversation")
		fmt.Fprintln(s.OutputWriter, "  /help                     - Show this help message")
		fmt.Fprintln(s.OutputWriter, "  /quit                     - Exit session")
		return true, nil

	case "switch":
		if len(parts) < 2 {
			fmt.Fprintln(s.OutputWriter, noticeStyle.Render("Usage: /switch <openai|anthropic|gemini|xai|deepseek>"))
			return true, nil
		}
		if err := s.SwitchProvider(parts[1]); err != nil {
			fmt.Fprintln(s.OutputWriter, errorStyle.Render("Switch error: "+err.Error()))
		}
		return true, nil

	case "model":
		if len(parts) < 3 {
			fmt.Fprintln(s.OutputWriter, noticeStyle.Render("Usage: /model <provider> <model-name>"))
			return true, nil
		}
		if err := s.SwitchModel(parts[1], parts[2]); err != nil {
			fmt.Fprintln(s.OutputWriter, errorStyle.Render("Model switch error: "+err.Error()))
		}
		return true, nil

	case "route":
		if len(parts) < 2 {
			fmt.Fprintln(s.OutputWriter, noticeStyle.Render("Usage: /route <route-name>"))
			return true, nil
		}
		if err := s.SwitchRoute(parts[1]); err != nil {
			fmt.Fprintln(s.OutputWriter, errorStyle.Render("Route switch error: "+err.Error()))
		}
		return true, nil

	case "usage":
		fmt.Fprintln(s.OutputWriter, quota.RenderUsageBars(s.QuotaTracker))
		return true, nil

	case "clear":
		s.History = make([]provider.ChatMessage, 0)
		fmt.Fprintln(s.OutputWriter, successStyle.Render("Conversation cleared."))
		return true, nil

	case "quit", "exit":
		return false, nil

	default:
		fmt.Fprintln(s.OutputWriter, errorStyle.Render(fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", cmd)))
		return true, nil
	}
}

// Send processes a user message, executing routing, fallback, streaming, and caching
func (s *Session) Send(ctx context.Context, userText string) error {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return nil
	}

	// 1. Check daily spend guard before making network call
	estTokens := EstimateTokens(userText)
	if err := s.QuotaTracker.CheckSpendGuard(estTokens); err != nil {
		fmt.Fprintln(s.OutputWriter, errorStyle.Render("Spend Guard Block: "+err.Error()))
		return err
	}

	// Add user turn to conversation
	userMsg := provider.ChatMessage{Role: "user", Content: userText}
	reqMessages := append(s.History, userMsg)

	// 2. Response Cache lookup (if active route has cache enabled)
	if s.ActiveRoute.Cache && s.ResponseCache != nil {
		cacheKey := cache.GenerateKey(s.ActiveProvider, s.ActiveModel, s.ActiveRoute.Name, reqMessages)
		if item, hit := s.ResponseCache.Get(cacheKey); hit {
			fmt.Fprintln(s.OutputWriter, item.Content)
			s.History = append(s.History, userMsg, provider.ChatMessage{Role: "assistant", Content: item.Content})
			return nil
		}
	}

	// 3. Fallback candidates loop
	unavailable := make(map[string]bool)
	for k, v := range s.SessionSkipped {
		if v {
			unavailable[k] = true
		}
	}

	// Collect candidate providers
	candidates := s.ActiveRoute.SelectCandidates(userText)

	// Place active provider first if allowed and not skipped
	var prioritizedCandidates []string
	if s.ActiveRoute.IsProviderAllowed(s.ActiveProvider) && !unavailable[s.ActiveProvider] {
		prioritizedCandidates = append(prioritizedCandidates, s.ActiveProvider)
	}
	for _, c := range candidates {
		if c != s.ActiveProvider && !unavailable[c] {
			prioritizedCandidates = append(prioritizedCandidates, c)
		}
	}

	if len(prioritizedCandidates) == 0 {
		return route.ErrNoAvailableProvider
	}

	var lastErr error

	for _, candProvider := range prioritizedCandidates {
		// Check rate limits from quota tracker
		if !s.QuotaTracker.CanCallProvider(candProvider) {
			continue
		}

		candModel := s.ActiveRoute.ResolveModel(candProvider, "")
		provInstance, err := s.Registry.Get(candProvider)
		if err != nil {
			continue
		}
		if candModel == "" {
			candModel = provInstance.DefaultModel()
		}

		apiKey, err := s.KeyStore.Get(candProvider)
		if err != nil {
			// Key not configured for this provider, continue to next fallback
			continue
		}

		// Prepare handoff token budget
		adaptedMessages, notices := PrepareHandoff(reqMessages, candProvider, 100000)
		for _, n := range notices {
			fmt.Fprintln(s.OutputWriter, noticeStyle.Render(n))
		}

		streamReq := provider.StreamRequest{
			Model:    candModel,
			Messages: adaptedMessages,
		}

		// Attempt streaming with single 5xx retry logic
		success, outputText, streamErr := s.executeStreamWithFallback(ctx, provInstance, apiKey, streamReq)

		if success {
			// Stream succeeded!
			s.ActiveProvider = candProvider
			s.ActiveModel = candModel
			s.History = append(s.History, userMsg, provider.ChatMessage{Role: "assistant", Content: outputText})

			// Record token usage
			usedTokens := EstimateTokens(userText) + EstimateTokens(outputText)
			s.QuotaTracker.RecordUsage(candProvider, usedTokens)

			// Store in response cache if enabled
			if s.ActiveRoute.Cache && s.ResponseCache != nil {
				cacheKey := cache.GenerateKey(candProvider, candModel, s.ActiveRoute.Name, reqMessages)
				_ = s.ResponseCache.Set(cacheKey, outputText, &provider.UsageMetrics{TotalTokens: usedTokens})
			}

			// Log execution event
			s.Logger.LogEvent(candProvider, candModel, s.ActiveRoute.Name, "ok", usedTokens, userText, outputText, nil)
			return nil
		}

		// Provider call failed before first token: handle according to error mapping
		lastErr = streamErr
		s.Logger.LogEvent(candProvider, candModel, s.ActiveRoute.Name, "error", 0, userText, "", streamErr)

		var provErr *provider.ProviderError
		if errors.As(streamErr, &provErr) {
			switch {
			case errors.Is(provErr.RawError, provider.ErrAuth):
				// 401/403: No retry. Skip provider for rest of session.
				s.SessionSkipped[candProvider] = true
				fmt.Fprintln(s.OutputWriter, noticeStyle.Render(fmt.Sprintf("[%s] Authentication failure. Skipping provider for this session.", candProvider)))

			case errors.Is(provErr.RawError, provider.ErrModelNotFound):
				// 404: Fall back, show notice naming model and suggesting dada doctor
				fmt.Fprintln(s.OutputWriter, noticeStyle.Render(fmt.Sprintf("[%s] Model %q not found. Falling back. Run 'dada doctor' to verify.", candProvider, candModel)))

			case errors.Is(provErr.RawError, provider.ErrRateLimited):
				// 429: Record reset time, fall back
				s.QuotaTracker.RecordRateLimit(candProvider, provErr.RetryAfter)
				fmt.Fprintln(s.OutputWriter, noticeStyle.Render(fmt.Sprintf("[%s] Rate limited. Falling back.", candProvider)))

			case errors.Is(provErr.RawError, provider.ErrContextTooLarge):
				// Re-run handoff once at 70% of budget
				fmt.Fprintln(s.OutputWriter, noticeStyle.Render(fmt.Sprintf("[%s] Context length error. Retrying with 70%% budget...", candProvider)))
				reducedMessages, _ := PrepareHandoff(reqMessages, candProvider, 70000)
				streamReq.Messages = reducedMessages
				retrySuccess, retryOutput, retryErr := s.executeStreamWithFallback(ctx, provInstance, apiKey, streamReq)
				if retrySuccess {
					s.ActiveProvider = candProvider
					s.ActiveModel = candModel
					s.History = append(s.History, userMsg, provider.ChatMessage{Role: "assistant", Content: retryOutput})
					return nil
				}
				lastErr = retryErr

			default:
				// Other 4xx errors: non-retryable
				if provErr.StatusCode >= 400 && provErr.StatusCode < 500 && provErr.StatusCode != 404 && provErr.StatusCode != 429 {
					return fmt.Errorf("client error %d from %s: %w", provErr.StatusCode, candProvider, provErr)
				}
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("%w: %v", route.ErrNoAvailableProvider, lastErr)
	}

	return route.ErrNoAvailableProvider
}

// executeStreamWithFallback streams output to stdout.
// If output started, NO fallback is permitted on error; partial output is kept with error marker.
func (s *Session) executeStreamWithFallback(
	ctx context.Context,
	prov provider.Provider,
	apiKey string,
	req provider.StreamRequest,
) (bool, string, error) {
	// 5xx single retry logic with 250-750ms jitter
	var ch <-chan provider.StreamChunk
	var err error

	for attempt := 0; attempt < 2; attempt++ {
		ch, err = prov.Stream(ctx, apiKey, req)
		if err == nil {
			break
		}

		var provErr *provider.ProviderError
		if errors.As(err, &provErr) && errors.Is(provErr.RawError, provider.ErrServer) && attempt == 0 {
			// One retry with 250-750ms jitter
			jitter := time.Duration(250+rand.Intn(500)) * time.Millisecond
			select {
			case <-time.After(jitter):
				continue
			case <-ctx.Done():
				return false, "", ctx.Err()
			}
		}
		break
	}

	if err != nil {
		return false, "", err
	}

	var output strings.Builder
	firstTokenEmitted := false

	for chunk := range ch {
		if chunk.Err != nil {
			if firstTokenEmitted {
				// Once output begins, keep partial and show error marker
				marker := "\n" + errorStyle.Render("[STREAM INTERRUPTED: "+redact.Redact(chunk.Err.Error())+"]")
				fmt.Fprintln(s.OutputWriter, marker)
				return true, output.String(), nil
			}
			return false, "", chunk.Err
		}

		if chunk.Content != "" {
			firstTokenEmitted = true
			output.WriteString(chunk.Content)
			// Strip ANSI escapes from model output
			fmt.Fprint(s.OutputWriter, stripModelANSI(chunk.Content))
		}
	}

	if firstTokenEmitted {
		fmt.Fprintln(s.OutputWriter)
		return true, output.String(), nil
	}

	return false, "", errors.New("empty response stream")
}

// stripModelANSI ensures no ANSI/OSC escapes are emitted by models
func stripModelANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1B {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
