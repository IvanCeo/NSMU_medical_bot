package bot

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
	tg "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Button struct{ Text, Data, URL string }
type UI interface {
	Client
	Show(context.Context, int64, int, string, [][]Button) error
	Answer(context.Context, string, string) error
	SendFile(context.Context, int64, domain.Attachment) error
}
type Cases interface {
	service.UserCases
	service.GroupCases
	service.LearningCases
	service.DialogCases
}
type Router struct {
	ui     UI
	cases  Cases
	tz     *time.Location
	logger *slog.Logger
}

func NewMVP(client UI, cases Cases, tz *time.Location, logger *slog.Logger) *Bot {
	b := New(client, nil, logger)
	b.router = &Router{client, cases, tz, logger}
	return b
}

func (r *Router) Handle(ctx context.Context, update tg.Update) (err error) {
	if update.CallbackQuery != nil {
		return r.callback(ctx, update.CallbackQuery)
	}
	defer func() {
		actor := int64(0)
		if update.Message != nil && update.Message.From != nil {
			actor = update.Message.From.ID
		}
		r.logFailure(ctx, "message_failed", "message", actor, err)
	}()
	m := update.Message
	if m == nil || m.Chat == nil || m.From == nil || m.From.IsBot {
		return nil
	}
	if !m.Chat.IsPrivate() {
		return r.groupMessage(ctx, m)
	}
	if m.Chat.ID != m.From.ID {
		return nil
	}
	actor := m.From.ID
	if _, e := r.cases.Register(ctx, domain.TelegramUser{TelegramID: actor, DisplayName: strings.TrimSpace(m.From.FirstName + " " + m.From.LastName)}); e != nil {
		return r.respondError(ctx, m.Chat.ID, e)
	}
	if strings.HasPrefix(m.Text, "/") {
		command, args, ok := r.command(m.Text)
		if !ok {
			return nil
		}
		return r.runCommand(ctx, actor, command, args)
	}
	d, e := r.cases.Current(ctx, actor)
	if e != nil {
		return r.respondError(ctx, actor, e)
	}
	if d == nil {
		return r.menu(ctx, actor, "Выбери действие в меню. /help — справка.")
	}
	in := domain.DialogInput{}
	if m.Document != nil {
		in.Attachment = &domain.AttachmentInput{Kind: domain.Document, FileID: m.Document.FileID, FileName: m.Document.FileName, MIME: m.Document.MimeType}
	} else if len(m.Photo) > 0 {
		in.Attachment = &domain.AttachmentInput{Kind: domain.Photo, FileID: m.Photo[len(m.Photo)-1].FileID}
	} else if m.Text != "" {
		in.Text = &m.Text
	} else {
		return r.show(ctx, actor, 0, "Нужен текст, документ или фотография. /cancel — отмена.", nil)
	}
	view, e := r.cases.Apply(ctx, actor, d.ID, d.Revision, in)
	if e != nil {
		return r.respondError(ctx, actor, e)
	}
	return r.showDialog(ctx, actor, view)
}

