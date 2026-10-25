package bot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/repository/sqlite"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
	tg "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type shown struct {
	chat    int64
	id      int
	text    string
	buttons [][]Button
}
type recordingUI struct {
	shown   []shown
	answers []string
	files   []domain.Attachment
}

func (u *recordingUI) Username() string                                  { return "TestBot" }
func (u *recordingUI) Updates(context.Context, int) ([]tg.Update, error) { return nil, nil }
func (u *recordingUI) Send(context.Context, int64, string) error         { return nil }
func (u *recordingUI) Show(_ context.Context, chat int64, id int, text string, buttons [][]Button) error {
	u.shown = append(u.shown, shown{chat, id, text, buttons})
	return nil
}
func (u *recordingUI) Answer(_ context.Context, id, text string) error {
	u.answers = append(u.answers, text)
	return nil
}
func (u *recordingUI) SendFile(_ context.Context, chat int64, f domain.Attachment) error {
	u.files = append(u.files, f)
	return nil
}
func (u *recordingUI) CheckBinding(context.Context, int64, int64) (domain.ChatPermissions, error) {
	return domain.ChatPermissions{ActorIsAdministrator: true, BotIsAdministrator: true, BotCanSend: true}, nil
}
func (u *recordingUI) SendNotification(context.Context, domain.Outgoing) (domain.MessageReceipt, error) {
	return domain.MessageReceipt{}, nil
}
func (u *recordingUI) last() shown { return u.shown[len(u.shown)-1] }
func TestMVP_TelegramFullWorkflow(t *testing.T) {
	ctx := t.Context()
	db, e := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	}()
	if e := sqlite.Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	if e := service.BootstrapAdmins(ctx, sqlite.NewUsers(db), []int64{1}, now); e != nil {
		t.Fatal(e)
	}
	ui := &recordingUI{}
	app := service.New(sqlite.NewStore(db), ui, ui, service.Options{Now: func() time.Time { return now }, Timezone: time.UTC, ReminderOffsets: []time.Duration{3 * time.Hour}})
	b := NewMVP(ui, app, time.UTC, discardLogger())
	message := func(actor int64, text string) {
		t.Helper()
		e := b.handle(ctx, tg.Update{Message: &tg.Message{MessageID: 100, From: &tg.User{ID: actor, FirstName: fmt.Sprintf("Test %d", actor)}, Chat: &tg.Chat{ID: actor, Type: "private"}, Text: text}})
		if e != nil {
			t.Fatal(e)
		}
	}
	callback := func(actor int64, data string) {
		t.Helper()
		e := b.handle(ctx, tg.Update{CallbackQuery: &tg.CallbackQuery{ID: "test-callback", From: &tg.User{ID: actor, FirstName: "Test"}, Message: &tg.Message{MessageID: 100, Chat: &tg.Chat{ID: actor, Type: "private"}}, Data: data}})
		if e != nil {
			t.Fatal(e)
		}
	}
	choose := func(actor int64, value string) {
		t.Helper()
		for _, row := range ui.last().buttons {
			for _, button := range row {
				if strings.HasSuffix(button.Data, ":"+value) {
					callback(actor, button.Data)
					return
				}
			}
		}
		t.Fatalf("no choice %s in %v", value, ui.last())
	}
	message(1, "/start")
	message(1, "/admin")
	choose(1, "create")
	message(1, "Test cohort")
	choose(1, "subjects")
	choose(1, "create_subject")
	message(1, "Anatomy")
	message(1, "-")
	groups, e := app.Groups(ctx, 1, domain.PageRequest{})
	if e != nil {
		t.Fatal(e)
	}
	g := groups.Items[0]
	message(2, "/start join_"+g.InviteCode)
	message(3, "/join "+g.InviteCode)
	p, e := app.Profile(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	message(1, "/admin")
	choose(1, fmt.Sprint(g.ID))
	choose(1, "members")
	choose(1, fmt.Sprint(p.User.ID))
	choose(1, "grant_editor")
	message(2, "/add")
	subjects, e := app.Subjects(ctx, 2, 0, false, domain.PageRequest{})
	if e != nil {
		t.Fatal(e)
	}
	choose(2, fmt.Sprint(subjects.Items[0].ID))
	choose(2, "homework")
	message(2, "Learn bones")
	message(2, "Chapter 1")
	message(2, now.Add(48*time.Hour).Format("02.01.2006 15:04"))
	choose(2, "normal")
	message(2, "-")
	e = b.handle(ctx, tg.Update{Message: &tg.Message{From: &tg.User{ID: 2}, Chat: &tg.Chat{ID: 2, Type: "private"}, Document: &tg.Document{FileID: "test-file", FileName: "test.pdf"}}})
	if e != nil {
		t.Fatal(e)
	}
	choose(2, "finish")
	choose(2, "confirm")
	message(3, "/tasks")
	callback(3, "tasks:all:0")
	choose(3, "1")
	if len(ui.files) != 1 {
		t.Fatal("task attachment was not delivered")
	}
	callback(3, "done:1:1")
	if ui.last().id != 100 || !strings.Contains(ui.last().text, "выполнено") {
		t.Fatal("completion did not edit current card")
	}
	callback(3, "undo:1:1")
	callback(3, "snooze:1:1:3h")
	message(3, "/profile")
	if !strings.Contains(ui.last().text, "Test cohort") {
		t.Fatal("profile missing group")
	}
	message(3, "/settings")
	choose(3, "quiet")
	message(3, "22:00")
	message(3, "08:00")
	choose(3, "quiet_off")
	message(3, "/cancel")
	message(3, "/help")
	message(3, "/leave")
	choose(3, "confirm")
	message(3, "/join")
	message(3, g.InviteCode)
	message(2, "/edit 1")
	choose(2, "keep")
	choose(2, "test")
	message(2, "Learn updated bones")
	choose(2, "keep")
	choose(2, "keep")
	choose(2, "high")
	choose(2, "keep")
	choose(2, "finish")
	choose(2, "confirm")
	callback(3, "done:1:1")
	if !strings.Contains(ui.answers[len(ui.answers)-1], "изменились") {
		t.Fatal("stale callback was not answered")
	}
	message(2, "/cancel_task 1")
	choose(2, "confirm")
	message(2, "/subjects manage")
	message(2, "/cancel")
	message(4, "/start")
	callback(4, "view:1")
	if strings.Contains(ui.last().text, "Learn updated bones") {
		t.Fatal("foreign task leaked")
	}
	callback(4, "malformed")
	if ui.answers[len(ui.answers)-1] == "" {
		t.Fatal("malformed callback not acknowledged with error")
	}
	message(3, "random text")
	if !strings.Contains(ui.last().text, "Выбери действие") {
		t.Fatal("navigation hint missing")
	}
	e = b.handle(ctx, tg.Update{Message: &tg.Message{From: &tg.User{ID: 1}, Chat: &tg.Chat{ID: -100, Type: "supergroup"}, Text: fmt.Sprintf("/bind %d", g.ID)}})
	if e != nil {
		t.Fatal(e)
	}
	if ui.last().chat != -100 {
		t.Fatal("binding response not in group")
	}
	before := len(ui.shown)
	message(3, "/start@AnotherBot")
	if len(ui.shown) != before {
		t.Fatal("handled another bot command")
	}
}
func TestMVP_CallbackAndTextLimits(t *testing.T) {
	ui := &recordingUI{}
	r := &Router{ui: ui}
	for _, q := range []*tg.CallbackQuery{{ID: "inline"}, {ID: "group", From: &tg.User{ID: 1}, Message: &tg.Message{Chat: &tg.Chat{ID: -100, Type: "group"}}}, {ID: "foreign", From: &tg.User{ID: 1}, Message: &tg.Message{Chat: &tg.Chat{ID: 2, Type: "private"}}}, {ID: "oversized", From: &tg.User{ID: 1}, Message: &tg.Message{Chat: &tg.Chat{ID: 1, Type: "private"}}, Data: strings.Repeat("x", 65)}} {
		if e := r.callback(t.Context(), q); e != nil {
			t.Fatal(e)
		}
	}
	if len(ui.answers) != 4 {
		t.Fatal("callback not acknowledged")
	}
	text := strings.Repeat("🧪", 5000)
	parts := splitText(text, 3500)
	if strings.Join(parts, "") != text || len(parts) != 3 {
		t.Fatal("split lost content")
	}
}
