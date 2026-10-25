package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	tg "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type navigationCases struct {
	callbackCases
	tasksErr error
	longCard bool
}

func (c *navigationCases) Profile(ctx context.Context, id int64) (domain.Profile, error) {
	u, e := c.Register(ctx, domain.TelegramUser{TelegramID: id})
	return domain.Profile{User: u, Group: &domain.Group{ID: 1, Title: "Test group"}, Preferences: domain.Preferences{RemindersEnabled: true}}, e
}
func (c *navigationCases) Tasks(ctx context.Context, id int64, f domain.AssignmentFilter, p domain.PageRequest) (domain.AssignmentPage, error) {
	v, e := c.Task(ctx, id, 42)
	if e != nil {
		return domain.AssignmentPage{}, e
	}
	return domain.AssignmentPage{Items: []domain.AssignmentView{v}}, c.tasksErr
}
func (c *navigationCases) Task(ctx context.Context, actor, id int64) (domain.AssignmentView, error) {
	v, e := c.callbackCases.Task(ctx, actor, id)
	if c.longCard {
		v.Description = strings.Repeat("🧪", 1800)
		v.URL = "https://example.com/" + strings.Repeat("x", 1500)
	}
	return v, e
}
func (c *navigationCases) Start(context.Context, int64, domain.DialogKind, int64) (domain.DialogView, error) {
	return domain.DialogView{ID: 11, Revision: 1, Step: "settings", PromptKey: "settings", Options: []domain.Option{{Label: "toggle", Value: "toggle"}}}, nil
}
func (c *navigationCases) Apply(context.Context, int64, int64, int64, domain.DialogInput) (domain.DialogView, error) {
	return domain.DialogView{ID: 11, Revision: 2, Step: "settings", PromptKey: "settings", Options: []domain.Option{{Label: "toggle", Value: "toggle"}}}, nil
}

func TestNavigation_CallbackReusesSourceMessage(t *testing.T) {
	for _, data := range []string{"menu:tasks", "tasks:all:0", "view:42", "subject:1:0", "menu:profile", "menu:settings", "dlg:11:1:toggle"} {
		t.Run(data, func(t *testing.T) {
			ui := &recordingUI{}
			r := &Router{ui: ui, cases: &navigationCases{}, tz: time.UTC, logger: discardLogger()}
			if e := r.Handle(t.Context(), tg.Update{CallbackQuery: testCallback(data)}); e != nil {
				t.Fatal(e)
			}
			if len(ui.shown) != 1 || ui.last().id != 7 {
				t.Fatalf("new message instead of source edit: %+v", ui.shown)
			}
			if !strings.Contains(ui.last().text, "/start — главное меню") {
				t.Fatal("missing navigation hint")
			}
		})
	}
}

func TestNavigation_ReplacesDialogKeyboard(t *testing.T) {
	ui := &recordingUI{}
	r := &Router{ui: ui, cases: &navigationCases{}, tz: time.UTC, logger: discardLogger()}
	if e := r.Handle(t.Context(), tg.Update{CallbackQuery: testCallback("dlg:11:1:toggle")}); e != nil {
		t.Fatal(e)
	}
	if ui.last().id != 7 || ui.last().buttons[0][0].Data != "dlg:11:2:toggle" {
		t.Fatal("old revision keyboard retained")
	}
	if !strings.Contains(ui.last().text, "/cancel") {
		t.Fatal("missing dialog escape")
	}
}

func TestNavigation_LongCardContinuesInNewMessages(t *testing.T) {
	ui := &recordingUI{}
	r := &Router{ui: ui, cases: &navigationCases{longCard: true}, tz: time.UTC, logger: discardLogger()}
	if e := r.Handle(t.Context(), tg.Update{CallbackQuery: testCallback("view:42")}); e != nil {
		t.Fatal(e)
	}
	if len(ui.shown) < 2 || ui.shown[0].id != 7 {
		t.Fatal("first part did not edit source")
	}
	for _, shown := range ui.shown[1:] {
		if shown.id != 0 {
			t.Fatal("continuation overwrote source")
		}
	}
	found := false
	for _, row := range ui.shown[0].buttons {
		for _, button := range row {
			if button.Data == "subject:1:0" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing subject return button")
	}
}

func TestNavigation_RejectedActionKeepsSourceCard(t *testing.T) {
	ui := &recordingUI{}
	r := &Router{ui: ui, cases: &navigationCases{tasksErr: domain.ErrNotInGroup}, tz: time.UTC, logger: discardLogger()}
	if e := r.Handle(t.Context(), tg.Update{CallbackQuery: testCallback("menu:tasks")}); e != nil {
		t.Fatal(e)
	}
	if len(ui.shown) != 1 || ui.last().id != 0 {
		t.Fatal("error replaced working source card")
	}
}

type failedEditUI struct {
	recordingUI
	failure error
}

func (u *failedEditUI) Show(ctx context.Context, chat int64, id int, text string, rows [][]Button) error {
	if e := u.recordingUI.Show(ctx, chat, id, text, rows); e != nil {
		return e
	}
	if id > 0 {
		return u.failure
	}
	return nil
}

func TestNavigation_FallsBackOnlyForUnavailableSource(t *testing.T) {
	for _, tc := range []struct {
		reason   string
		fallback bool
	}{{"message_not_found", true}, {"message_not_editable", true}, {"invalid_button_data", false}} {
		t.Run(tc.reason, func(t *testing.T) {
			ui := &failedEditUI{failure: &requestError{code: 400, kind: domain.ErrTelegramUnavailable, reason: tc.reason}}
			r := &Router{ui: ui, cases: &navigationCases{}, tz: time.UTC, logger: discardLogger()}
			err := r.Handle(t.Context(), tg.Update{CallbackQuery: testCallback("menu:tasks")})
			if (err == nil) != tc.fallback {
				t.Fatalf("err=%v fallback=%v", err, tc.fallback)
			}
			if tc.fallback {
				if len(ui.shown) != 2 || ui.shown[0].id != 7 || ui.shown[1].id != 0 {
					t.Fatal("did not create a usable replacement")
				}
			} else if len(ui.shown) != 1 {
				t.Fatal("retried an unrelated bad request")
			}
		})
	}
}
