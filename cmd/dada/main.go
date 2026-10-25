package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ronishpaudel/dada-router/internal/cache"
	"github.com/ronishpaudel/dada-router/internal/chat"
	"github.com/ronishpaudel/dada-router/internal/config"
	"github.com/ronishpaudel/dada-router/internal/keyring"
	"github.com/ronishpaudel/dada-router/internal/logger"
	"github.com/ronishpaudel/dada-router/internal/provider"
	"github.com/ronishpaudel/dada-router/internal/quota"
	"github.com/ronishpaudel/dada-router/internal/redact"
	"github.com/ronishpaudel/dada-router/internal/route"
)

var (
	routeFlag            string
	insecureFileStore    bool
	resetYesFlag         bool

	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500"))
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
	succStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	boldStyle = lipgloss.NewStyle().Bold(true)
)

func getKeyStore(insecure bool) (keyring.Store, error) {
	if insecure {
		fmt.Fprintln(os.Stderr, warnStyle.Render("WARNING: --insecure-file-store active. Using encrypted credentials file instead of OS keychain."))
		fmt.Fprint(os.Stderr, "Enter keystore passphrase: ")
		passphrase, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("failed to read passphrase: %w", err)
		}
		path, err := keyring.GetDefaultEncryptedStorePath()
		if err != nil {
			return nil, err
		}
		return keyring.NewFileStoreAdapter(path, passphrase), nil
	}
	return keyring.NewSystemKeyringStore(), nil
}

func main() {
	var appLogger *logger.Logger

	// Top-level panic recovery
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n%s\n", errStyle.Render("An unexpected error occurred. Exiting safely."))
			if appLogger != nil {
				appLogger.LogPanic(r, debug.Stack())
			}
			os.Exit(1)
		}
	}()

	rootCmd := &cobra.Command{
		Use:   "dada",
		Short: "Unified multi-provider AI terminal session",
		Long:  "dada-router gives you one continuous terminal session for OpenAI, Anthropic, Gemini, xAI, and DeepSeek using your own API keys.",
		Run: func(cmd *cobra.Command, args []string) {
			runChat(routeFlag, insecureFileStore)
		},
	}

	rootCmd.PersistentFlags().StringVar(&routeFlag, "route", "", "Route to use for chat session")
	rootCmd.PersistentFlags().BoolVar(&insecureFileStore, "insecure-file-store", false, "Use encrypted-file credential fallback (headless Linux)")

	// Subcommands
	chatCmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive chat session (default)",
		Run: func(cmd *cobra.Command, args []string) {
			runChat(routeFlag, insecureFileStore)
		},
	}

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Add or update provider API keys with 1-token validation probe",
		Run: func(cmd *cobra.Command, args []string) {
			runSetup(insecureFileStore)
		},
	}

	usageCmd := &cobra.Command{
		Use:   "usage",
		Short: "Show per-provider quota bars and reset windows",
		Run: func(cmd *cobra.Command, args []string) {
			runUsage()
		},
	}

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate API keys, check configured models, and inspect API versions",
		Run: func(cmd *cobra.Command, args []string) {
			runDoctor(insecureFileStore)
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Display redacted application execution logs",
		Run: func(cmd *cobra.Command, args []string) {
			runLogs()
		},
	}

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete keys, cache, logs, and settings",
		Run: func(cmd *cobra.Command, args []string) {
			runReset(resetYesFlag, insecureFileStore)
		},
	}
	resetCmd.Flags().BoolVarP(&resetYesFlag, "yes", "y", false, "Confirm reset without interactive confirmation")

	// Route subcommand group
	routeGroupCmd := &cobra.Command{
		Use:   "route",
		Short: "Manage and inspect multi-provider routes",
	}

	routeListCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured routes",
		Run: func(cmd *cobra.Command, args []string) {
			runRouteList()
		},
	}

	routeShowCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details for a route",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runRouteShow(args[0])
		},
	}

	routeSetCmd := &cobra.Command{
		Use:   "set <name> <providers...>",
		Short: "Set ordered providers for a route",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runRouteSet(args[0], args[1:])
		},
	}

	routeGroupCmd.AddCommand(routeListCmd, routeShowCmd, routeSetCmd)

	// Keys subcommand group
	keysGroupCmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage stored API keys",
	}

	keysRemoveCmd := &cobra.Command{
		Use:   "remove <provider>",
		Short: "Remove a stored provider API key",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runKeysRemove(args[0], insecureFileStore)
		},
	}

	keysGroupCmd.AddCommand(keysRemoveCmd)

	rootCmd.AddCommand(chatCmd, setupCmd, usageCmd, doctorCmd, logsCmd, resetCmd, routeGroupCmd, keysGroupCmd)

	// Initialize logger for crash handler
	settings, _ := config.LoadSettings()
	if settings != nil {
		appLogger, _ = logger.NewLogger(settings)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(2) // Usage error
	}
}