func (r *Router) command(text string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	if i := strings.IndexByte(command, '@'); i >= 0 {
		if !strings.EqualFold(command[i+1:], r.ui.Username()) {
			return "", "", false
		}
		command = command[:i]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return command, args, true
}

func (r *Router) groupMessage(ctx context.Context, m *tg.Message) error {
	command, args, ok := r.command(m.Text)
	if !ok || command != "bind" {
		return nil
	}
	if !m.Chat.IsGroup() && !m.Chat.IsSuperGroup() {
		return nil
	}
	gid, e := positiveID(args)
	if e == nil {
		e = r.cases.BindChat(ctx, m.From.ID, gid, m.Chat.ID)
	}
	if e != nil {
		return r.respondError(ctx, m.Chat.ID, e)
	}
	return r.ui.Show(ctx, m.Chat.ID, 0, "Чат привязан. Здесь будет ежедневный дайджест; личные напоминания приходят участникам в личку.", nil)
}

func (r *Router) runCommand(ctx context.Context, actor int64, command, args string) error {
	var e error
	switch command {
	case "start":
		return r.commandStart(ctx, actor, args)
	case "help":
		return r.help(ctx, actor)
	case "profile":
		return r.showProfile(ctx, actor)
	case "join":
		return r.commandJoin(ctx, actor, args)
	case "leave":
		return r.startDialog(ctx, actor, domain.DialogLeave, 0)
	case "tasks":
		return r.tasksCommand(ctx, actor, args)
	case "subjects":
		return r.commandSubjects(ctx, actor, args)
	case "settings":
		return r.startDialog(ctx, actor, domain.DialogSettings, 0)
	case "admin":
		return r.startDialog(ctx, actor, domain.DialogAdmin, 0)
	case "add":
		return r.startDialog(ctx, actor, domain.DialogAdd, 0)
	case "edit", "cancel_task":
		return r.commandEdit(ctx, actor, command, args)
	case "cancel":
		e = r.cases.Cancel(ctx, actor)
		if e == nil {
			return r.menu(ctx, actor, "Диалог отменён.")
		}
	default:
		return r.menu(ctx, actor, "Неизвестная команда. /help — справка.")
	}
	return r.respondError(ctx, actor, e)
}

func (r *Router) startDialog(ctx context.Context, id int64, kind domain.DialogKind, entity int64) error {
	v, e := r.cases.Start(ctx, id, kind, entity)
	if e != nil {
		return r.respondError(ctx, id, e)
	}
	return r.showDialog(ctx, id, v)
}

func (r *Router) respondError(ctx context.Context, chat int64, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.logFailure(ctx, "handler_failed", "operation", chat, err)
	return r.ui.Show(ctx, chat, 0, navigationText(chat, errorText(err)), nil)
}

func errorText(err error) string {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		return "Нет доступа к этому действию."
	case errors.Is(err, domain.ErrNotInGroup):
		return "Сначала вступи в группу: /join."
	case errors.Is(err, domain.ErrConflict):
		return "Данные изменились или действие конфликтует с текущим состоянием. Обнови карточку; для смены группы сначала выйди из прежней."
	case errors.Is(err, domain.ErrExpiredDialog):
		return "Диалог истёк. Начни заново."
	case errors.Is(err, domain.ErrNotFound):
		return "Запись не найдена. Проверь ID или код приглашения."
	case errors.Is(err, domain.ErrInvalidInput):
		return "Проверь ввод и попробуй ещё раз. /cancel — отмена."
	default:
		return "Временная ошибка. Попробуй позже."
	}
}

func positiveID(v string) (int64, error) {
	id, e := strconv.ParseInt(v, 10, 64)
	if e != nil || id <= 0 {
		return 0, domain.ErrInvalidInput
	}
	return id, nil
}

func (r *Router) callback(ctx context.Context, q *tg.CallbackQuery) (err error) {
	notice := ""
	defer func() {
		text := notice
		if err != nil {
			text = errorText(err)
		}
		actor := int64(0)
		if q.From != nil {
			actor = q.From.ID
		}
		action := callbackAction(q.Data)
		r.logFailure(ctx, "callback_failed", action, actor, err)
		answerErr := r.ui.Answer(ctx, q.ID, text)
		r.logFailure(ctx, "callback_answer_failed", action, actor, answerErr)
		if expectedError(err) {
			err = nil
		}
		err = errors.Join(err, answerErr)
	}()
	if q.From == nil || q.From.IsBot || q.Message == nil || q.Message.Chat == nil || !q.Message.Chat.IsPrivate() || q.Message.Chat.ID != q.From.ID {
		return domain.ErrForbidden
	}
	if len(q.Data) > 64 {
		return domain.ErrInvalidInput
	}
	actor := q.From.ID
	if _, e := r.cases.Register(ctx, domain.TelegramUser{TelegramID: actor, DisplayName: strings.TrimSpace(q.From.FirstName + " " + q.From.LastName)}); e != nil {
		return e
	}
	ctx = navigationContext(ctx, q.Message.Chat.ID, q.Message.MessageID)
	p := strings.Split(q.Data, ":")
	switch p[0] {
	case "menu":
		return r.callbackMenu(ctx, actor, p)
	case "tasks":
		if len(p) != 3 {
			return domain.ErrInvalidInput
		}
		return r.showTasks(ctx, actor, p[1], p[2], nil)
	case "subject":
		return r.callbackSubject(ctx, actor, p)
	case "view":
		return r.callbackView(ctx, actor, p)
	case "done", "undo", "snooze":
		notice, err = r.callbackDone(ctx, q, actor, p)
		return err
	case "dlg":
		return r.callbackDlg(ctx, actor, p)
	default:
		return domain.ErrInvalidInput
	}
}

