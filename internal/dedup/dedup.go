// Package dedup provides notification deduplication to prevent redundant messages.
// Claude Code often fires multiple hook events in quick succession (e.g., Stop + idle_prompt)
// that result in essentially duplicate notifications. This package suppresses duplicates
// within a configurable time window.
//
// IMPORTANT: Dedup state is persisted to a file so it survives across process invocations.
// Each claudible invocation is a separate process, so in-memory state would be lost.
package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/swiftj/claudible/internal/notification"
	"github.com/swiftj/claudible/internal/state"
)

const (
	// DefaultWindow is the default time window for deduplication.
	// Notifications with similar content within this window are considered duplicates.
	// Set to 2 minutes because Claude Code can fire multiple hook events (Stop, Notification)
	// up to 1-2 minutes apart for the same work session.
	DefaultWindow = 2 * time.Minute

	// DefaultCleanupInterval is how often to clean up expired entries.
	DefaultCleanupInterval = 3 * time.Minute

	// stateFileName is the name of the file used to persist dedup state.
	stateFileName = "dedup_state.json"
)

// Deduplicator tracks recent notifications and suppresses duplicates.
type Deduplicator struct {
	mu        sync.Mutex
	window    time.Duration
	recent    map[string]*recentEntry
	stopChan  chan struct{}
	statePath string // Path to persist state file
}

// recentEntry tracks a recently sent notification.
type recentEntry struct {
	Hash      string      `json:"hash"`
	State     state.State `json:"state"`
	Timestamp time.Time   `json:"timestamp"`
	Summary   string      `json:"summary"`
}

// persistedState is the JSON structure saved to disk.
type persistedState struct {
	Entries map[string]*recentEntry `json:"entries"`
}

// New creates a new Deduplicator with the specified time window.
// If window is 0, DefaultWindow is used.
// State is loaded from disk if available, enabling dedup across process invocations.
func New(window time.Duration) *Deduplicator {
	return NewWithPersistence(window, true)
}

// NewWithPersistence creates a new Deduplicator with optional persistence.
// Use persist=false for testing to avoid cross-test contamination.
func NewWithPersistence(window time.Duration, persist bool) *Deduplicator {
	if window <= 0 {
		window = DefaultWindow
	}

	// Determine state file path (~/.config/claudible/dedup_state.json)
	statePath := ""
	if persist {
		if homeDir, err := os.UserHomeDir(); err == nil {
			statePath = filepath.Join(homeDir, ".config", "claudible", stateFileName)
		}
	}

	d := &Deduplicator{
		window:    window,
		recent:    make(map[string]*recentEntry),
		stopChan:  make(chan struct{}),
		statePath: statePath,
	}

	// Load persisted state from disk (only if persistence enabled)
	if persist {
		d.loadState()
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

// cleanup removes expired entries from the recent map and persists state.
func (d *Deduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	changed := false
	for key, entry := range d.recent {
		if now.Sub(entry.Timestamp) > d.window {
			delete(d.recent, key)
			changed = true
		}
	}

	// Persist state after cleanup if anything changed
	if changed {
		d.saveStateLocked()
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

	if exists && now.Sub(entry.Timestamp) <= d.window {
		// We have a recent notification for this session

		// Primary check: same request = same work, likely duplicate
		if entry.Hash == requestHash {
			// Same work detected within the time window

			// If we already sent a COMPLETE, always suppress subsequent messages
			if entry.State == state.StateComplete {
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
		Hash:      requestHash,
		State:     msg.State,
		Timestamp: now,
		Summary:   msg.Summary,
	}

	// Persist state after recording new entry
	d.saveStateLocked()

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
		Hash:      d.hashRequest(msg),
		State:     msg.State,
		Timestamp: time.Now(),
		Summary:   msg.Summary,
	}

	// Persist state
	d.saveStateLocked()
}

// Clear removes all tracked notifications (useful for testing).
func (d *Deduplicator) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recent = make(map[string]*recentEntry)
	d.saveStateLocked()
}

// loadState loads the persisted dedup state from disk.
// If the file doesn't exist or is invalid, it starts fresh.
func (d *Deduplicator) loadState() {
	if d.statePath == "" {
		return
	}

	data, err := os.ReadFile(d.statePath)
	if err != nil {
		// File doesn't exist or can't be read, start fresh
		return
	}

	var persisted persistedState
	if err := json.Unmarshal(data, &persisted); err != nil {
		// Invalid JSON, start fresh
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Load entries, but only if they haven't expired
	now := time.Now()
	for key, entry := range persisted.Entries {
		if now.Sub(entry.Timestamp) <= d.window {
			d.recent[key] = entry
		}
	}
}

// saveStateLocked persists the current dedup state to disk.
// Must be called with d.mu held.
func (d *Deduplicator) saveStateLocked() {
	if d.statePath == "" {
		return
	}

	persisted := persistedState{
		Entries: d.recent,
	}

	data, err := json.Marshal(persisted)
	if err != nil {
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(d.statePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	// Write atomically by writing to temp file then renaming
	tmpPath := d.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, d.statePath)
}
