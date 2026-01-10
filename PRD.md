Claude Code Notification Hooks

Product Requirements Document (PRD)

⸻

1. Overview

1.1 Purpose

Claude Code Notification Hooks provide a pluggable, configurable, and extensible notification system that informs users when a Claude Code agent:
	•	has completed the work requested,
	•	is waiting for user input, or
	•	has stopped execution for any reason (success, interruption, error).

The system is designed to support multiple notification delivery mechanisms (iMessage, SMS, HTTP push, etc.), with AppleScript iMessage as the default implementation, while remaining portable across macOS, Linux, and cloud or headless environments.

⸻

1.2 Goals
	•	Deliver reliable, low-friction notifications when Claude Code changes state
	•	Allow rich contextual messages derived from agent transcripts
	•	Support multiple notification transports via a unified abstraction
	•	Require zero modification to Claude Code itself
	•	Be usable both interactively (local dev) and unattended (long-running tasks)

⸻

1.3 Non-Goals
	•	Replacing Claude Code’s built-in UI or logs
	•	Providing real-time streaming notifications
	•	Acting as a messaging platform (no inbox, replies, or conversations)
	•	Bypassing platform security models (e.g., iMessage on non-Apple systems)

⸻

2. Target Users

Persona	Description
Power Developer	Runs long Claude Code tasks and wants async notifications
Vibe Coder	Iteratively prompts Claude and wants “done / waiting” alerts
Remote Worker	Leaves laptop unattended and wants phone notifications
CI / Automation User	Runs Claude Code in scripts or pipelines
macOS User	Wants native Messages (iMessage) notifications
Cross-Platform User	Needs SMS, push, or HTTP notifications


⸻

3. High-Level Architecture

Claude Code
   │
   │ (Hook Event)
   ▼
Hook Runner (stdin JSON)
   │
   ├─ Context Extractor
   │     ├─ transcript.jsonl
   │     ├─ last user prompt
   │     ├─ last assistant output
   │     └─ smart state classification
   │
   ├─ Notification Router
   │     ├─ iMessage (AppleScript)
   │     ├─ SMS (Twilio / Vonage / etc.)
   │     ├─ HTTP Push (Pushcut / Pushover / Webhook)
   │     └─ Custom Plugins
   │
   ▼
User Notification (Phone / App / Service)


⸻

4. Hook Event Support

4.1 Supported Claude Code Events

Event	Description
idle_prompt	Agent is waiting for user input
stop	Agent has stopped responding
completion	Agent completed a task (derived)

Note: Completion is inferred via transcript analysis and/or prompt-based evaluation.

⸻

4.2 Hook Input Contract

Hooks receive a JSON payload via stdin, containing (at minimum):

{
  "session_id": "abc123",
  "cwd": "/path/to/repo",
  "transcript_path": "/path/to/transcript.jsonl",
  "notification_type": "idle_prompt"
}


⸻

5. Context Extraction & Intelligence

5.1 Transcript Parsing
	•	Transcripts are read from transcript_path (JSONL)
	•	Extract:
	•	Last user prompt
	•	Last assistant response
	•	Key completion indicators (e.g. “done”, “tests pass”)

⸻

5.2 Smart State Classification

The system determines a semantic state:

State	Meaning
complete	Work is done
waiting	Claude expects user input
stopped	Execution stopped unexpectedly
idle	Neutral waiting state


⸻

5.3 Prompt-Based Evaluation (Optional)

For advanced users, a secondary LLM-based evaluator may be enabled:
	•	Inputs:
	•	Last user request
	•	Last assistant output
	•	Output:

{
  "state": "complete | waiting | needs_review",
  "summary": "One-line human readable summary",
  "confidence": 0.93
}



Fallback to heuristic classification if unavailable.

⸻

6. Notification System

6.1 Notification Abstraction

All notifications conform to a common interface:

interface NotificationProvider {
  name: string
  enabled: boolean
  send(message: NotificationMessage): Promise<void>
}


⸻

6.2 Notification Message Format

{
  "title": "Claude finished",
  "state": "complete",
  "summary": "Implemented feature X and tests passed",
  "request": "Add caching to API layer",
  "cwd": "/repo/path",
  "session_id": "abc123",
  "timestamp": "2026-01-09T13:45:00Z"
}

Rendered to a human-optimized short message per transport.

⸻

7. Notification Providers

7.1 AppleScript iMessage (Default)

Platform: macOS
Transport: iMessage (Messages.app)
Cost: Free

Requirements
	•	Messages.app logged in
	•	Automation permissions granted
	•	Mac must be awake (screen may be locked)

Behavior
	•	Sends iMessage to the user’s own Apple ID or phone number
	•	Works unattended

⸻

7.2 SMS Provider (Pluggable)

Examples
	•	Twilio
	•	Vonage
	•	MessageBird
	•	Plivo

Configuration

sms:
  provider: twilio
  enabled: true
  from: "+1XXXXXXXXXX"
  to: "+1YYYYYYYYYY"


⸻

7.3 HTTP Push / Webhook

Examples
	•	Pushcut
	•	Pushover
	•	IFTTT
	•	Zapier
	•	Custom webhooks

Configuration

http:
  enabled: true
  endpoint: "https://api.pushcut.io/XXXX"
  headers:
    Content-Type: application/json


⸻

7.4 Desktop Notifications (Optional)
	•	macOS osascript display notification
	•	Linux notify-send
	•	Windows Toast (future)

⸻

8. Configuration

8.1 Global Config File

notifications:
  default_provider: imessage
  providers:
    imessage:
      enabled: true
      target: "+17034737862"

    sms:
      enabled: false

    http:
      enabled: false

behavior:
  notify_on:
    - complete
    - waiting
  max_message_length: 900
  include_repo_path: true


⸻

8.2 Provider Selection Rules
	1.	Explicitly enabled providers
	2.	Default provider fallback
	3.	Optional fan-out to multiple providers

⸻

9. Reliability & Safety

9.1 Failure Handling
	•	Provider failures are non-fatal
	•	Log errors, continue execution
	•	Optional fallback providers

⸻

9.2 Privacy & Security
	•	No data sent unless provider enabled
	•	No credentials logged
	•	Local execution only unless HTTP/SMS enabled

⸻

10. Extensibility

10.1 Custom Providers

Users may add new providers by implementing:

providers/
  slack.sh
  discord.py
  pagerduty.js

Registered via config.

⸻

10.2 Future Enhancements
	•	Notification batching
	•	Priority levels
	•	Quiet hours / Focus integration
	•	Reply-based triggers (where supported)
	•	Web UI for configuration

⸻

11. Success Metrics
	•	Notification delivery success rate
	•	False “complete” vs “waiting” classification rate
	•	Time-to-notification after agent completion
	•	User-reported satisfaction

⸻

12. Summary

This system transforms Claude Code from a blocking, foreground tool into an asynchronous, ambient assistant that:
	•	Works while you step away
	•	Respects platform preferences
	•	Feels native on macOS
	•	Scales to headless and CI environments
	•	Encourages long-running, high-leverage Claude workflows
	•	Lastly, this entire project must be written in Golang
