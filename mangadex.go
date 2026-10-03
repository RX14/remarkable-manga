// mangadex.go: the slice of the MangaDex API this tool consumes (chapter metadata).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const mangadexAPI = "https://api.mangadex.org"

// userAgent is sent on every request. MangaDex requires a non-spoofed User-Agent
// on all requests (docs: Limitations and Requirements).
const userAgent = "remarkable-manga/0.1 (personal manga-to-reMarkable tool)"

// Chapter is the subset of MangaDex chapter metadata we consume.
type Chapter struct {
	ID          string
	Chapter     string // chapter number as published ("12", "5.5"); empty when the release has none
	Title       string // chapter title as published; empty when none
	Pages       int    // page count the release declares
	Version     int    // entity edit counter; bumps when an upload is fixed in place
	PublishedAt time.Time
	SeriesName  string // localized series title, see pickTitle
}

// Client talks to the MangaDex API.
type Client struct {
	http *http.Client
	log  *slog.Logger
}

// NewClient returns a Client with sane timeouts and default logging.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second},
		log:  slog.Default(),
	}
}

// APIError reports a non-2xx MangaDex response. X-Request-ID is included because
// MangaDex asks for it when reporting problems (docs: Issues and Questions).
type APIError struct {
	Op        string
	Status    int
	RequestID string
	Detail    string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("mangadex %s: HTTP %d", e.Op, e.Status)
	if e.RequestID != "" {
		msg += fmt.Sprintf(" (X-Request-ID %s)", e.RequestID)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// get performs a GET and decodes the JSON response into out.
func (c *Client) get(ctx context.Context, op, endpoint string, q url.Values, out any) error {
	u := mangadexAPI + endpoint
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("mangadex %s: build request: %w", op, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mangadex %s: %w", op, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("mangadex %s: read response: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{
			Op:        op,
			Status:    resp.StatusCode,
			RequestID: resp.Header.Get("X-Request-ID"),
			Detail:    apiErrorDetail(body),
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("mangadex %s: decode response: %w", op, err)
	}
	return nil
}

// apiErrorDetail pulls the human-readable part out of a MangaDex error envelope.
func apiErrorDetail(body []byte) string {
	var e struct {
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil || len(e.Errors) == 0 {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 200 {
			detail = detail[:200]
		}
		return detail
	}
	first := e.Errors[0]
	return strings.TrimSpace(first.Title + " " + first.Detail)
}

// FetchChapter fetches one chapter's metadata by UUID, including its series title.
func (c *Client) FetchChapter(ctx context.Context, id string) (Chapter, error) {
	q := url.Values{}
	q.Add("includes[]", "manga")

	var raw struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Chapter   string    `json:"chapter"`
				Title     string    `json:"title"`
				Pages     int       `json:"pages"`
				Version   int       `json:"version"`
				PublishAt time.Time `json:"publishAt"`
			} `json:"attributes"`
			Relationships []struct {
				Type       string          `json:"type"`
				Attributes json.RawMessage `json:"attributes"` // present when requested via includes[]
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := c.get(ctx, "chapter "+id, "/chapter/"+url.PathEscape(id), q, &raw); err != nil {
		return Chapter{}, err
	}

	ch := Chapter{
		ID:          raw.Data.ID,
		Chapter:     raw.Data.Attributes.Chapter,
		Title:       raw.Data.Attributes.Title,
		Pages:       raw.Data.Attributes.Pages,
		Version:     raw.Data.Attributes.Version,
		PublishedAt: raw.Data.Attributes.PublishAt,
	}
	for _, rel := range raw.Data.Relationships {
		if rel.Type != "manga" || len(rel.Attributes) == 0 {
			continue
		}
		var m struct {
			Title     map[string]string   `json:"title"`
			AltTitles []map[string]string `json:"altTitles"`
		}
		if err := json.Unmarshal(rel.Attributes, &m); err != nil {
			return Chapter{}, fmt.Errorf("mangadex chapter %s: decode manga relationship: %w", id, err)
		}
		ch.SeriesName = pickTitle(m.Title, m.AltTitles)
	}
	if ch.SeriesName == "" {
		return Chapter{}, fmt.Errorf("mangadex chapter %s: response carried no series title (was includes[]=manga honored?)", id)
	}
	return ch, nil
}

// pickTitle chooses a display title from MangaDex localized title maps.
// Preference: English, then romanized Japanese (the "ja-ro" tag; romanized
// variants carry the -ro suffix, e.g. "ko-ro"), then fallback: first non-empty
// title by sorted language tag so the choice is deterministic.
func pickTitle(title map[string]string, altTitles []map[string]string) string {
	byLang := map[string]string{} // canonical title wins over alternates per language
	add := func(m map[string]string) {
		for lang, name := range m {
			if _, seen := byLang[lang]; !seen && name != "" {
				byLang[lang] = name
			}
		}
	}
	add(title)
	for _, alt := range altTitles {
		add(alt)
	}

	for _, lang := range []string{"en", "ja-ro"} {
		if s := byLang[lang]; s != "" {
			return s
		}
	}
	langs := make([]string, 0, len(byLang))
	for lang := range byLang {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		if byLang[lang] != "" {
			return byLang[lang]
		}
	}
	return ""
}

// documentName is the on-device document name: "CH12 Series Name", or just the
// series name when the release has no chapter number.
func documentName(ch Chapter) string {
	if ch.Chapter == "" {
		return ch.SeriesName
	}
	return "CH" + ch.Chapter + " " + ch.SeriesName
}

// FetchPageURLs resolves a chapter's page download URLs at original quality
// ("data", pixel-for-pixel as uploaded), in reading order.
//
// URLs are ephemeral: MangaDex guarantees the base URL for only 15 minutes, so
// resolve just-in-time at download and never persist the result. Chapter IDs
// are durable; page URLs are not.
//
// The base URL is an opaque string — per the docs it may be any scheme, host,
// port and path shape ("use it as-is"), so we only ever concatenate.
func (c *Client) FetchPageURLs(ctx context.Context, chapterID string) ([]string, error) {
	var raw struct {
		BaseURL string `json:"baseUrl"`
		Chapter struct {
			Hash string   `json:"hash"`
			Data []string `json:"data"` // ordered filenames = reading order
		} `json:"chapter"`
	}
	endpoint := "/at-home/server/" + url.PathEscape(chapterID)
	if err := c.get(ctx, "at-home "+chapterID, endpoint, nil, &raw); err != nil {
		return nil, err
	}
	if raw.BaseURL == "" || raw.Chapter.Hash == "" || len(raw.Chapter.Data) == 0 {
		return nil, fmt.Errorf("mangadex at-home %s: empty baseUrl/hash or no page files (placeholder/external chapter?)", chapterID)
	}

	urls := make([]string, len(raw.Chapter.Data))
	for i, name := range raw.Chapter.Data {
		urls[i] = raw.BaseURL + "/data/" + raw.Chapter.Hash + "/" + name
	}
	return urls, nil
}

// atHomeReportURL is the MangaDex@Home network's retrieval-report endpoint
// (api.mangadex.network, NOT api.mangadex.org).
const atHomeReportURL = "https://api.mangadex.network/report"

// DownloadPages downloads the chapter's pages in reading order. urls must come
// from FetchPageURLs for the same chapterID.
//
// On a failed fetch (unhealthy @Home node, or a base URL gone stale) we
// re-resolve a fresh base URL and retry the page once before failing.
//
// Note: the MD@Home *report* call described in the docs is deliberately not
// implemented. Reporting is confirmed no longer required (MangaDex's own site
// stopped sending it; Keiyoushi extensions-source#10250, 2025-08) and the
// endpoint has been chronically dead (522) in practice since 2025-03
// (mangadex-downloader#146).
func (c *Client) DownloadPages(ctx context.Context, chapterID string, urls []string) ([][]byte, error) {
	pages := make([][]byte, len(urls))
	for i := range urls {
		data, cached, dur, err := c.fetchImage(ctx, urls[i])
		if err == nil {
			c.log.Debug("page fetched", "page", i+1, "of", len(urls), "bytes", len(data), "dur", dur, "cached", cached)
			pages[i] = data
			continue
		}

		c.log.Warn("page fetch failed, re-resolving base URL", "page", i+1, "of", len(urls), "err", err)
		fresh, ferr := c.FetchPageURLs(ctx, chapterID)
		if ferr != nil {
			return nil, fmt.Errorf("page %d of %d: %w (re-resolving base URL also failed: %v)", i+1, len(urls), err, ferr)
		}
		if len(fresh) != len(urls) {
			return nil, fmt.Errorf("page %d of %d: %w (re-resolve returned %d pages, want %d)", i+1, len(urls), err, len(fresh), len(urls))
		}
		urls = fresh

		data, cached, dur, err = c.fetchImage(ctx, urls[i])
		if err != nil {
			return nil, fmt.Errorf("page %d of %d (after base-URL refresh): %w", i+1, len(urls), err)
		}
		c.log.Debug("page fetched after refresh", "page", i+1, "of", len(urls), "bytes", len(data), "dur", dur, "cached", cached)
		pages[i] = data
	}
	return pages, nil
}

// fetchImage retrieves one page file. It deliberately attaches no auth headers:
// @Home base URLs are third-party volunteer nodes, and per the docs any auth
// header here leaks the token to the operator (and is rejected on own infra).
// duration is complete-retrieval time, kept for logging.
func (c *Client) fetchImage(ctx context.Context, u string) (data []byte, cached bool, duration time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, 0, fmt.Errorf("image %s: %w", u, err)
	}
	req.Header.Set("User-Agent", userAgent)

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, time.Since(start), fmt.Errorf("image %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, time.Since(start), fmt.Errorf("image %s: HTTP %d", u, resp.StatusCode)
	}
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, time.Since(start), fmt.Errorf("image %s: read body: %w", u, err)
	}
	return data, strings.HasPrefix(resp.Header.Get("X-Cache"), "HIT"), time.Since(start), nil
}

// FetchFeed fetches a manga's chapter feed in English.
func (c *Client) FetchFeed(ctx context.Context, mangaID string) ([]Chapter, error) {
	q := url.Values{}
	q.Set("limit", "50") // a week of releases, with room for batch drops
	q.Set("order[publishAt]", "desc")
	q.Add("translatedLanguage[]", "en")

	var raw struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Chapter   string    `json:"chapter"`
				Title     string    `json:"title"`
				Pages     int       `json:"pages"`
				Version   int       `json:"version"`
				PublishAt time.Time `json:"publishAt"`
			} `json:"attributes"`
		} `json:"data"`
	}
	endpoint := "/manga/" + url.PathEscape(mangaID) + "/feed"
	if err := c.get(ctx, "feed "+mangaID, endpoint, q, &raw); err != nil {
		return nil, err
	}
	chapters := make([]Chapter, len(raw.Data))
	for i, d := range raw.Data {
		chapters[i] = Chapter{
			ID:          d.ID,
			Chapter:     d.Attributes.Chapter,
			Title:       d.Attributes.Title,
			Pages:       d.Attributes.Pages,
			Version:     d.Attributes.Version,
			PublishedAt: d.Attributes.PublishAt,
		}
	}
	return chapters, nil
}

