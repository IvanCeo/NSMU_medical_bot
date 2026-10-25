package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Client interface {
	Username() string
	Updates(ctx context.Context, offset int) ([]tgbotapi.Update, error)
	Send(ctx context.Context, chatID int64, text string) error
}

type EchoService interface {
	Reply(ctx context.Context, user domain.TelegramUser, text string) (string, error)
}

type Bot struct {
	client Client
	echo   EchoService
	logger *slog.Logger
	router *Router
}

func New(client Client, echo EchoService, logger *slog.Logger) *Bot {
	return &Bot{client: client, echo: echo, logger: logger}
}

func (b *Bot) Run(ctx context.Context) error {
	offset, delay := 0, time.Second
	for ctx.Err() == nil {
		updates, err := b.client.Updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if fatal(err) {
				return fmt.Errorf("poll Telegram: %w", err)
			}
			b.logger.ErrorContext(ctx, "Telegram polling failed", "event", "poll_failed", "err", err)
			if !pause(ctx, retryDelay(err, delay)) {
				return nil
			}
			delay = min(delay*2, 30*time.Second)
			continue
		}
		delay = time.Second
		for _, update := range updates {
			if ctx.Err() != nil {
				return nil
			}
			if err := b.handle(ctx, update); err != nil {
				if fatal(err) {
					return err
				}
				b.logger.ErrorContext(ctx, "Telegram reply failed", "event", "reply_failed", "err", err)
			}
			offset = max(offset, update.UpdateID+1)
		}
	}
	return nil
}

func (b *Bot) handle(ctx context.Context, update tgbotapi.Update) error {
	if b.router != nil {
		return b.router.Handle(ctx, update)
	}
	message := update.Message
	if message == nil || message.Chat == nil || !message.Chat.IsPrivate() ||
		message.From == nil || message.From.IsBot || message.Text == "" {
		return nil
	}
	user := domain.TelegramUser{
		TelegramID:  message.From.ID,
		DisplayName: strings.TrimSpace(message.From.FirstName + " " + message.From.LastName),
	}
	text := message.Text
	if words := strings.Fields(text); len(words) > 0 &&
		(words[0] == "/start" || strings.EqualFold(words[0], "/start@"+b.client.Username())) {
		text = "/start"
	}
	reply, err := b.echo.Reply(ctx, user, text)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		b.logger.ErrorContext(ctx, "Message handling failed", "event", "handler_failed")
		reply = "Временная ошибка. Попробуй позже."
	}
	return b.sendReply(ctx, message.Chat.ID, reply)
}

func (b *Bot) sendReply(ctx context.Context, chatID int64, text string) error {
	for attempt := 0; ; attempt++ {
		err := b.client.Send(ctx, chatID, text)
		if err == nil || ctx.Err() != nil || fatal(err) || attempt == 2 {
			return err
		}
		var response *requestError
		if errors.As(err, &response) && response.code != 429 && response.code < 500 {
			return err
		}
		if !pause(ctx, retryDelay(err, time.Duration(attempt+1)*time.Second)) {
			return nil
		}
	}
}

func fatal(err error) bool {
	return errors.Is(err, domain.ErrTelegramUnauthorized) || errors.Is(err, domain.ErrTelegramConflict)
}

func retryDelay(err error, fallback time.Duration) time.Duration {
	var response *requestError
	if errors.As(err, &response) && response.retryAfter > fallback {
		return response.retryAfter
	}
	return fallback
}

func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
