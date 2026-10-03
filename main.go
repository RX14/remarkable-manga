// remarkable-manga daemon: watches subscriptions, sends new chapters to the reMarkable,
// and serves the subscription web UI.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func main() {
	dbPath := flag.String("db", "remarkable-manga.sqlite", "state database")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	s, err := openStore(*dbPath)
	if err != nil {
		fatal(err)
	}
	mdx := NewClient()

	go func() {
		for {
			start := time.Now()
			if err := sendRun(mdx, s); err != nil {
				slog.Error("run failed", "err", err)
			}
			slog.Info("run done", "took", time.Since(start).Round(time.Millisecond))
			time.Sleep(pollEvery)
		}
	}()

	fatal(serve(*addr, mdx, s))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
