// Package notify delivers out-of-band notices about chapters pushed to the
// tablet — currently Telegram DMs.
//
// The bot library owns the Telegram protocol; this package owns the
// credentials, the client lifecycle, the send policy (best effort: callers log
// failures and carry on), and the wording of the notice itself.
package notify

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/RX14/remarkable-manga/mangadex"
	"github.com/go-telegram/bot"
)

// Telegram sends messages to a single chat via the Telegram Bot API.
// A nil *Telegram is valid and means "not configured": Notify is a no-op.
type Telegram struct {
	bot    *bot.Bot
	chatID any // int64 for numeric chat ids, string for @channel usernames
}

// NewTelegram builds a sender from credentials: the BotFather token and the
// destination chat id (numeric, or @username). It returns (nil, nil) when
// either credential is empty, so an unconfigured deployment simply notifies
// nobody instead of erroring at every delivery.
func NewTelegram(token, chatID string) (*Telegram, error) {
	if token == "" || chatID == "" {
		return nil, nil
	}
	b, err := bot.New(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	var id any = chatID
	if n, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		id = n
	}
	return &Telegram{bot: b, chatID: id}, nil
}

// Notify sends the delivery notice for one chapter that has been pushed to the
// tablet — e.g. "Sent to tablet: Bloom Into You ch. 12 (Yuri-ism) (24 pages)".
// The wording is this side's presentation of the chapter; the tablet's own
// document naming is separate policy in the daemon.
func (t *Telegram) Notify(ctx context.Context, ch mangadex.Chapter) error {
	if t == nil {
		return nil
	}
	desc := ch.SeriesName
	if ch.Chapter != "" {
		desc += " ch. " + ch.Chapter
	}
	if len(ch.Groups) > 0 {
		desc += " (" + strings.Join(ch.Groups, ", ") + ")"
	}
	text := fmt.Sprintf("Sent to tablet: %s (%d pages)", desc, ch.Pages)
	_, err := t.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: t.chatID,
		Text:   text,
	})
	if err != nil {
		return fmt.Errorf("telegram notify: %w", err)
	}
	return nil
}
