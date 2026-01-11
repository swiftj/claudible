// Package dedup provides notification deduplication to prevent redundant messages.
// Claude Code often fires multiple hook events in quick succession (e.g., Stop + idle_prompt)
// that result in essentially duplicate notifications. This package suppresses duplicates
// within a configurable time window.
package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/swiftj/claudible/internal/notification"
	"github.com/swiftj/claudible/internal/state"
)

const (
	// DefaultWindow is the default time window for deduplication.
	// Notifications with similar content within this window are considered duplicates.
	DefaultWindow = 30 * time.Second

	// DefaultCleanupInterval is how often to clean up expired entries.
	DefaultCleanupInterval = 1 * time.Minute
)

// Deduplicator tracks recent notifications and suppresses duplicates.
type Deduplicator struct {
	mu       sync.Mutex
	window   time.Duration
	recent   map[string]*recentEntry
	stopChan chan struct{}
}

// recentEntry tracks a recently sent notification.
type recentEntry struct {
	hash      string
	state     state.State
	timestamp time.Time
	summary   string
}

// New creates a new Deduplicator with the specified time window.
// If window is 0, DefaultWindow is used.
func New(window time.Duration) *Deduplicator {
	if window <= 0 {
		window = DefaultWindow
	}

	d := &Deduplicator{
		window:   window,
		recent:   make(map[string]*recentEntry),
		stopChan: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go d.cleanupLoop()

	return d
}

// Stop stops the background cleanup goroutine.
func (d *Deduplicator) Stop() {
	close(d.stopChan)
}

// cleanupLoop periodically removes expired entries.
func (d *Deduplicator) cleanupLoop() {
	ticker := time.NewTicker(DefaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.cleanup()
		case <-d.stopChan:
			return
		}
	}
}

// cleanup removes expired entries from the recent map.
func (d *Deduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, entry := range d.recent {
		if now.Sub(entry.timestamp) > d.window {
			delete(d.recent, key)
		}
	}
}

// ShouldSend checks if a notification should be sent or suppressed as a duplicate.
// Returns true if the notification should be sent, false if it's a duplicate.
func (d *Deduplicator) ShouldSend(msg notification.NotificationMessage) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Generate a key based on session ID (or cwd if no session)
	key := msg.SessionID
	if key == "" {
		key = msg.Cwd
	}
	if key == "" {
		// No session or cwd, can't deduplicate - allow it
		return true
	}

	// Generate request hash - this identifies the "work" being done
	// The Request (user prompt) is the most stable identifier across duplicate events
	requestHash := d.hashRequest(msg)

	now := time.Now()
	entry, exists := d.recent[key]

	if exists && now.Sub(entry.timestamp) <= d.window {
		// We have a recent notification for this session

		// Primary check: same request = same work, likely duplicate
		if entry.hash == requestHash {
			// Same work detected within the time window

			// If we already sent a COMPLETE, always suppress subsequent messages
			if entry.state == state.StateComplete {
				return false
			}

			// If we sent WAITING and now have COMPLETE, suppress the duplicate
			// The first notification (WAITING or COMPLETE) wins
			// This prevents the "double notification" problem entirely
			return false
		}

		// Different request hash = different work, allow through
	}

	// New notification or different work - record and allow
	d.recent[key] = &recentEntry{
		hash:      requestHash,
		state:     msg.State,
		timestamp: now,
		summary:   msg.Summary,
	}

	return true
}

// hashRequest creates a hash of the user request for work identification.
// The Request (user prompt) is stable across duplicate events, unlike the Summary
// which may vary depending on LLM responses or state classification.
func (d *Deduplicator) hashRequest(msg notification.NotificationMessage) string {
	// Use the request as the primary identifier for "same work"
	// Fall back to summary if request is empty
	content := msg.Request
	if content == "" {
		content = msg.Summary
	}
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:8]) // First 8 bytes is enough
}

// hashContent creates a hash of the notification content for exact duplicate detection.
func (d *Deduplicator) hashContent(msg notification.NotificationMessage) string {
	// Hash the summary and request together (these identify the "work")
	content := msg.Summary + "|" + msg.Request
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:8]) // First 8 bytes is enough
}

// isSimilarContent checks if two summaries are similar enough to be considered duplicates.
// This handles cases where the LLM generates slightly different summaries for the same work.
func (d *Deduplicator) isSimilarContent(a, b string) bool {
	// If either is empty, not similar
	if a == "" || b == "" {
		return false
	}

	// Exact match
	if a == b {
		return true
	}

	// Check for high overlap using simple heuristics:
	// If one is a prefix/suffix of the other (within reason), consider similar
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	// If the shorter string is at least 50 chars and is contained in the longer, similar
	if minLen >= 50 {
		if len(a) > len(b) && contains(a, b) {
			return true
		}
		if len(b) > len(a) && contains(b, a) {
			return true
		}
	}

	// Check for common prefix (at least 60% overlap)
	commonPrefix := 0
	for i := 0; i < minLen && a[i] == b[i]; i++ {
		commonPrefix++
	}

	if float64(commonPrefix)/float64(minLen) >= 0.6 {
		return true
	}

	return false
}

// contains checks if s contains substr (simple implementation).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

// findSubstring finds the index of substr in s, or -1 if not found.
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// RecordSent records that a notification was sent (for external tracking).
// This is useful when the caller wants to record a send without going through ShouldSend.
func (d *Deduplicator) RecordSent(msg notification.NotificationMessage) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := msg.SessionID
	if key == "" {
		key = msg.Cwd
	}
	if key == "" {
		return
	}

	d.recent[key] = &recentEntry{
		hash:      d.hashRequest(msg),
		state:     msg.State,
		timestamp: time.Now(),
		summary:   msg.Summary,
	}
}

// Clear removes all tracked notifications (useful for testing).
func (d *Deduplicator) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recent = make(map[string]*recentEntry)
}