// SearchResult is one work with the disambiguators the card shows.
type SearchResult struct {
	ID           string
	Name         string // picked title, see pickTitle
	Year         int
	OriginalLang string
	Author       string
	HasEN        bool
}

// AuthorResult is one author/artist hit.
type AuthorResult struct {
	ID   string
	Name string
}

// mangaWire is the subset of a manga resource we consume in search results.
type mangaWire struct {
	ID         string `json:"id"`
	Attributes struct {
		Title                        map[string]string   `json:"title"`
		AltTitles                    []map[string]string `json:"altTitles"`
		Year                         int                 `json:"year"`
		OriginalLanguage             string              `json:"originalLanguage"`
		AvailableTranslatedLanguages []string            `json:"availableTranslatedLanguages"`
	} `json:"attributes"`
	Relationships []struct {
		Type       string `json:"type"`
		Attributes struct {
			Name string `json:"name"`
		} `json:"attributes"`
	} `json:"relationships"`
}

func (m mangaWire) result() SearchResult {
	r := SearchResult{
		ID:           m.ID,
		Name:         pickTitle(m.Attributes.Title, m.Attributes.AltTitles),
		Year:         m.Attributes.Year,
		OriginalLang: m.Attributes.OriginalLanguage,
	}
	for _, lang := range m.Attributes.AvailableTranslatedLanguages {
		if lang == "en" {
			r.HasEN = true
			break
		}
	}
	for _, rel := range m.Relationships {
		if rel.Type == "author" {
			r.Author = rel.Attributes.Name
			break
		}
	}
	return r
}

