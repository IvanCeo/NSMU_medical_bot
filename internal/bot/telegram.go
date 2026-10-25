package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Telegram struct {
	api  *tgbotapi.BotAPI
	http *http.Client
	rate *rateLimiter
}

func NewTelegram(ctx context.Context, token string) (*Telegram, error) {
	return newTelegram(ctx, token, tgbotapi.APIEndpoint, &http.Client{Timeout: 35 * time.Second})
}

func newTelegram(ctx context.Context, token, endpoint string, client *http.Client) (*Telegram, error) {
	api, err := tgbotapi.NewBotAPIWithClient(token, endpoint, contextClient{ctx, client})
	if err != nil {
		return nil, safeError(ctx, err)
	}
	return &Telegram{api: api, http: client, rate: newRateLimiter()}, nil
}

func (t *Telegram) Username() string {
	return t.api.Self.UserName
}

func (t *Telegram) scoped(ctx context.Context) *tgbotapi.BotAPI {
	api := *t.api
	api.Client = contextClient{ctx, t.http}
	return &api
}

func (t *Telegram) Updates(ctx context.Context, offset int) ([]tgbotapi.Update, error) {
	request := tgbotapi.NewUpdate(offset)
	request.Timeout = 25
	request.AllowedUpdates = []string{"message", "callback_query"}
	updates, err := t.scoped(ctx).GetUpdates(request)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	return updates, nil
}

func (t *Telegram) Send(ctx context.Context, chatID int64, text string) error {
	if err := t.rate.Wait(ctx, chatID, chatID < 0); err != nil {
		return err
	}
	_, err := t.scoped(ctx).Send(tgbotapi.NewMessage(chatID, text))
	if err != nil {
		return safeError(ctx, err)
	}
	return nil
}

type contextClient struct {
	ctx    context.Context
	client *http.Client
}

func (c contextClient) Do(request *http.Request) (*http.Response, error) {
	return c.client.Do(request.WithContext(c.ctx))
}

type requestError struct {
	code       int
	retryAfter time.Duration
	kind       error
}

func (e *requestError) Error() string {
	return fmt.Sprintf("Telegram request failed (code %d): %v", e.code, e.kind)
}

func (e *requestError) Unwrap() error {
	return e.kind
}

func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("Telegram request canceled: %w", ctx.Err())
	}
	kind := domain.ErrTelegramUnavailable
	var response *tgbotapi.Error
	if errors.As(err, &response) {
		switch response.Code {
		case http.StatusUnauthorized:
			kind = domain.ErrTelegramUnauthorized
		case http.StatusConflict:
			kind = domain.ErrTelegramConflict
		}
		return &requestError{response.Code, time.Duration(response.RetryAfter) * time.Second, kind}
	}
	// HTTP errors include the request URL, which contains the token.
	return fmt.Errorf("Telegram request failed: %w", kind)
}
