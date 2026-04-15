package main

import (
	"strings"
	"testing"
)

func TestCSSStyleIncludesProtocolColorTokens(t *testing.T) {
	for _, token := range []string{
		"--proto-at:",
		"--proto-at-deep:",
		"--proto-ssb:",
		"--proto-ssb-deep:",
		"--proto-bridge:",
		"--status-ingress:",
		"--status-egress:",
		"--status-bridge:",
		"--status-success:",
		"--status-failure:",
		"--post-accent:",
		"color-mix(in oklch",
	} {
		if !strings.Contains(cssStyle, token) {
			t.Fatalf("cssStyle missing expected token/snippet %q", token)
		}
	}
}

func TestCSSStyleRemovesLegacyLeftStripeAccents(t *testing.T) {
	for _, legacy := range []string{
		"border-left: 4px solid var(--brand);",
		".status.info { border-left: 4px solid var(--brand); }",
		".status.success { border-left: 4px solid var(--ok); }",
		".status.error { border-left: 4px solid var(--danger); }",
	} {
		if strings.Contains(cssStyle, legacy) {
			t.Fatalf("cssStyle still contains legacy accent stripe snippet %q", legacy)
		}
	}
}
