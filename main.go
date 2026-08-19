package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

//go:embed assets
var assets embed.FS

type Notification struct {
	Number          string `json:"number"`
	Title           string `json:"title"`
	Type            string `json:"type"`
	Reason          string `json:"reason"`
	Repo            string `json:"repo"`
	Owner           string `json:"owner"`
	SubjectURL      string `json:"subject_url"`
	NotificationURL string `json:"notification_url"`
	SubscriptionURL string `json:"subscription_url"`
}

type NotificationDetail struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	URL         string    `json:"url"`
	CommentsURL string    `json:"comments_url"`
	Created     string    `json:"created"`
	Updated     string    `json:"updated"`
	Body        string    `json:"body"`
	Comments    []Comment `json:"comments"`
}

type Comment struct {
	CreatedAt string `json:"created_at"`
	Body      string `json:"body"`
	Author    string `json:"author"`
}

// notificationCacheEntry holds both notification details and comments
type notificationCacheEntry struct {
	detail   *NotificationDetail
	comments []Comment
}

// notificationCache is a simple in-memory cache for notification details and comments
type notificationCache struct {
	mu    sync.RWMutex
	items map[string]*notificationCacheEntry
}

func newNotificationCache() *notificationCache {
	return &notificationCache{
		items: make(map[string]*notificationCacheEntry),
	}
}

func (c *notificationCache) get(subjectURL string) (*notificationCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[subjectURL]
	return entry, ok
}

func (c *notificationCache) setDetail(subjectURL string, detail *NotificationDetail) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items[subjectURL] == nil {
		c.items[subjectURL] = &notificationCacheEntry{}
	}
	c.items[subjectURL].detail = detail
	log.Printf("Cache: added notification details for %s", subjectURL)
}

func (c *notificationCache) setComments(subjectURL string, comments []Comment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items[subjectURL] == nil {
		c.items[subjectURL] = &notificationCacheEntry{}
	}
	c.items[subjectURL].comments = comments
	log.Printf("Cache: added comments for %s", subjectURL)
}

// stringSliceFlag implements flag.Value for a repeatable string flag.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ", ") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func run(ctx context.Context, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:8082", "Address to bind the server to (e.g., 127.0.0.1:8082)")
	dynamicAssets := flags.Bool("dynamic-assets", false, "Serve assets from disk instead of embedded (useful for development)")
	var skipRepos stringSliceFlag
	var skipReviewRequestedFrom stringSliceFlag
	flags.Var(&skipRepos, "skip-repo", "Auto-dismiss all notifications from this repo (owner/name). Repeatable.")
	flags.Var(&skipReviewRequestedFrom, "skip-review-requested-from", "Auto-dismiss review_requested notifications from this org. Repeatable.")

	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Table,
			extension.Strikethrough,
			extension.TaskList,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
	ghCLI := NewGitHubCLI()
	cache := newNotificationCache()

	mux := http.NewServeMux()

	// Serve assets either from disk (useful for debug) or from embed in binary
	if *dynamicAssets {
		mux.Handle("GET /", http.FileServer(http.Dir("assets")))
	} else {
		assetsSub, err := fs.Sub(assets, "assets")
		if err != nil {
			return fmt.Errorf("could not load assets from binary: %w", err)
		}
		mux.Handle("GET /", http.FileServerFS(assetsSub))
	}

	mux.Handle("GET /api/notifications", handleGetNotifications(ghCLI, cache, skipRepos, skipReviewRequestedFrom))
	mux.Handle("GET /api/notification/details", handleGetNotificationDetails(ghCLI, md, cache))
	mux.Handle("POST /api/notification/mark-read", handleMarkNotificationRead(ghCLI))
	var handler http.Handler = mux
	handler = loggingMiddleware(handler)

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	go func() {
		log.Printf("listening on %s\n", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("error listening and serving: %s\n", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Go(func() {
		<-ctx.Done()
		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("error shutting down http server: %s\n", err)
		}
	})
	wg.Wait()
	return nil
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		duration := time.Since(start)
		log.Printf("%s %s %d %.1fms", r.Method, r.URL.Path, rw.status, float64(duration.Microseconds())/1000.0)
	})
}

func markdownToHTML(md goldmark.Markdown, markdown string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		log.Printf("Error converting markdown: %v", err)
		return markdown
	}
	return buf.String()
}

