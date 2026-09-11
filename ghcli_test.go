package main

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestGitHubCLIReady(t *testing.T) {
	var commandName string
	var commandArgs []string
	cli := &GitHubCLI{
		lookPath: func(name string) (string, error) {
			if name != "gh" {
				t.Fatalf("looked up %q, want gh", name)
			}
			return "/usr/local/bin/gh", nil
		},
		runCommand: func(name string, args ...string) ([]byte, error) {
			commandName = name
			commandArgs = args
			return nil, nil
		},
	}

	if err := cli.CheckReady(); err != nil {
		t.Fatalf("check ready: %v", err)
	}
	if commandName != "/usr/local/bin/gh" {
		t.Fatalf("command name = %q", commandName)
	}
	if strings.Join(commandArgs, " ") != "auth status" {
		t.Fatalf("command args = %q", commandArgs)
	}
}

func TestGitHubCLIReadyRequiresExecutable(t *testing.T) {
	cli := &GitHubCLI{
		lookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}

	err := cli.CheckReady()
	if err == nil {
		t.Fatal("check succeeded without gh")
	}
	if !strings.Contains(err.Error(), "GitHub CLI is not available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGitHubCLIReadyRequiresAuthentication(t *testing.T) {
	cli := &GitHubCLI{
		lookPath: func(string) (string, error) {
			return "/usr/local/bin/gh", nil
		},
		runCommand: func(string, ...string) ([]byte, error) {
			return []byte("not logged in"), errors.New("exit status 1")
		},
	}

	err := cli.CheckReady()
	if err == nil {
		t.Fatal("check succeeded without authentication")
	}
	if !strings.Contains(err.Error(), "GitHub CLI is not authenticated") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("error omitted command output: %v", err)
	}
}

func TestRunChecksGitHubCLIBeforeBinding(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer listener.Close()

	cli := &GitHubCLI{
		lookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}

	err = runWithGitHubCLI(t.Context(), []string{"ghnotiflow", "-addr", listener.Addr().String()}, cli)
	if err == nil {
		t.Fatal("run succeeded without gh")
	}
	if !strings.Contains(err.Error(), "GitHub CLI startup check failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
