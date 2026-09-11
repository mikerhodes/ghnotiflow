package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitHubCLI handles interactions with the GitHub CLI
type GitHubCLI struct {
	lookPath   func(string) (string, error)
	runCommand func(string, ...string) ([]byte, error)
}

// NewGitHubCLI creates a new GitHubCLI instance with default configuration
func NewGitHubCLI() *GitHubCLI {
	return &GitHubCLI{
		lookPath: exec.LookPath,
		runCommand: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
	}
}

// CheckReady verifies that the GitHub CLI is installed and authenticated.
func (g *GitHubCLI) CheckReady() error {
	ghPath, err := g.lookPath("gh")
	if err != nil {
		return fmt.Errorf("GitHub CLI is not available: %w", err)
	}

	output, err := g.runCommand(ghPath, "auth", "status")
	if err != nil {
		return fmt.Errorf("GitHub CLI is not authenticated: %v - %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// FetchNotifications retrieves all notifications from GitHub using gh CLI
func (g *GitHubCLI) FetchNotifications() ([]Notification, error) {
	output, err := g.runCommand("gh", "api", "notifications", "--paginate", "--jq",
		`.[] | {
			number: ((.subject.url // "") | split("/") | last),
			title: .subject.title,
			type: .subject.type,
			reason: .reason,
			repo: .repository.full_name,
			owner: .repository.owner.login,
			subject_url: .subject.url,
			notification_url: .url,
			subscription_url: .subscription_url
		}`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch notifications: %v - %s", err, string(output))
	}

	// Parse JSON lines
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	notifications := make([]Notification, 0)

	for _, line := range lines {
		if line == "" {
			continue
		}
		var notif Notification
		if err := json.Unmarshal([]byte(line), &notif); err != nil {
			continue
		}
		notifications = append(notifications, notif)
	}

	return notifications, nil
}

// FetchIssueDetails retrieves issue/PR details from GitHub using gh CLI
func (g *GitHubCLI) FetchIssueDetails(subjectURL string) (*NotificationDetail, error) {
	output, err := g.runCommand("gh", "api", subjectURL, "--jq",
		`{
			number: .number,
			title: .title,
			state: .state,
			merged: (.merged_at != null),
			url: .html_url,
			comments_url: .comments_url,
			created: .created_at,
			updated: .updated_at,
			body: .body
		}`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch notification details: %v - %s", err, string(output))
	}

	var detail NotificationDetail
	if err := json.Unmarshal(output, &detail); err != nil {
		return nil, fmt.Errorf("failed to parse notification details: %v", err)
	}

	return &detail, nil
}

// FetchComments retrieves all comments for an issue/PR from GitHub using gh CLI
func (g *GitHubCLI) FetchComments(commentsURL string) ([]Comment, error) {
	if commentsURL == "" {
		return nil, nil
	}

	output, err := g.runCommand("gh", "api", commentsURL, "--paginate", "--jq",
		`[.[] | {created_at: .created_at, body: .body, author: .user.login}]`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch comments: %v - %s", err, string(output))
	}

	var comments []Comment
	if err := json.Unmarshal(output, &comments); err != nil {
		return nil, fmt.Errorf("failed to parse comments: %v", err)
	}

	return comments, nil
}

// MarkNotificationAsRead marks a notification as read using gh CLI
func (g *GitHubCLI) MarkNotificationAsRead(notificationURL string) error {
	output, err := g.runCommand("gh", "api", "-X", "PATCH", notificationURL)
	if err != nil {
		return fmt.Errorf("failed to mark notification as read: %v - %s", err, string(output))
	}
	return nil
}
