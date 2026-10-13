package lsp

import (
	"context"
	"time"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// GetFileDiagnostics returns diagnostics for a specific file.
func (c *Client) GetFileDiagnostics(uri protocol.DocumentURI) []protocol.Diagnostic {
	diags, _ := c.diagnostics.Get(uri)
	return diags
}

// GetDiagnostics returns all diagnostics for all files.
func (c *Client) GetDiagnostics() map[protocol.DocumentURI][]protocol.Diagnostic {
	if c == nil {
		return nil
	}
	return c.diagnostics.Copy()
}

// GetDiagnosticCounts returns cached diagnostic counts by severity.
// Uses the VersionedMap version to avoid recomputing on every call.
func (c *Client) GetDiagnosticCounts() DiagnosticCounts {
	if c == nil {
		return DiagnosticCounts{}
	}
	currentVersion := c.diagnostics.Version()

	c.diagCountsMu.Lock()
	defer c.diagCountsMu.Unlock()

	if currentVersion == c.diagCountsVersion {
		return c.diagCountsCache
	}

	// Recompute counts.
	counts := DiagnosticCounts{}
	for _, diags := range c.diagnostics.Seq2() {
		for _, diag := range diags {
			switch diag.Severity {
			case protocol.SeverityError:
				counts.Error++
			case protocol.SeverityWarning:
				counts.Warning++
			case protocol.SeverityInformation:
				counts.Information++
			case protocol.SeverityHint:
				counts.Hint++
			}
		}
	}

	c.diagCountsCache = counts
	c.diagCountsVersion = currentVersion
	return counts
}

// WaitForDiagnostics waits until diagnostics stop changing for a settling
// period, indicating the LSP server has finished processing. If no
// diagnostics change within firstChangeDuration, it returns early since the
// server likely isn't going to republish.
func (c *Client) WaitForDiagnostics(ctx context.Context, timeout time.Duration) {
	if c == nil {
		return
	}

	const (
		firstChangeDuration = 1 * time.Second
		settleDuration      = 300 * time.Millisecond
	)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	firstChangeTimer := time.NewTimer(min(timeout, firstChangeDuration))
	defer firstChangeTimer.Stop()
	previousVersion := c.diagnostics.Version()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-firstChangeTimer.C:
			// No change arrived quickly — server isn't republishing.
			return
		case <-ticker.C:
			currentVersion := c.diagnostics.Version()
			if currentVersion != previousVersion {
				// Diagnostics changed — now wait for them to settle.
				c.waitForDiagnosticsToSettle(ctx, deadline.C, settleDuration)
				return
			}
		}
	}
}

// waitForDiagnosticsToSettle waits until diagnostics version stays the same
// for settleDuration, indicating the LSP server has finished publishing.
func (c *Client) waitForDiagnosticsToSettle(ctx context.Context, deadline <-chan time.Time, settleDuration time.Duration) {
	lastVersion := c.diagnostics.Version()
	settleTicker := time.NewTicker(50 * time.Millisecond)
	defer settleTicker.Stop()

	// Track how long the version has been stable.
	stableStart := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-settleTicker.C:
			currentVersion := c.diagnostics.Version()
			if currentVersion != lastVersion {
				// New change detected — reset the stable timer.
				lastVersion = currentVersion
				stableStart = time.Now()
			} else if time.Since(stableStart) >= settleDuration {
				// Diagnostics have been stable for the settle duration.
				return
			}
		}
	}
}
