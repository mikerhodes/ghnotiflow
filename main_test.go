package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
)

func TestRenderNotificationDetailDoesNotMutateCachedContent(t *testing.T) {
	md := goldmark.New()
	detail := &NotificationDetail{Body: "**description**"}
	comments := []Comment{{Body: "**comment**"}}

	first := renderNotificationDetail(md, detail, comments)
	second := renderNotificationDetail(md, detail, comments)

	if detail.Body != "**description**" {
		t.Fatalf("detail body mutated: %q", detail.Body)
	}
	if comments[0].Body != "**comment**" {
		t.Fatalf("comment body mutated: %q", comments[0].Body)
	}
	if first.Body != second.Body || first.Comments[0].Body != second.Comments[0].Body {
		t.Fatal("repeated rendering produced different content")
	}
	if !strings.Contains(second.Body, "<strong>description</strong>") {
		t.Fatalf("rendered detail body missing content: %q", second.Body)
	}
	if !strings.Contains(second.Comments[0].Body, "<strong>comment</strong>") {
		t.Fatalf("rendered comment body missing content: %q", second.Comments[0].Body)
	}
}

func TestRenderNotificationDetailUsesEmptyCommentsArray(t *testing.T) {
	rendered := renderNotificationDetail(goldmark.New(), &NotificationDetail{}, nil)

	data, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("marshal rendered detail: %v", err)
	}
	if !strings.Contains(string(data), `"comments":[]`) {
		t.Fatalf("comments should be an empty array: %s", data)
	}
}

func TestRenderNotificationDetailPreservesMergedState(t *testing.T) {
	rendered := renderNotificationDetail(goldmark.New(), &NotificationDetail{Merged: true}, nil)

	if !rendered.Merged {
		t.Fatal("merged state was not preserved")
	}
}

func TestRunReturnsWhenAddressIsAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer listener.Close()

	err = run(context.Background(), []string{"ghnotiflow", "-addr", listener.Addr().String()})
	if err == nil {
		t.Fatal("run succeeded with an occupied address")
	}
	if !strings.Contains(err.Error(), "could not listen") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReturnsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, []string{"ghnotiflow", "-addr", "127.0.0.1:0"}); err != nil {
		t.Fatalf("run after cancellation: %v", err)
	}
}

func TestDynamicAssetsRequireIndexFile(t *testing.T) {
	_, err := newAssetsHandler(true, t.TempDir())
	if err == nil {
		t.Fatal("dynamic assets succeeded without index.html")
	}
	if !strings.Contains(err.Error(), "could not load dynamic assets") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDynamicAssetsServeIndexFile(t *testing.T) {
	assetsDir := t.TempDir()
	indexPath := filepath.Join(assetsDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	handler, err := newAssetsHandler(true, assetsDir)
	if err != nil {
		t.Fatalf("create dynamic assets handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "ready" {
		t.Fatalf("body = %q, want %q", response.Body.String(), "ready")
	}
}
