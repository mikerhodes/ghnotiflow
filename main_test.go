package main

import (
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