// SearchManga finds works by title (canonical and localized names). Results
// with English chapters rank above untranslated ones; stable otherwise.
func (c *Client) SearchManga(ctx context.Context, query string) ([]SearchResult, error) {
	q := url.Values{}
	q.Set("title", query)
	q.Set("limit", "20")
	q.Add("includes[]", "author")
	addAllRatings(q)
	var raw struct {
		Data []mangaWire `json:"data"`
	}
	if err := c.get(ctx, "search "+query, "/manga", q, &raw); err != nil {
		return nil, err
	}
	return rankResults(raw.Data), nil
}

// SearchAuthors finds authors and artists by name.
func (c *Client) SearchAuthors(ctx context.Context, query string) ([]AuthorResult, error) {
	q := url.Values{}
	q.Set("name", query)
	q.Set("limit", "10")
	var raw struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.get(ctx, "author "+query, "/author", q, &raw); err != nil {
		return nil, err
	}
	authors := make([]AuthorResult, len(raw.Data))
	for i, a := range raw.Data {
		authors[i] = AuthorResult{ID: a.ID, Name: a.Attributes.Name}
	}
	return authors, nil
}

// WorksByAuthor lists an author's or artist's works.
func (c *Client) WorksByAuthor(ctx context.Context, authorID string) ([]SearchResult, error) {
	q := url.Values{}
	q.Set("authorOrArtist", authorID)
	q.Set("limit", "50")
	q.Add("includes[]", "author")
	addAllRatings(q)
	var raw struct {
		Data []mangaWire `json:"data"`
	}
	if err := c.get(ctx, "works "+authorID, "/manga", q, &raw); err != nil {
		return nil, err
	}
	return rankResults(raw.Data), nil
}

func rankResults(data []mangaWire) []SearchResult {
	results := make([]SearchResult, len(data))
	for i, d := range data {
		results[i] = d.result()
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].HasEN && !results[j].HasEN })
	return results
}

func addAllRatings(q url.Values) {
	for _, rating := range []string{"safe", "suggestive", "erotica", "pornographic"} {
		q.Add("contentRating[]", rating)
	}
}
