package bot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestHandle_MessageKinds(t *testing.T) {
	for _, tt := range []struct {
		name, text, chatType        string
		senderBot, noSender, edited bool
		wantCall                    bool
		wantText                    string
	}{
		{"echo", "text <b>literal</b>", "private", false, false, false, true, "text <b>literal</b>"},
		{"start", "/start", "private", false, false, false, true, "/start"},
		{"addressed start", "/start@TestBot payload", "private", false, false, false, true, "/start"},
		{"attachment", "", "private", false, false, false, false, ""},
		{"group", "text", "supergroup", false, false, false, false, ""},
		{"bot sender", "text", "private", true, false, false, false, ""},
		{"no sender", "text", "private", false, true, false, false, ""},
		{"edited", "text", "private", false, false, true, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message := &tgbotapi.Message{Text: tt.text, Chat: &tgbotapi.Chat{ID: 1, Type: tt.chatType}, From: &tgbotapi.User{ID: 1, FirstName: "Test", LastName: "User", IsBot: tt.senderBot}}
			if tt.noSender {
				message.From = nil
			}
			update := tgbotapi.Update{Message: message}
			if tt.edited {
				update.Message = nil
				update.EditedMessage = message
			}
			called, sent := false, false
			client := &fakeClient{send: func(ctx context.Context, id int64, text string) error {
				sent = true
				if id != 1 || text != "service-reply" {
					t.Error("incorrect reply")
				}
				return nil
			}}
			echo := replyFunc(func(ctx context.Context, user domain.TelegramUser, text string) (string, error) {
				called = true
				if text != tt.wantText || user.TelegramID != 1 || user.DisplayName != "Test User" {
					t.Error("incorrect service input")
				}
				return "service-reply", nil
			})
			if err := New(client, echo, discardLogger()).handle(t.Context(), update); err != nil {
				t.Fatal(err)
			}
			if called != tt.wantCall || sent != tt.wantCall {
				t.Fatal("incorrect handling of message kind")
			}
		})
	}
}

func TestHandle_StorageFailure(t *testing.T) {
	var output bytes.Buffer
	client := &fakeClient{send: func(ctx context.Context, id int64, text string) error {
		if text != "Временная ошибка. Попробуй позже." {
			t.Fatal("raw storage error was sent to user")
		}
		return nil
	}}
	echo := replyFunc(func(context.Context, domain.TelegramUser, string) (string, error) {
		return "", errors.New("private-test-content")
	})
	update := tgbotapi.Update{Message: &tgbotapi.Message{Text: "private-test-content", Chat: &tgbotapi.Chat{Type: "private"}, From: &tgbotapi.User{ID: 1}}}
	if err := New(client, echo, slog.New(slog.NewTextHandler(&output, nil))).handle(t.Context(), update); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "private-test-content") {
		t.Fatal("logs exposed message contents")
	}
}

func TestRun_AdvancesOffsetAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	client := &fakeClient{updates: func(ctx context.Context, offset int) ([]tgbotapi.Update, error) {
		calls++
		if calls == 1 {
			if offset != 0 {
				t.Error("unexpected initial offset")
			}
			return []tgbotapi.Update{{UpdateID: 10}, {UpdateID: 11}}, nil
		}
		if offset != 12 {
			t.Errorf("offset = %d, want 12", offset)
		}
		cancel()
		return nil, ctx.Err()
	}}
	if err := New(client, nil, discardLogger()).Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRun_PollingErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		canceled bool
	}{
		{"unauthorized", domain.ErrTelegramUnauthorized, false},
		{"conflict", domain.ErrTelegramConflict, false},
		{"cancel retry wait", domain.ErrTelegramUnavailable, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			client := &fakeClient{updates: func(context.Context, int) ([]tgbotapi.Update, error) { return nil, tt.err }}
			err := New(client, nil, discardLogger()).Run(ctx)
			if tt.canceled && err != nil {
				t.Fatal(err)
			}
			if !tt.canceled && !errors.Is(err, tt.err) {
				t.Fatalf("want fatal error, got %v", err)
			}
		})
	}
}

type fakeClient struct {
	updates func(context.Context, int) ([]tgbotapi.Update, error)
	send    func(context.Context, int64, string) error
}

func (*fakeClient) Username() string { return "TestBot" }
func (c *fakeClient) Updates(ctx context.Context, offset int) ([]tgbotapi.Update, error) {
	return c.updates(ctx, offset)
}
func (c *fakeClient) Send(ctx context.Context, id int64, text string) error {
	return c.send(ctx, id, text)
}

type replyFunc func(context.Context, domain.TelegramUser, string) (string, error)

func (f replyFunc) Reply(ctx context.Context, user domain.TelegramUser, text string) (string, error) {
	return f(ctx, user, text)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
