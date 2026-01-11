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

	// Generate content hash for similarity detection
	hash := d.hashContent(msg)

	now := time.Now()
	entry, exists := d.recent[key]

	if exists && now.Sub(entry.timestamp) <= d.window {
		// We have a recent notification for this session

		// Check if content is similar (same hash = same content)
		if entry.hash == hash {
			// Exact duplicate - suppress
			return false
		}

		// Check if content is similar but states differ
		// This handles the case where COMPLETE and WAITING arrive for the same work
		if d.isSimilarContent(entry.summary, msg.Summary) {
			// Similar content with different state
			// Prefer COMPLETE over WAITING (COMPLETE is more informative)
			if entry.state == state.StateComplete && msg.State == state.StateWaiting {
				// Already sent COMPLETE, suppress WAITING
				return false
			}
			if entry.state == state.StateWaiting && msg.State == state.StateComplete {
				// Sent WAITING but now have COMPLETE - allow COMPLETE through
				// Update the entry to COMPLETE
				entry.state = msg.State
				entry.hash = hash
				entry.summary = msg.Summary
				entry.timestamp = now
				return true
			}
			// Same or other state combinations with similar content - suppress
			return false
		}
	}

	// New notification or different content - record and allow
	d.recent[key] = &recentEntry{
		hash:      hash,
		state:     msg.State,
		timestamp: now,
		summary:   msg.Summary,
	}

	return true
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
		hash:      d.hashContent(msg),
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