func (r *Router) commandStart(ctx context.Context, actor int64, args string) error {
	var e error
	if args == "" {
		if e := r.cases.Cancel(ctx, actor); e != nil {
			return r.respondError(ctx, actor, e)
		}
	}
	if strings.HasPrefix(args, "join_") {
		_, e = r.cases.JoinGroup(ctx, actor, strings.TrimPrefix(args, "join_"))
		if e != nil {
			return r.respondError(ctx, actor, e)
		}
	} else if strings.HasPrefix(args, "task_") {
		id, e := positiveID(strings.TrimPrefix(args, "task_"))
		if e != nil {
			return r.respondError(ctx, actor, e)
		}
		return r.showTask(ctx, actor, id, 0, true)
	}
	return r.menu(ctx, actor, "Привет! Здесь задания, дедлайны и напоминания твоей группы.")
}

func (r *Router) commandEdit(ctx context.Context, actor int64, command, args string) error {
	var e error
	id := int64(0)
	if args != "" {
		id, e = positiveID(args)
		if e != nil {
			return r.respondError(ctx, actor, e)
		}
	}
	kind := domain.DialogEdit
	if command == "cancel_task" {
		kind = domain.DialogCancelTask
	}
	return r.startDialog(ctx, actor, kind, id)
}

func (r *Router) callbackMenu(ctx context.Context, actor int64, p []string) (err error) {
	if len(p) != 2 {
		return domain.ErrInvalidInput
	}
	switch p[1] {
	case "tasks":
		return r.showTasks(ctx, actor, "day", "", nil)
	case "subjects":
		return r.showSubjects(ctx, actor, "", 0)
	case "profile":
		return r.showProfile(ctx, actor)
	case "settings":
		return r.startDialog(ctx, actor, domain.DialogSettings, 0)
	case "admin":
		return r.startDialog(ctx, actor, domain.DialogAdmin, 0)
	case "join":
		return r.startDialog(ctx, actor, domain.DialogJoin, 0)
	default:
		return domain.ErrInvalidInput
	}

}

func (r *Router) callbackDone(ctx context.Context, q *tg.CallbackQuery, actor int64, p []string) (string, error) {
	if (p[0] == "snooze" && (len(p) != 4 || p[3] != "3h")) || (p[0] != "snooze" && len(p) != 3) {
		return "", domain.ErrInvalidInput
	}
	id, e := positiveID(p[1])
	if e != nil {
		return "", e
	}
	v, e := positiveID(p[2])
	if e != nil {
		return "", e
	}
	if p[0] == "snooze" {
		if e := r.cases.Snooze(ctx, actor, id, v, 3*time.Hour); e != nil {
			return "", e
		}
		return "Напомню через 3 часа; тихие часы учитываются.", nil
	} else {
		if _, e := r.cases.SetComplete(ctx, actor, id, v, p[0] == "done"); e != nil {
			return "", e
		}
	}
	return "", r.showTask(ctx, actor, id, q.Message.MessageID, false)
}

func (r *Router) callbackDlg(ctx context.Context, actor int64, p []string) (err error) {
	if len(p) != 4 || len(p[3]) > 24 {
		return domain.ErrInvalidInput
	}
	id, e := positiveID(p[1])
	if e != nil {
		return e
	}
	v, e := positiveID(p[2])
	if e != nil {
		return e
	}
	view, e := r.cases.Apply(ctx, actor, id, v, domain.DialogInput{Selection: &p[3]})
	if e != nil {
		return e
	}
	return r.showDialog(ctx, actor, view)
}

func (r *Router) commandJoin(ctx context.Context, actor int64, args string) error {
	var e error
	if args != "" {
		_, e = r.cases.JoinGroup(ctx, actor, args)
		if e == nil {
			return r.showProfile(ctx, actor)
		}
	} else {
		return r.startDialog(ctx, actor, domain.DialogJoin, 0)
	}
	return r.respondError(ctx,
		actor, e)
}

func (r *Router) commandSubjects(ctx context.Context, actor int64, args string) error {
	var e error
	if args == "manage" {
		return r.startDialog(ctx, actor, domain.DialogSubjects, 0)
	}
	gid := int64(0)
	if args != "" {
		gid, e = positiveID(args)
		if e != nil {
			return r.respondError(ctx, actor, e)
		}
	}
	return r.showSubjects(ctx, actor, "", gid)
}

func (r *Router) callbackSubject(ctx context.Context, actor int64, p []string) (err error) {
	if len(p) != 3 {
		return domain.ErrInvalidInput
	}
	id, e := positiveID(p[1])
	if e != nil {
		return e
	}
	return r.showTasks(ctx, actor, "all", p[2], &id)
}

func (r *Router) callbackView(ctx context.Context, actor int64, p []string) (err error) {
	if len(p) != 2 {
		return domain.ErrInvalidInput
	}
	id, e := positiveID(p[1])
	if e != nil {
		return e
	}
	return r.showTask(ctx, actor, id, 0, true)
}
