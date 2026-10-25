package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	tg "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type callbackCases struct {
	Cases
	snoozeCalls int
	snoozeErr   error
}

func (c *callbackCases) Register(context.Context, domain.TelegramUser) (domain.User, error) {
	gid := int64(1)
	return domain.User{ID: 1, TelegramID: 1, GroupID: &gid, Role: domain.Student}, nil
}
func (c *callbackCases) Snooze(_ context.Context, actor, id, version int64, delay time.Duration) error {
	c.snoozeCalls++
	return c.snoozeErr
}
func (c *callbackCases) Task(context.Context, int64, int64) (domain.AssignmentView, error) {
	return domain.AssignmentView{Assignment: domain.Assignment{ID: 42, GroupID: 1, SubjectID: 1, Kind: domain.Homework, Title: "Test task", DeadlineAt: time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC), Status: domain.Active, Importance: domain.Normal, Version: 1}}, nil
}
func (c *callbackCases) Subjects(context.Context, int64, int64, bool, domain.PageRequest) (domain.SubjectPage, error) {
	return domain.SubjectPage{Items: []domain.Subject{{ID: 1, Name: "Test subject"}}}, nil
}

type callbackUI struct {
	recordingUI
	showErr, answerErr error
}

func (u *callbackUI) Show(ctx context.Context, chat int64, id int, text string, rows [][]Button) error {
	if e := u.recordingUI.Show(ctx, chat, id, text, rows); e != nil {
		return e
	}
	return u.showErr
}
func (u *callbackUI) Answer(ctx context.Context, id, text string) error {
	if e := u.recordingUI.Answer(ctx, id, text); e != nil {
		return e
	}
	return u.answerErr
}
func testCallback(data string) *tg.CallbackQuery {
	return &tg.CallbackQuery{ID: "test-callback", From: &tg.User{ID: 1, FirstName: "Test"}, Message: &tg.Message{MessageID: 7, Chat: &tg.Chat{ID: 1, Type: "private"}, Text: "Original card"}, Data: data}
}

func TestCallback_SnoozeConfirmsWithoutEditing(t *testing.T) {
	cases := &callbackCases{}
	ui := &callbackUI{showErr: &requestError{code: 400, kind: domain.ErrTelegramUnavailable}}
	r := &Router{ui: ui, cases: cases, tz: time.UTC, logger: discardLogger()}
	if e := r.callback(t.Context(), testCallback("snooze:42:1:3h")); e != nil {
		t.Fatal(e)
	}
	if cases.snoozeCalls != 1 {
		t.Fatal("snooze not scheduled")
	}
	if len(ui.shown) != 0 {
		t.Fatalf("snooze attempted an unchanged edit: %+v", ui.shown)
	}
	if len(ui.answers) != 1 || !strings.Contains(ui.answers[0], "3 часа") {
		t.Fatalf("confirmation=%v", ui.answers)
	}
}

func TestCallback_HandlerAndAnswerFailuresAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name            string
		handler, answer error
		events          []string
	}{
		{"handler", errors.New("private-test-content"), nil, []string{"callback_failed"}},
		{"answer", nil, domain.ErrTelegramUnavailable, []string{"callback_answer_failed"}},
		{"both", domain.ErrStorage, domain.ErrTelegramUnavailable, []string{"callback_failed", "callback_answer_failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			ui := &callbackUI{answerErr: tc.answer}
			r := &Router{ui: ui, cases: &callbackCases{snoozeErr: tc.handler}, tz: time.UTC, logger: slog.New(slog.NewJSONHandler(&output, nil))}
			err := r.callback(t.Context(), testCallback("snooze:42:1:3h"))
			for _, want := range []error{tc.handler, tc.answer} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("lost error %v: %v", want, err)
				}
			}
			for _, event := range tc.events {
				if !strings.Contains(output.String(), event) {
					t.Fatalf("missing event %s: %s", event, output.String())
				}
			}
			if strings.Contains(output.String(), "private-test-content") {
				t.Fatal("unsafe error details in logs")
			}
		})
	}
}

func TestTelegram_ShowEditNoopIsSuccessful(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		code              int
		ok                bool
	}{
		{"unchanged", "Bad Request: message is not modified: specified new message content and reply markup are exactly the same as a current content and reply markup of the message", 400, true},
		{"short unchanged", "Bad Request: message is not modified", 400, true},
		{"other bad request", "Bad Request: BUTTON_DATA_INVALID", 400, false},
		{"wrong code", "Bad Request: message is not modified", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getMe") {
					writeJSON(t, w, `{"ok":true,"result":{"id":99,"is_bot":true,"username":"TestBot"}}`)
					return
				}
				writeJSON(t, w, fmt.Sprintf(`{"ok":false,"error_code":%d,"description":%q}`, tc.code, tc.description))
			}))
			defer server.Close()
			client, e := newTelegram(t.Context(), "test-token", server.URL+"/bot%s/%s", server.Client())
			if e != nil {
				t.Fatal(e)
			}
			e = client.Show(t.Context(), 1, 7, "Test", nil)
			if (e == nil) != tc.ok {
				t.Fatalf("got %v, want success=%v", e, tc.ok)
			}
		})
	}
}

type callbackPollingUI struct{ callbackUI }

func (u *callbackPollingUI) Updates(context.Context, int) ([]tg.Update, error) {
	return []tg.Update{{UpdateID: 1, CallbackQuery: testCallback("snooze:42:1:3h")}}, nil
}

func TestCallback_FatalAnswerDoesNotExposeHandlerDetails(t *testing.T) {
	ui := &callbackPollingUI{callbackUI: callbackUI{answerErr: &requestError{code: 401, kind: domain.ErrTelegramUnauthorized}}}
	cases := &callbackCases{snoozeErr: errors.New("private-test-content")}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := NewMVP(ui, cases, time.UTC, discardLogger()).Run(ctx)
	if !errors.Is(err, domain.ErrTelegramUnauthorized) {
		t.Fatalf("not fatal: %v", err)
	}
	if strings.Contains(err.Error(), "private-test-content") {
		t.Fatal("fatal callback exposed handler details to startup logger")
	}
}

func TestTelegram_ShowExplicitlyClearsKeyboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			writeJSON(t, w, `{"ok":true,"result":{"id":99,"is_bot":true,"username":"TestBot"}}`)
			return
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		if r.FormValue("reply_markup") != `{"inline_keyboard":[]}` {
			t.Errorf("old keyboard not explicitly removed: %s", r.FormValue("reply_markup"))
		}
		if r.FormValue("disable_web_page_preview") != "true" {
			t.Error("editing enabled link previews")
		}
		writeJSON(t, w, `{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"}}}`)
	}))
	defer server.Close()
	client, e := newTelegram(t.Context(), "test-token", server.URL+"/bot%s/%s", server.Client())
	if e != nil {
		t.Fatal(e)
	}
	if e := client.Show(t.Context(), 1, 7, "Next step", nil); e != nil {
		t.Fatal(e)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, e := w.Write([]byte(value)); e != nil {
		t.Error(e)
	}
}
