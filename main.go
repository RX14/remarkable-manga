// remarkable-manga daemon: watches subscriptions, sends new chapters to the reMarkable,
// and serves the subscription web UI.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/RX14/remarkable-manga/mangadex"
	"github.com/RX14/remarkable-manga/notify"
)

func main() {
	dbPath := flag.String("db", "remarkable-manga.sqlite", "state database")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()})))

	s, err := openStore(*dbPath)
	if err != nil {
		fatal(err)
	}
	mdx := mangadex.NewClient()
	nt, err := notify.NewTelegram(os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID"))
	if err != nil {
		fatal(err)
	}
	if nt == nil {
		slog.Info("telegram notifications off: TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID not set")
	}

	go func() {
		for {
			if err := sendRun(mdx, s, nt); err != nil {
				slog.Error("run failed", "err", err)
			}
			time.Sleep(pollEvery)
		}
	}()

	fatal(serve(*addr, mdx, s))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// logLevel reads REMARKABLE_MANGA_LOG_LEVEL (debug|info|warn|error; default
// info). slog has no environment configuration of its own.
func logLevel() slog.Level {
	switch strings.ToLower(os.Getenv("REMARKABLE_MANGA_LOG_LEVEL")) {
	case "", "info":
		return slog.LevelInfo
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	fmt.Fprintf(os.Stderr, "REMARKABLE_MANGA_LOG_LEVEL=%q: unknown level, using info\n", os.Getenv("REMARKABLE_MANGA_LOG_LEVEL"))
	return slog.LevelInfo
}