func handleGetNotifications(ghCLI *GitHubCLI, cache *notificationCache, skipRepos, skipReviewRequestedFrom stringSliceFlag) http.Handler {
	skipRepoSet := make(map[string]bool, len(skipRepos))
	for _, r := range skipRepos {
		skipRepoSet[r] = true
	}
	skipReviewOrgSet := make(map[string]bool, len(skipReviewRequestedFrom))
	for _, o := range skipReviewRequestedFrom {
		skipReviewOrgSet[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notifications, err := ghCLI.FetchNotifications()
		if err != nil {
			log.Printf("Error fetching notifications: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var unwantedNotifications []Notification
		var wantedNotifications []Notification
		for _, n := range notifications {
			log.Printf("checking notification %+v", n)
			if n.Reason == "review_requested" && skipReviewOrgSet[n.Owner] {
				unwantedNotifications = append(unwantedNotifications, n)
			} else if skipRepoSet[n.Repo] {
				unwantedNotifications = append(unwantedNotifications, n)
			} else {
				wantedNotifications = append(wantedNotifications, n)
			}
		}

		// Mark unwanted read so we never see them again.
		go func() {
			for _, notif := range unwantedNotifications {
				if notif.NotificationURL == "" {
					continue
				}
				if err := ghCLI.MarkNotificationAsRead(notif.NotificationURL); err != nil {
					log.Printf("Failed to mark review_requested notification as read %s: %v", notif.NotificationURL, err)
				} else {
					log.Printf("Discarded unwanted notification: %s", notif.Title)
				}
			}
		}()

		// Populate notification cache with wanted notifications
		go func() {
			for _, notif := range wantedNotifications {
				if notif.SubjectURL == "" {
					continue
				}

				detail, err := ghCLI.FetchIssueDetails(notif.SubjectURL)
				if err != nil {
					log.Printf("Cache: failed to fetch details for %s: %v", notif.SubjectURL, err)
					continue
				}

				cache.setDetail(notif.SubjectURL, detail)

				// Fetch and cache comments if available
				if detail.CommentsURL != "" {
					comments, err := ghCLI.FetchComments(detail.CommentsURL)
					if err != nil {
						log.Printf("Cache: failed to fetch comments for %s: %v", notif.SubjectURL, err)
					} else {
						cache.setComments(notif.SubjectURL, comments)
					}
				}
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantedNotifications)
	})
}

func handleGetNotificationDetails(ghCLI *GitHubCLI, md goldmark.Markdown, cache *notificationCache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subjectURL := r.URL.Query().Get("subject_url")
		if subjectURL == "" {
			http.Error(w, "subject_url is required", http.StatusBadRequest)
			return
		}

		// Check cache first
		var detail *NotificationDetail
		var comments []Comment
		var err error

		if entry, ok := cache.get(subjectURL); ok {
			if entry.detail != nil {
				log.Printf("Cache: using cached details for %s", subjectURL)
				detail = entry.detail
			}
			if entry.comments != nil {
				log.Printf("Cache: using cached comments for %s", subjectURL)
				comments = entry.comments
			}
		}

		// Fetch details if not in cache
		if detail == nil {
			detail, err = ghCLI.FetchIssueDetails(subjectURL)
			if err != nil {
				log.Printf("Error fetching notification details for %s: %v", subjectURL, err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Convert markdown to HTML
		if detail.Body != "" {
			detail.Body = markdownToHTML(md, detail.Body)
		}

		// Fetch comments if not in cache
		if detail.CommentsURL != "" && comments == nil {
			comments, err = ghCLI.FetchComments(detail.CommentsURL)
			if err != nil {
				log.Printf("Failed to fetch comments from %s: %v", detail.CommentsURL, err)
			}
		}

		if comments == nil {
			comments = []Comment{}
		}

		detail.Comments = comments

		// Convert markdown to HTML for each comment
		for i := range detail.Comments {
			if detail.Comments[i].Body != "" {
				detail.Comments[i].Body = markdownToHTML(md, detail.Comments[i].Body)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detail)
	})
}

func handleMarkNotificationRead(ghCLI *GitHubCLI) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			NotificationURL string `json:"notification_url"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.NotificationURL == "" {
			http.Error(w, "notification_url is required", http.StatusBadRequest)
			return
		}

		if err := ghCLI.MarkNotificationAsRead(req.NotificationURL); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})
}

