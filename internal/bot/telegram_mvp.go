package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
	tg "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func keyboard(rows [][]Button) tg.InlineKeyboardMarkup {
	out := tg.InlineKeyboardMarkup{InlineKeyboard: make([][]tg.InlineKeyboardButton, 0, len(rows))}
	for _, row := range rows {
		var buttons []tg.InlineKeyboardButton
		for _, b := range row {
			if b.URL != "" {
				buttons = append(buttons, tg.NewInlineKeyboardButtonURL(b.Text, b.URL))
			} else {
				buttons = append(buttons, tg.NewInlineKeyboardButtonData(b.Text, b.Data))
			}
		}
		out.InlineKeyboard = append(out.InlineKeyboard, buttons)
	}
	return out
}
func (t *Telegram) Show(ctx context.Context, chat int64, messageID int, text string, buttons [][]Button) error {
	if e := t.rate.Wait(ctx, chat, chat < 0); e != nil {
		return e
	}
	var request tg.Chattable
	if messageID > 0 {
		v := tg.NewEditMessageText(chat, messageID, text)
		v.DisableWebPagePreview = true
		k := keyboard(buttons)
		v.ReplyMarkup = &k
		request = v
	} else {
		v := tg.NewMessage(chat, text)
		v.DisableWebPagePreview = true
		if len(buttons) > 0 {
			v.ReplyMarkup = keyboard(buttons)
		}
		request = v
	}
	_, e := t.scoped(ctx).Send(request)
	if e != nil {
		safe := safeError(ctx, e)
		var failure *requestError
		if messageID > 0 && errors.As(safe, &failure) && failure.reason == "message_not_modified" {
			return nil
		}
		return safe
	}
	return nil
}
func (t *Telegram) Answer(ctx context.Context, id, text string) error {
	_, e := t.scoped(ctx).Request(tg.NewCallback(id, text))
	if e != nil {
		return safeError(ctx, e)
	}
	return nil
}
func (t *Telegram) SendFile(ctx context.Context, chat int64, f domain.Attachment) error {
	if e := t.rate.Wait(ctx, chat, false); e != nil {
		return e
	}
	var request tg.Chattable
	if f.Kind == domain.Photo {
		request = tg.NewPhoto(chat, tg.FileID(f.FileID))
	} else {
		request = tg.NewDocument(chat, tg.FileID(f.FileID))
	}
	_, e := t.scoped(ctx).Send(request)
	if e != nil {
		return safeError(ctx, e)
	}
	return nil
}
func (t *Telegram) CheckBinding(ctx context.Context, chat, actor int64) (domain.ChatPermissions, error) {
	var out domain.ChatPermissions
	api := t.scoped(ctx)
	info, e := api.GetChat(tg.ChatInfoConfig{ChatConfig: tg.ChatConfig{ChatID: chat}})
	if e != nil {
		return out, safeError(ctx, e)
	}
	if !info.IsGroup() && !info.IsSuperGroup() {
		return out, domain.ErrForbidden
	}
	member, e := api.GetChatMember(tg.GetChatMemberConfig{ChatConfigWithUser: tg.ChatConfigWithUser{ChatID: chat, UserID: actor}})
	if e != nil {
		return out, safeError(ctx, e)
	}
	self, e := api.GetChatMember(tg.GetChatMemberConfig{ChatConfigWithUser: tg.ChatConfigWithUser{ChatID: chat, UserID: t.api.Self.ID}})
	if e != nil {
		return out, safeError(ctx, e)
	}
	out.ActorIsAdministrator = member.IsAdministrator() || member.IsCreator()
	out.BotIsAdministrator = self.IsAdministrator() || self.IsCreator()
	out.BotCanSend = out.BotIsAdministrator
	return out, nil
}
func (t *Telegram) SendNotification(ctx context.Context, m domain.Outgoing) (domain.MessageReceipt, error) {
	if e := t.rate.Wait(ctx, m.ChatID, m.Kind == "group"); e != nil {
		return domain.MessageReceipt{}, e
	}
	prefix := "Напоминание\n"
	if m.Kind == "group" {
		prefix = "Дедлайны на "
	}
	request := tg.NewMessage(m.ChatID, prefix+m.Text)
	request.DisableWebPagePreview = true
	var buttons [][]Button
	for _, b := range m.Buttons {
		title := b.Label
		if title == "open_task" {
			title = "Открыть задание"
			if m.Kind == "group" {
				title = fmt.Sprintf("Задание #%d", b.AssignmentID)
			}
		}
		button := Button{Text: title, Data: fmt.Sprintf("view:%d", b.AssignmentID)}
		if m.Kind == "group" {
			button.Data = ""
			button.URL = fmt.Sprintf("https://t.me/%s?start=task_%d", t.Username(), b.AssignmentID)
		}
		buttons = append(buttons, []Button{button})
	}
	if len(buttons) > 0 {
		request.ReplyMarkup = keyboard(buttons)
	}
	message, e := t.scoped(ctx).Send(request)
	if e != nil {
		safe := safeError(ctx, e)
		var r *requestError
		if errors.As(safe, &r) {
			return domain.MessageReceipt{}, &domain.DeliveryFailure{Code: strconv.Itoa(r.code), Permanent: r.code >= 400 && r.code < 500 && r.code != 429, RetryAfter: r.retryAfter}
		}
		return domain.MessageReceipt{}, safe
	}
	return domain.MessageReceipt{ChatID: m.ChatID, MessageID: message.MessageID}, nil
}