func runChat(routeName string, insecure bool) {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", errStyle.Render("Configuration error: "+err.Error()))
		os.Exit(1)
	}

	store, err := getKeyStore(insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", errStyle.Render(err.Error()))
		os.Exit(1)
	}

	reg := provider.NewRegistry(settings)
	quotaTracker := quota.NewTracker(settings)
	respCache, _ := cache.NewResponseCache(0)
	appLogger, _ := logger.NewLogger(settings)

	session, err := chat.NewSession(settings, reg, store, quotaTracker, respCache, appLogger, routeName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", errStyle.Render("Session init error: "+err.Error()))
		os.Exit(1)
	}

	isTTY := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))

	// Non-TTY mode: one-shot execution from stdin
	if !isTTY {
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(1)
		}
		text := strings.TrimSpace(string(input))
		if text == "" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), session.ActiveRoute.Timeout)
		defer cancel()

		if err := session.Send(ctx, text); err != nil {
			if errors.Is(err, route.ErrNoAvailableProvider) {
				os.Exit(3) // All providers unavailable
			}
			os.Exit(1)
		}
		return
	}

	// Interactive TTY REPL
	fmt.Printf("%s\n", boldStyle.Render("dada-router (type /help for commands, /quit to exit)"))
	scanner := bufio.NewScanner(os.Stdin)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	for {
		fmt.Print(session.Prompt())

		lineChan := make(chan string, 1)
		errChan := make(chan error, 1)

		go func() {
			if scanner.Scan() {
				lineChan <- scanner.Text()
			} else {
				errChan <- scanner.Err()
			}
		}()

		select {
		case <-sigChan:
			// Ctrl-C at prompt exits cleanly with 0
			fmt.Println()
			os.Exit(0)

		case err := <-errChan:
			if err != nil {
				fmt.Fprintln(os.Stderr, errStyle.Render("Read error: "+err.Error()))
			}
			return

		case line := <-lineChan:
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "/") {
				continueChat, slashErr := session.HandleSlashCommand(line)
				if slashErr != nil {
					fmt.Fprintln(os.Stderr, errStyle.Render(slashErr.Error()))
				}
				if !continueChat {
					os.Exit(0)
				}
				continue
			}

			// Context with timeout and signal handling for streaming interruption
			ctx, cancel := context.WithTimeout(context.Background(), session.ActiveRoute.Timeout)

			go func() {
				select {
				case <-sigChan:
					// Ctrl-C during streaming: cancel request context
					cancel()
				case <-ctx.Done():
				}
			}()

			sendErr := session.Send(ctx, line)
			cancel()

			if sendErr != nil {
				if errors.Is(sendErr, route.ErrNoAvailableProvider) {
					fmt.Fprintln(os.Stderr, errStyle.Render("All providers in route are currently unavailable."))
				} else {
					fmt.Fprintln(os.Stderr, errStyle.Render(redact.Redact(sendErr.Error())))
				}
			}
		}
	}
}

