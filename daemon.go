// daemon.go: the send loop and its state (one SQLite file).
//
// The rules, precisely:
//
//	for chapter in feed (EN, newest first):
//	    if publishAt older than 7d: continue
//	    if db.exists(id, version): continue
//	    fetch → wrap → upload → db.insert(id, version)
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/juruen/rmapi/api"
	_ "modernc.org/sqlite"
)

const (
	sendWindow   = 7 * 24 * time.Hour // first sends only happen within a week of publish
	chapterDelay = 5 * time.Second    // pause between chapter fetches
	pollEvery    = 6 * time.Hour
	seriesDir    = "Manga"
)

type subscription struct {
	mangaID string
	name    string
}

type store struct{ db *sql.DB }

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS subscriptions (
			manga_id TEXT PRIMARY KEY,
			name     TEXT NOT NULL,
			added_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sent_chapters (
			chapter_id TEXT NOT NULL,
			version    INTEGER NOT NULL,
			sent_at    TEXT NOT NULL,
			PRIMARY KEY (chapter_id, version))`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			return nil, fmt.Errorf("init db: %w", err)
		}
	}
	return &store{db}, nil
}

func (s *store) subscriptions() ([]subscription, error) {
	rows, err := s.db.Query(`SELECT manga_id, name FROM subscriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []subscription
	for rows.Next() {
		var sub subscription
		if err := rows.Scan(&sub.mangaID, &sub.name); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *store) sentExists(chapterID string, version int) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM sent_chapters WHERE chapter_id = ? AND version = ?`,
		chapterID, version).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *store) markSent(chapterID string, version int) error {
	_, err := s.db.Exec(
		`INSERT INTO sent_chapters (chapter_id, version, sent_at) VALUES (?, ?, ?)`,
		chapterID, version, time.Now().UTC().Format(time.RFC3339))
	return err
}

// sendRun is one pass of the send loop over all subscriptions.
func sendRun(mdx *Client, s *store) error {
	subs, err := s.subscriptions()
	if err != nil {
		return fmt.Errorf("load subscriptions: %w", err)
	}

	rm, err := connect()
	if err != nil {
		return err
	}
	parent, err := ensureDir(rm, seriesDir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	for _, sub := range subs {
		chapters, err := mdx.FetchFeed(ctx, sub.mangaID)
		if err != nil {
			slog.Error("feed failed", "series", sub.name, "err", err)
			continue
		}
		for _, ch := range chapters {
			ch.SeriesName = sub.name
			if time.Since(ch.PublishedAt) > sendWindow {
				continue
			}
			seen, err := s.sentExists(ch.ID, ch.Version)
			if err != nil {
				return fmt.Errorf("sent lookup: %w", err)
			}
			if seen {
				continue
			}
			time.Sleep(chapterDelay)
			if err := sendChapter(ctx, mdx, rm, parent, ch); err != nil {
				slog.Error("send failed, will retry next run", "chapter", ch.ID, "err", err)
				continue
			}
			if err := s.markSent(ch.ID, ch.Version); err != nil {
				return fmt.Errorf("mark sent: %w", err)
			}
		}
	}
	return nil
}

// sendChapter runs the per-chapter pipeline: pages → PDF → upload.
func sendChapter(ctx context.Context, mdx *Client, rm api.ApiCtx, parent string, ch Chapter) error {
	urls, err := mdx.FetchPageURLs(ctx, ch.ID)
	if err != nil {
		return err
	}
	pages, err := mdx.DownloadPages(ctx, ch.ID, urls)
	if err != nil {
		return err
	}
	pdf, err := wrapPDF(ctx, pages)
	if err != nil {
		return err
	}
	_, err = uploadPDF(rm, parent, documentName(ch), pdf)
	return err
}

func (s *store) addSubscription(mangaID, name string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO subscriptions (manga_id, name, added_at) VALUES (?, ?, ?)`,
		mangaID, name, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *store) removeSubscription(mangaID string) error {
	_, err := s.db.Exec(`DELETE FROM subscriptions WHERE manga_id = ?`, mangaID)
	return err
}
