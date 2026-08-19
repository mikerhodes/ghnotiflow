# GitHub Notification Processor

A web-based GitHub notification viewer with a keyboard-driven interface.

- Navigate with keyboard shortcuts (J, K, L, O)
- Mark notifications as read
- Open issues directly in GitHub

## Setup

### Prerequisites

GitHub CLI (`gh`) must be installed and authenticated:

```bash
gh auth login
```

### Running

Build with `go build .`, then:

```
Usage: ghnotiflow [flags]

  -addr string
        Address to bind the server to (default "127.0.0.1:8082")
  -dynamic-assets
        Serve assets from disk instead of embedded (useful for development)
  -skip-repo owner/name
        Auto-dismiss all notifications from this repo. Repeatable.
  -skip-review-requested-from org
        Auto-dismiss review_requested notifications from this org. Repeatable.
```

Then open http://127.0.0.1:8082 in your browser.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `J` | Scroll down |
| `K` | Scroll up |
| `L` | Mark notification as read and continue to next |
| `O` | Open issue/PR in GitHub |
