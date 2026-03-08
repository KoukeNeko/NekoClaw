package tooling

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const (
	maxResponseBodyBytes  = 1 << 20 // 1 MB download limit
	defaultFetchMaxChars  = 8000
	defaultSearchCount    = 5
	maxSearchCount        = 20
	braveSearchEndpoint   = "https://api.search.brave.com/res/v1/web/search"
	webFetchUserAgent     = "NekoClaw/1.0 (AI Assistant; +https://github.com/doeshing/nekoclaw)"
)

// --- web_fetch ---

func (e *RuntimeExecutor) runWebFetch(raw json.RawMessage) (string, error) {
	var args struct {
		URL      string `json:"url"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}

	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are supported")
	}

	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = defaultFetchMaxChars
	}
	if e.policy.MaxOutputBytes > 0 && maxChars > e.policy.MaxOutputBytes {
		maxChars = e.policy.MaxOutputBytes
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// Limit body read to prevent memory exhaustion.
	limited := io.LimitReader(resp.Body, maxResponseBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	var text string

	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "xhtml") {
		title, extracted := extractTextFromHTML(string(body))
		var sb strings.Builder
		if title != "" {
			sb.WriteString("Title: ")
			sb.WriteString(title)
			sb.WriteByte('\n')
		}
		sb.WriteString("URL: ")
		sb.WriteString(rawURL)
		sb.WriteString("\n\n")
		sb.WriteString(extracted)
		text = sb.String()
	} else {
		// Plain text or other text formats — return as-is.
		text = fmt.Sprintf("URL: %s\n\n%s", rawURL, string(body))
	}

	if len(text) > maxChars {
		text = text[:maxChars] + "\n\n[truncated]"
	}
	return text, nil
}

// extractTextFromHTML parses HTML and returns (title, plainText).
// Skips script, style, and noscript elements.
func extractTextFromHTML(htmlContent string) (string, string) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		// Fallback: return raw content stripped of tags.
		return "", htmlContent
	}

	var title string
	var sb strings.Builder
	var inTitle bool

	// Tags whose content should be skipped entirely.
	skipTags := map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"svg":      true,
		"head":     true,
	}

	// Block-level tags that get newlines around them.
	blockTags := map[string]bool{
		"p": true, "div": true, "br": true, "h1": true, "h2": true,
		"h3": true, "h4": true, "h5": true, "h6": true, "li": true,
		"blockquote": true, "pre": true, "article": true, "section": true,
		"header": true, "footer": true, "main": true, "tr": true,
	}

	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)

			// Track <title> to extract page title.
			if tag == "title" {
				inTitle = true
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, skip)
				}
				inTitle = false
				return
			}

			if skipTags[tag] {
				return
			}

			if blockTags[tag] {
				sb.WriteByte('\n')
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, skip)
			}

			if blockTags[tag] || tag == "br" {
				sb.WriteByte('\n')
			}
			return
		}

		if n.Type == html.TextNode && !skip {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if inTitle && title == "" {
					title = text
				}
				sb.WriteString(text)
				sb.WriteByte(' ')
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}

	walk(doc, false)

	// Clean up excessive whitespace.
	result := collapseWhitespace(sb.String())
	return strings.TrimSpace(title), strings.TrimSpace(result)
}

// collapseWhitespace replaces runs of blank lines with a single blank line
// and trims trailing spaces on each line.
func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blankRun := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			blankRun++
			if blankRun <= 1 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}

// --- web_search ---

// braveSearchResponse is the minimal structure for parsing Brave results.
type braveSearchResponse struct {
	Web struct {
		Results []braveSearchResult `json:"results"`
	} `json:"web"`
}

type braveSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func (e *RuntimeExecutor) runWebSearch(raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	if e.braveSearchKey == "" {
		return "", fmt.Errorf("web_search is not configured: set brave_search_api_key in config.json")
	}

	count := args.Count
	if count <= 0 {
		count = defaultSearchCount
	}
	if count > maxSearchCount {
		count = maxSearchCount
	}

	reqURL := fmt.Sprintf("%s?q=%s&count=%d",
		braveSearchEndpoint,
		url.QueryEscape(query),
		count,
	)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Subscription-Token", e.braveSearchKey)
	req.Header.Set("Accept", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("Brave Search API key is invalid or expired (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Brave Search API error: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read search response failed: %w", err)
	}

	var result braveSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse search response failed: %w", err)
	}

	if len(result.Web.Results) == 0 {
		return "No results found.", nil
	}

	var sb strings.Builder
	for i, r := range result.Web.Results {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(fmt.Sprintf("[%d] %s\n    %s\n    %s\n",
			i+1,
			strings.TrimSpace(r.Title),
			strings.TrimSpace(r.URL),
			strings.TrimSpace(r.Description),
		))
	}

	return truncateHeadTail(sb.String(), e.policy.MaxOutputBytes), nil
}