func runSetup(insecure bool) {
	fmt.Println(boldStyle.Render("=== dada-router Provider Key Setup ==="))
	fmt.Println("Keys are validated with a 1-token probe before being saved.")
	fmt.Println("Press Ctrl-C at any time to abort without saving.")
	fmt.Println()

	// Catch Ctrl-C during setup to abort immediately
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nSetup aborted. No changes saved.")
		os.Exit(0)
	}()

	store, err := getKeyStore(insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", errStyle.Render(err.Error()))
		os.Exit(1)
	}

	settings, _ := config.LoadSettings()
	reg := provider.NewRegistry(settings)

	providers := config.AllSupportedProviders()

	for _, p := range providers {
		existingKey, _ := store.Get(p)
		status := "(not configured)"
		if existingKey != "" {
			status = "(configured)"
		}

		fmt.Printf("Configure %s %s? [y/N]: ", boldStyle.Render(strings.ToUpper(p)), status)
		var answer string
		_, _ = fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			continue
		}

		fmt.Printf("Enter API key for %s (input hidden): ", p)
		keyBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, errStyle.Render("Failed to read input: "+err.Error()))
			continue
		}

		key := strings.TrimSpace(string(keyBytes))
		keyring.ZeroBytes(keyBytes)

		if key == "" {
			fmt.Println("Skipped.")
			continue
		}

		fmt.Printf("Validating key with %s (1-token request)... ", p)
		provInstance, err := reg.Get(p)
		if err != nil {
			fmt.Println(errStyle.Render("ERROR: " + err.Error()))
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		valErr := provInstance.ValidateKey(ctx, key)
		cancel()

		if valErr != nil {
			fmt.Println(errStyle.Render("FAILED!"))
			fmt.Println(errStyle.Render("Validation error: " + redact.Redact(valErr.Error())))
			fmt.Println("Key was not saved.")
		} else {
			fmt.Println(succStyle.Render("VALID!"))
			if err := store.Set(p, key); err != nil {
				fmt.Println(errStyle.Render("Failed to store key: " + err.Error()))
			} else {
				fmt.Println(succStyle.Render("Key stored securely."))
			}
		}
		fmt.Println()
	}

	fmt.Println(succStyle.Render("Setup completed."))
}

func runUsage() {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}

	tracker := quota.NewTracker(settings)
	fmt.Print(quota.RenderUsageBars(tracker))
}

func runDoctor(insecure bool) {
	fmt.Println(boldStyle.Render("=== dada-router Doctor ==="))
	fmt.Println()

	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Config error: "+err.Error()))
		os.Exit(1)
	}

	store, err := getKeyStore(insecure)
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Store error: "+err.Error()))
		os.Exit(1)
	}

	reg := provider.NewRegistry(settings)

	fmt.Println("1. Checking API Versions & Verification Dates:")
	for _, p := range config.AllSupportedProviders() {
		info, ok := provider.PinnedAPIVersions[p]
		if !ok {
			continue
		}
		older60 := provider.IsOlderThanDays(info.VerifiedOn, 60)
		dateStr := info.VerifiedOn.Format("2006-01-02")
		if older60 {
			fmt.Printf("  • %-10s %-12s (verified: %s) %s\n", p, info.Version, dateStr, warnStyle.Render("[WARN: verification older than 60 days]"))
		} else {
			fmt.Printf("  • %-10s %-12s (verified: %s) %s\n", p, info.Version, dateStr, succStyle.Render("[OK]"))
		}
	}
	fmt.Println()

	fmt.Println("2. Validating Stored Provider Keys:")
	for _, p := range config.AllSupportedProviders() {
		key, err := store.Get(p)
		if err != nil {
			fmt.Printf("  • %-10s %s\n", p, warnStyle.Render("Not configured"))
			continue
		}

		provInstance, err := reg.Get(p)
		if err != nil {
			fmt.Printf("  • %-10s %s\n", p, errStyle.Render("Unknown provider"))
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		valErr := provInstance.ValidateKey(ctx, key)
		cancel()

		if valErr != nil {
			fmt.Printf("  • %-10s %s (%s)\n", p, errStyle.Render("INVALID"), redact.Redact(valErr.Error()))
		} else {
			fmt.Printf("  • %-10s %s\n", p, succStyle.Render("VALID"))
		}
	}
	fmt.Println()
	fmt.Println(succStyle.Render("Doctor verification complete."))
}

