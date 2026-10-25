package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Supported settings version
const CurrentSettingsVersion = 1

var (
	ErrInvalidSettingsVersion = errors.New("unsupported settings.json version")
	ErrInsecureFilePerms      = errors.New("settings.json must not be group- or world-writable")
)

// RouteConfig defines a route's parameters in settings.json
type RouteConfig struct {
	OrderedProviders []string          `json:"ordered_providers"`
	ModelPerProvider map[string]string `json:"model_per_provider"`
	AllowedProviders []string          `json:"allowed_providers"`
	FallbackPolicy   string            `json:"fallback_policy"`
	TimeoutSeconds   int               `json:"timeout_seconds,omitempty"`
	Cache            bool              `json:"cache"`
}

// ProviderConfig defines per-provider configurations in settings.json
type ProviderConfig struct {
	Model           string    `json:"model"`
	RequestsPerMin  int       `json:"requests_per_min,omitempty"`
	TokensPerMin    int       `json:"tokens_per_min,omitempty"`
	DailyTokenLimit int       `json:"daily_token_limit,omitempty"`
	LimitVerifiedOn string    `json:"limit_verified_on,omitempty"` // YYYY-MM-DD
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

// LogConfig defines logging configuration
type LogConfig struct {
	Enabled    bool `json:"enabled"`
	LogPrompts bool `json:"log_prompts"`
}

// CacheConfig defines caching configuration
type CacheConfig struct {
	Enabled bool `json:"enabled"`
	TTLHour int  `json:"ttl_hours"`
}

// SpendGuardConfig defines spend guard ceiling
type SpendGuardConfig struct {
	DailyTokenLimit int `json:"daily_token_limit"`
}

// Settings represents the structure of ~/.config/dada-router/settings.json
type Settings struct {
	Version      int                       `json:"version"`
	DefaultRoute string                    `json:"default_route"`
	Log          LogConfig                 `json:"log"`
	Cache        CacheConfig               `json:"cache"`
	SpendGuard   SpendGuardConfig          `json:"spend_guard"`
	Routes       map[string]RouteConfig    `json:"routes"`
	Providers    map[string]ProviderConfig `json:"providers"`
}

// NewDefaultSettings returns a pristine default Settings instance
func NewDefaultSettings() *Settings {
	providerDefaults := GetProviderDefaults()
	providers := make(map[string]ProviderConfig, len(providerDefaults))
	defaultModels := make(map[string]string, len(providerDefaults))

	for name, d := range providerDefaults {
		providers[name] = ProviderConfig{
			Model:           d.DefaultModel,
			RequestsPerMin:  d.RequestsPerMin,
			TokensPerMin:    d.TokensPerMin,
			DailyTokenLimit: d.DailyTokenLimit,
			LimitVerifiedOn: "2026-01-01",
		}
		defaultModels[name] = d.DefaultModel
	}

	routes := map[string]RouteConfig{
		DefaultRouteName: {
			OrderedProviders: []string{
				ProviderGemini,
				ProviderOpenAI,
				ProviderAnthropic,
				ProviderXAI,
				ProviderDeepSeek,
			},
			ModelPerProvider: defaultModels,
			AllowedProviders: AllSupportedProviders(),
			FallbackPolicy:   "immediate",
			TimeoutSeconds:   int(DefaultTotalTimeout.Seconds()),
			Cache:            false,
		},
	}

	return &Settings{
		Version:      CurrentSettingsVersion,
		DefaultRoute: DefaultRouteName,
		Log: LogConfig{
			Enabled:    false,
			LogPrompts: false,
		},
		Cache: CacheConfig{
			Enabled: false,
			TTLHour: 24,
		},
		SpendGuard: SpendGuardConfig{
			DailyTokenLimit: DefaultDailyTokenLimit,
		},
		Routes:    routes,
		Providers: providers,
	}
}

// GetConfigDir returns the directory used for dada-router configurations.
// Precedence: DADA_ROUTER_CONFIG_DIR > ~/.config/dada-router
func GetConfigDir() (string, error) {
	if custom := os.Getenv("DADA_ROUTER_CONFIG_DIR"); custom != "" {
		return custom, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine user home directory: %w", err)
	}

	return filepath.Join(home, ".config", "dada-router"), nil
}

// GetSettingsPath returns the absolute path to settings.json
func GetSettingsPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// GetLogPath returns the absolute path to the log file (mode 0600 in logs/)
func GetLogPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs", "dada-router.log"), nil
}

// LoadSettings reads and parses settings.json, verifying permissions and version.
// If the file does not exist, it initializes and returns NewDefaultSettings().
func LoadSettings() (*Settings, error) {
	path, err := GetSettingsPath()
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewDefaultSettings(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat settings file: %w", err)
	}

	// Refuse settings.json if group- or world-writable (on Unix systems)
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("%w: current permissions %o", ErrInsecureFilePerms, fi.Mode().Perm())
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings.json: %w", err)
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("malformed settings.json: %w", err)
	}

	if s.Version != CurrentSettingsVersion {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidSettingsVersion, s.Version, CurrentSettingsVersion)
	}

	if s.Routes == nil {
		s.Routes = make(map[string]RouteConfig)
	}
	if s.Providers == nil {
		s.Providers = make(map[string]ProviderConfig)
	}

	return &s, nil
}

// SaveSettings writes settings to settings.json atomically with 0600 mode.
func SaveSettings(s *Settings) error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	path := filepath.Join(dir, "settings.json")
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary settings file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomically replace settings.json: %w", err)
	}

	return nil
}
