package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSearchTool performs a simple HTTP GET to fetch web content.
// Uses DuckDuckGo HTML for search (no API key required).
type WebSearchTool struct{}

// NewWebSearchTool creates a web search tool.
func NewWebSearchTool() WebSearchTool { return WebSearchTool{} }

func (WebSearchTool) Name() string { return "web" }
func (WebSearchTool) Description() string {
	return "Fetch web content. Input: URL or search query (uses DDG)"
}

// Execute fetches the URL or performs a DDG search.
func (WebSearchTool) Execute(ctx context.Context, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("usage: web <url|query>")
	}

	client := &http.Client{Timeout: 15 * time.Second}

	// If it's a URL, fetch directly
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return fetchURL(ctx, client, input)
	}

	// Otherwise, search DDG
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(input)
	return fetchURL(ctx, client, searchURL)
}

func fetchURL(ctx context.Context, client *http.Client, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "jito/0.1.0 (open-uppu agent)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100_000)) // cap at 100KB
	if err != nil {
		return "", err
	}

	// Strip HTML tags for plain text output (very basic)
	text := stripHTML(string(body))
	return truncate(text, 4000), nil
}

func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	// Collapse whitespace
	return strings.Join(strings.Fields(out.String()), " ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}