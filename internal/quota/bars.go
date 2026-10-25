package quota

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	providerStyle = lipgloss.NewStyle().
			Bold(true).
			Width(12).
			Foreground(lipgloss.Color("#04B575"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87"))

	barFilled = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3C7EFB"))

	barEmpty = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#333333"))
)

// formatProgressBar builds an ASCII/Lipgloss bar clamped to 0-100%
func formatProgressBar(used int, limit int, width int) string {
	if limit <= 0 {
		return "[unlimited]"
	}

	ratio := float64(used) / float64(limit)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1.0 {
		ratio = 1.0
	}

	filledLen := int(math.Round(ratio * float64(width)))
	if filledLen > width {
		filledLen = width
	}
	emptyLen := width - filledLen

	pct := int(math.Round(ratio * 100))
	filledStr := strings.Repeat("█", filledLen)
	emptyStr := strings.Repeat("░", emptyLen)

	return fmt.Sprintf("%s%s %3d%%", barFilled.Render(filledStr), barEmpty.Render(emptyStr), pct)
}

// RenderUsageBars outputs the formatted usage bars
func RenderUsageBars(tracker *Tracker) string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render("═══ Provider Quotas & Usage (estimated) ═══"))
	sb.WriteString("\n\n")

	usedDaily, limitDaily := tracker.GetDailySpendStatus()
	spendPct := 0
	if limitDaily > 0 {
		spendPct = int(float64(usedDaily) / float64(limitDaily) * 100)
		if spendPct > 100 {
			spendPct = 100
		}
	}

	dailySpendHeader := fmt.Sprintf("Daily Spend Guard: %d / %d tokens (%d%%)", usedDaily, limitDaily, spendPct)
	if usedDaily >= limitDaily && limitDaily > 0 {
		sb.WriteString(warnStyle.Render(dailySpendHeader + " [CEILING REACHED]"))
	} else {
		sb.WriteString(dimStyle.Render(dailySpendHeader))
	}
	sb.WriteString("\n\n")

	reports := tracker.Report()
	for _, r := range reports {
		// Provider line
		line := providerStyle.Render(strings.ToUpper(r.Provider))

		// Token bar
		tokenBar := formatProgressBar(r.TokensUsed, r.TokensLimit, 20)

		// Reset time in user's local zone with abbreviation
		localReset := r.ResetTime.Local()
		timeZone, _ := localReset.Zone()
		resetFormatted := fmt.Sprintf("reset: %s %s", localReset.Format("15:04:05"), timeZone)

		status := dimStyle.Render(resetFormatted)
		if r.IsRateLimited {
			status = warnStyle.Render("RATE LIMITED - " + resetFormatted)
		}

		verifiedText := dimStyle.Render(fmt.Sprintf("(verified: %s)", r.LimitVerifiedOn))

		sb.WriteString(fmt.Sprintf("%s %s %s %s\n", line, tokenBar, status, verifiedText))
	}

	out := sb.String()
	if os.Getenv("NO_COLOR") != "" {
		return stripEscapes(out)
	}
	return out
}

// stripEscapes removes ANSI escape sequences for NO_COLOR environments
func stripEscapes(s string) string {
	// Simple escape stripper
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == 0x1B {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