func runLogs() {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Config error: "+err.Error()))
		os.Exit(1)
	}

	appLogger, err := logger.NewLogger(settings)
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Logger error: "+err.Error()))
		os.Exit(1)
	}

	content, err := appLogger.ReadLogs()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Log read error: "+err.Error()))
		os.Exit(1)
	}

	fmt.Println(content)
}

func runReset(confirmed bool, insecure bool) {
	if !confirmed {
		fmt.Print(warnStyle.Render("Are you sure you want to delete all keys, cache, logs, and settings? [y/N]: "))
		var answer string
		_, _ = fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("Reset cancelled.")
			return
		}
	}

	store, _ := getKeyStore(insecure)
	if store != nil {
		_ = store.Reset()
	}

	respCache, _ := cache.NewResponseCache(0)
	if respCache != nil {
		_ = respCache.Reset()
	}

	settings, _ := config.LoadSettings()
	appLogger, _ := logger.NewLogger(settings)
	if appLogger != nil {
		_ = appLogger.Reset()
	}

	settingsPath, err := config.GetSettingsPath()
	if err == nil {
		_ = os.Remove(settingsPath)
	}

	fmt.Println(succStyle.Render("Reset complete. All keys, cache, logs, and settings deleted."))
}

func runRouteList() {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render(err.Error()))
		os.Exit(1)
	}

	fmt.Println(boldStyle.Render("Configured Routes:"))
	for name, r := range settings.Routes {
		fmt.Printf("  • %-15s (providers: %s)\n", name, strings.Join(r.OrderedProviders, " -> "))
	}
}

func runRouteShow(name string) {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render(err.Error()))
		os.Exit(1)
	}

	r, ok := settings.Routes[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "%s\n", errStyle.Render(fmt.Sprintf("Route %q not found", name)))
		os.Exit(2)
	}

	fmt.Printf("%s: %s\n", boldStyle.Render("Route"), name)
	fmt.Printf("  Ordered Providers: %s\n", strings.Join(r.OrderedProviders, " -> "))
	fmt.Printf("  Allowed Providers: %s\n", strings.Join(r.AllowedProviders, ", "))
	fmt.Printf("  Fallback Policy:   %s\n", r.FallbackPolicy)
	fmt.Printf("  Cache:             %v\n", r.Cache)
}

func runRouteSet(name string, providers []string) {
	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render(err.Error()))
		os.Exit(1)
	}

	r, ok := settings.Routes[name]
	if !ok {
		r = config.RouteConfig{
			AllowedProviders: providers,
			FallbackPolicy:   "immediate",
		}
	}

	r.OrderedProviders = providers
	settings.Routes[name] = r

	if err := config.SaveSettings(settings); err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Failed to save settings: "+err.Error()))
		os.Exit(1)
	}

	fmt.Println(succStyle.Render(fmt.Sprintf("Route %q updated with providers: %s", name, strings.Join(providers, " -> "))))
}

func runKeysRemove(providerName string, insecure bool) {
	store, err := getKeyStore(insecure)
	if err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render(err.Error()))
		os.Exit(1)
	}

	if err := store.Delete(providerName); err != nil {
		fmt.Fprintln(os.Stderr, errStyle.Render("Failed to remove key: "+err.Error()))
		os.Exit(1)
	}

	fmt.Println(succStyle.Render(fmt.Sprintf("Removed key for %s.", providerName)))
}
