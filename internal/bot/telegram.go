package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	reason     string
}

func (e *requestError) Error() string {
	return fmt.Sprintf("Telegram request failed (code %d, reason %s): %v", e.code, e.reason, e.kind)
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
		return &requestError{code: response.Code, retryAfter: time.Duration(response.RetryAfter) * time.Second, kind: kind, reason: telegramReason(response)}
	}
	// HTTP errors include the request URL, which contains the token.
	return fmt.Errorf("Telegram request failed: %w", kind)
}

func telegramReason(response *tgbotapi.Error) string {
	message := strings.TrimPrefix(response.Message, "Bad Request: ")
	if response.Code == http.StatusBadRequest {
		if message == "message is not modified" || strings.HasPrefix(message, "message is not modified: ") {
			return "message_not_modified"
		}
		for description, reason := range map[string]string{"message to edit not found": "message_not_found", "message can't be edited": "message_not_editable", "BUTTON_DATA_INVALID": "invalid_button_data"} {
			if message == description {
				return reason
			}
		}
	}
	return "request_failed"
}
