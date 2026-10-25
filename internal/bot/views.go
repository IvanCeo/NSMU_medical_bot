package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func (r *Router) menu(ctx context.Context, id int64, text string) error {
	p, e := r.cases.Profile(ctx, id)
	if e != nil {
		return r.respondError(ctx, id, e)
	}
	buttons := [][]Button{{{Text: "Профиль", Data: "menu:profile"}, {Text: "Настройки", Data: "menu:settings"}}}
	if p.User.GroupID == nil {
		buttons = append(buttons, []Button{{Text: "Вступить в группу", Data: "menu:join"}})
	} else {
		buttons = append(buttons, []Button{{Text: "Задания", Data: "menu:tasks"}, {Text: "Предметы", Data: "menu:subjects"}})
	}
	if p.User.Role == domain.Admin {
		buttons = append(buttons, []Button{{Text: "Администрирование", Data: "menu:admin"}})
	}
	if p.User.Role == domain.Editor || p.User.Role == domain.Admin {
		text += "\n/add — новое задание; /edit — изменить; /cancel_task — отменить; /subjects manage — предметы."
	}
	return r.ui.Show(ctx, id, 0, text, buttons)
}
func (r *Router) help(ctx context.Context, id int64) error {
	text := "/start — меню\n/profile — профиль\n/join [код] — вступить в группу\n/leave — выйти с подтверждением\n/tasks — задания и сроки\n/subjects — предметы\n/settings — напоминания и тихие часы\n/cancel — отменить диалог\n\nНапоминания приходят лично. В группе публикуется только ежедневный дайджест. Время отображается в " + r.tz.String() + ".\nНе отправляй данные пациентов."
	p, e := r.cases.Profile(ctx, id)
	if e != nil {
		return r.respondError(ctx, id, e)
	}
	if p.User.Role == domain.Editor || p.User.Role == domain.Admin {
		text += "\n\n/add — мастер задания\n/edit [ID] — изменить\n/cancel_task [ID] — отменить\n/subjects manage — создать, изменить или архивировать предмет"
	}
	if p.User.Role == domain.Admin {
		text += "\n/admin — группы, приглашения, редакторы и журнал\n/tasks ID_группы [day/week/all/overdue] [all/pending/done] — задания выбранной группы\n/subjects ID_группы — её предметы\n/bind ID — привязать учебную группу из её чата (нужны права администратора чата у тебя и бота)"
	}
	text += "\n/tasks [day/week/all/overdue] [all/pending/done] — фильтры периода и выполнения"
	return r.ui.Show(ctx, id, 0, text, nil)
}
func (r *Router) showProfile(ctx context.Context, id int64) error {
	p, e := r.cases.Profile(ctx, id)
	if e != nil {
		return r.respondError(ctx, id, e)
	}
	group := "не выбрана"
	if p.Group != nil {
		group = p.Group.Title
	}
	text := fmt.Sprintf("%s\nTelegram ID: %d\nРоль: %s\nГруппа: %s\n%s", p.User.DisplayName, p.User.TelegramID, label(string(p.User.Role)), group, preferencesText(p.Preferences))
	return r.ui.Show(ctx, id, 0, text, nil)
}
func preferencesText(p domain.Preferences) string {
	state := "выключены"
	if p.RemindersEnabled {
		state = "включены"
	}
	text := "Личные напоминания: " + state
	if p.QuietHours != nil {
		text += "\nТихие часы: " + p.QuietHours.Start + "–" + p.QuietHours.End
	} else {
		text += "\nТихие часы: выключены"
	}
	return text
}
func (r *Router) showTasks(ctx context.Context, id int64, period, cursor string, subject *int64) error {
	return r.taskPages(ctx, id, domain.AssignmentFilter{Period: period, SubjectID: subject}, cursor, false)
}
func (r *Router) tasksCommand(ctx context.Context, id int64, args string) error {
	f := domain.AssignmentFilter{Period: "day", DoneFilter: "all"}
	parts := strings.Fields(args)
	if len(parts) > 0 {
		if gid, e := positiveID(parts[0]); e == nil {
			f.GroupID = gid
			parts = parts[1:]
		}
	}
	if len(parts) > 0 {
		f.Period = parts[0]
		parts = parts[1:]
	}
	if len(parts) > 0 {
		f.DoneFilter = parts[0]
		parts = parts[1:]
	}
	if len(parts) > 0 {
		return r.respondError(ctx, id, domain.ErrInvalidInput)
	}
	return r.taskPages(ctx, id, f, "", f.GroupID > 0 || f.DoneFilter != "all")
}
func (r *Router) taskPages(ctx context.Context, id int64, f domain.AssignmentFilter, cursor string, allPages bool) error {
	for {
		next, e := r.taskPage(ctx, id, f, cursor, allPages)
		if e != nil {
			return e
		}
		if !allPages || next == "" {
			return nil
		}
		cursor = next
	}
}
func (r *Router) taskPage(ctx context.Context, id int64, f domain.AssignmentFilter, cursor string, allPages bool) (string, error) {
	p, e := r.cases.Tasks(ctx, id, f, domain.PageRequest{Cursor: cursor})
	if e != nil {
		return "", r.respondError(ctx, id, e)
	}
	text := "Задания · " + label(f.Period)
	if f.DoneFilter != "" && f.DoneFilter != "all" {
		text += " · " + label(f.DoneFilter)
	}
	var rows [][]Button
	for _, a := range p.Items {
		mark := "○"
		if a.Done {
			mark = "✓"
		}
		text += fmt.Sprintf("\n%s #%d · %s · %s", mark, a.ID, a.Title, a.DeadlineAt.In(r.tz).Format("02.01 15:04"))
		rows = append(rows, []Button{{Text: fmt.Sprintf("Открыть #%d", a.ID), Data: fmt.Sprintf("view:%d", a.ID)}})
	}
	if len(p.Items) == 0 {
		text += "\nПока пусто."
	}
	if p.NextCursor != "" && !allPages {
		data := "tasks:" + f.Period + ":" + p.NextCursor
		if f.SubjectID != nil {
			data = fmt.Sprintf("subject:%d:%s", *f.SubjectID, p.NextCursor)
		}
		rows = append(rows, []Button{{Text: "Далее →", Data: data}})
	}
	if f.GroupID == 0 {
		rows = append(rows, []Button{{Text: "Сегодня", Data: "tasks:day:0"}, {Text: "7 дней", Data: "tasks:week:0"}}, []Button{{Text: "Все", Data: "tasks:all:0"}, {Text: "Просроченные", Data: "tasks:overdue:0"}})
	}
	return p.NextCursor, r.ui.Show(ctx, id, 0, text, rows)
}
func (r *Router) showSubjects(ctx context.Context, id int64, cursor string, gid int64) error {
	for {
		p, e := r.cases.Subjects(ctx, id, gid, false, domain.PageRequest{Cursor: cursor, Limit: 20})
		if e != nil {
			return r.respondError(ctx, id, e)
		}
		text := "Предметы группы"
		var rows [][]Button
		for _, a := range p.Items {
			rows = append(rows, []Button{{Text: a.Name, Data: fmt.Sprintf("subject:%d:0", a.ID)}})
		}
		if len(rows) == 0 {
			text += "\nПока пусто."
		}
		if e := r.ui.Show(ctx, id, 0, text, rows); e != nil {
			return e
		}
		if p.NextCursor == "" {
			return nil
		}
		cursor = p.NextCursor
	}
}
func (r *Router) taskText(ctx context.Context, actor int64, v domain.AssignmentView) (string, error) {
	subject := fmt.Sprintf("#%d", v.SubjectID)
	cursor := ""
	for {
		page, e := r.cases.Subjects(ctx, actor, v.GroupID, false, domain.PageRequest{Cursor: cursor, Limit: 20})
		if e != nil {
			return "", e
		}
		for _, sub := range page.Items {
			if sub.ID == v.SubjectID {
				subject = sub.Name
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	state := label(string(v.Status))
	if v.Done {
		state += " · выполнено"
	}
	text := fmt.Sprintf("#%d · %s\n%s · %s\n%s\nСрок: %s\nСтатус: %s", v.ID, subject, label(string(v.Kind)), label(string(v.Importance)), v.Title, v.DeadlineAt.In(r.tz).Format("02.01.2006 15:04"), state)
	if v.Description != "" {
		text += "\n\n" + v.Description
	}
	if v.URL != "" {
		text += "\n\n" + v.URL
	}
	if len(v.Attachments) > 0 {
		text += fmt.Sprintf("\nВложений: %d", len(v.Attachments))
	}
	return text, nil
}
func (r *Router) showTask(ctx context.Context, actor, id int64, editID int, files bool) error {
	v, e := r.cases.Task(ctx, actor, id)
	if e != nil {
		return r.respondError(ctx, actor, e)
	}
	text, e := r.taskText(ctx, actor, v)
	if e != nil {
		return e
	}
	var rows [][]Button
	if v.Status == domain.Active {
		action, title := "done", "Выполнено"
		if v.Done {
			action, title = "undo", "Снять отметку"
		}
		rows = append(rows, []Button{{Text: title, Data: fmt.Sprintf("%s:%d:%d", action, id, v.Version)}})
		if !v.Done {
			rows = append(rows, []Button{{Text: "Напомнить через 3 часа", Data: fmt.Sprintf("snooze:%d:%d:3h", id, v.Version)}})
		}
	}
	parts := splitText(text, 3500)
	if e := r.ui.Show(ctx, actor, editID, parts[0], rows); e != nil {
		return e
	}
	if editID == 0 {
		for _, part := range parts[1:] {
			if e := r.ui.Show(ctx, actor, 0, part, nil); e != nil {
				return e
			}
		}
	}
	if files {
		for _, f := range v.Attachments {
			if e := r.ui.SendFile(ctx, actor, f); e != nil {
				return e
			}
		}
	}
	return nil
}
func splitText(text string, maxUnits int) []string {
	var out []string
	var part strings.Builder
	units := 0
	for _, r := range text {
		n := utf16.RuneLen(r)
		if units+n > maxUnits {
			out = append(out, part.String())
			part.Reset()
			units = 0
		}
		part.WriteRune(r)
		units += n
	}
	if part.Len() > 0 {
		out = append(out, part.String())
	}
	if len(out) == 0 {
		return []string{" "}
	}
	return out
}
func (r *Router) showDialog(ctx context.Context, id int64, v domain.DialogView) error {
	if v.Step == "completed" {
		return r.menu(ctx, id, "Готово.")
	}
	text := prompt(v.PromptKey)
	if v.Preview != "" {
		switch v.Step {
		case "task_preview":
			var a domain.AssignmentInput
			if e := json.Unmarshal([]byte(v.Preview), &a); e != nil {
				return e
			}
			text += fmt.Sprintf("\n%s\n%s · %s\n%s\n%s\n%s\nВложений: %d", a.Title, label(string(a.Kind)), label(string(a.Importance)), a.DeadlineAt.In(r.tz).Format("02.01.2006 15:04"), a.Description, a.URL, len(a.Attachments))
		case "settings":
			var p domain.Preferences
			if e := json.Unmarshal([]byte(v.Preview), &p); e != nil {
				return e
			}
			text += "\n" + preferencesText(p)
		case "admin_group":
			var g domain.Group
			if e := json.Unmarshal([]byte(v.Preview), &g); e != nil {
				return e
			}
			state := "выключен"
			if g.DigestEnabled {
				state = "включён"
			}
			chat := "не привязан"
			if g.TelegramChatID != nil {
				chat = fmt.Sprint(*g.TelegramChatID)
			}
			text += fmt.Sprintf("\n#%d %s\nКод: %s\nДайджест: %s\nЧат: %s\nПросмотр: /tasks %d all", g.ID, g.Title, g.InviteCode, state, chat, g.ID)
		case "admin_audit":
			var entries []domain.AuditEntry
			if e := json.Unmarshal([]byte(v.Preview), &entries); e != nil {
				return e
			}
			if len(entries) == 0 {
				text += "\nПока пусто."
			}
			for _, a := range entries {
				text += fmt.Sprintf("\n%s · %s · %s #%d", a.At.In(r.tz).Format("02.01 15:04"), auditLabel(a.Action), auditLabel(a.Entity), a.EntityID)
			}
		default:
			text += "\n" + v.Preview
		}
	}
	if v.Step == "admin_digest" && v.Preview == "" {
		text += "\nСегодня дедлайнов нет."
	}
	var rows [][]Button
	for _, o := range v.Options {
		data := fmt.Sprintf("dlg:%d:%d:%s", v.ID, v.Revision, o.Value)
		if len(data) > 64 {
			return domain.ErrInvalidInput
		}
		rows = append(rows, []Button{{Text: label(o.Label), Data: data}})
	}
	parts := splitText(text, 3500)
	for i, part := range parts {
		buttons := [][]Button(nil)
		if i == len(parts)-1 {
			buttons = rows
		}
		if e := r.ui.Show(ctx, id, 0, part, buttons); e != nil {
			return e
		}
	}
	return nil
}

func auditLabel(value string) string {
	values := map[string]string{"create": "создание", "update": "изменение", "cancel": "отмена", "join": "вступление", "leave": "выход", "role": "роль", "remove": "исключение", "rotate_invite": "смена приглашения", "bind": "привязка чата", "settings": "настройки", "completion": "отметка выполнения", "group": "группа", "subject": "предмет", "assignment": "задание", "user": "участник"}
	if v, ok := values[value]; ok {
		return v
	}
	return value
}
func prompt(key string) string {
	prompts := map[string]string{
		"invite": "Введи код приглашения. /cancel — отмена.", "leave_confirm": "Подтверди выход из группы. Ожидающие напоминания будут отменены.",
		"choose_group": "Выбери учебную группу.", "admin_groups": "Группы: выбери существующую или создай новую.", "group_title": "Введи название группы (до 200 символов).", "admin_group": "Управление группой. Код ниже — приглашение; передай его участникам.",
		"member_list": "Выбери участника.", "member_action": "Действие с участником.", "member_remove_confirm": "Подтверди исключение участника. С редактора сначала нужно снять полномочия.",
		"subject_menu": "Управление предметами.", "subject_list": "Выбери предмет.", "subject_action": "Действие с предметом.", "subject_name": "Введи название предмета (до 200 символов).", "subject_teacher": "Введи преподавателя или «-», чтобы оставить поле пустым.",
		"task_id": "Введи ID задания из его карточки.", "task_subject": "Выбери предмет. Если список пуст, создай предмет через /subjects manage.", "task_kind": "Выбери вид задания.", "task_title": "Введи заголовок (1–200 символов).", "task_description": "Введи описание (до 3000 символов) или «-» без описания.", "task_deadline": "Введи будущий срок: ДД.ММ.ГГГГ ЧЧ:ММ в часовом поясе бота.", "task_importance": "Выбери важность.", "task_url": "Введи ссылку http/https или «-» без ссылки.", "task_attachments": "Отправь документы или фотографии (до 10), затем нажми «Готово». Вложений:", "task_preview": "Проверь задание и подтверди публикацию.", "task_cancel_confirm": "Подтверди отмену задания.",
		"settings": "Личные уведомления.", "quiet_start": "Начало тихих часов: ЧЧ:ММ.", "quiet_end": "Конец тихих часов: ЧЧ:ММ; можно через полночь.", "admin_digest": "Предварительный просмотр сегодняшнего дайджеста.", "admin_audit": "Журнал изменений: действие, объект и ID.",
	}
	if p, ok := prompts[key]; ok {
		return p
	}
	return "Выбери действие. /cancel — отмена."
}
func label(v string) string {
	if v == "pending" {
		return "невыполненные"
	}
	if v == "done" {
		return "выполненные"
	}
	labels := map[string]string{"student": "студент", "editor": "редактор", "admin": "администратор", "active": "активно", "cancelled": "отменено", "homework": "домашнее задание", "test": "контрольная", "exam": "экзамен", "practice": "практика", "other": "другое", "low": "низкая важность", "normal": "обычная важность", "high": "высокая важность", "day": "сегодня", "week": "7 дней", "all": "все активные", "overdue": "просроченные", "next": "Далее →", "keep": "Оставить текущее", "finish": "Готово", "clear": "Убрать вложения", "confirm": "Подтвердить", "toggle": "Включить / выключить напоминания", "quiet": "Настроить тихие часы", "quiet_off": "Выключить тихие часы", "create_subject": "Создать предмет", "edit_subject": "Изменить предмет", "rename_subject": "Изменить название", "teacher": "Изменить преподавателя", "archive": "Архивировать", "restore": "Восстановить", "create_group": "Создать группу", "rename": "Переименовать", "rotate": "Новый код приглашения", "digest_toggle": "Включить / выключить дайджест", "members": "Участники и редакторы", "subjects": "Предметы", "add": "Добавить задание", "digest_preview": "Посмотреть дайджест", "audit": "Журнал", "grant_editor": "Назначить редактором", "revoke_editor": "Снять роль редактора", "remove_member": "Исключить", "back": "Назад"}
	if s, ok := labels[v]; ok {
		return s
	}
	for from, to := range map[string]string{"(student)": "(студент)", "(editor)": "(редактор)", "(admin)": "(администратор)", "[archive]": "[архив]"} {
		v = strings.ReplaceAll(v, from, to)
	}
	return v
}
