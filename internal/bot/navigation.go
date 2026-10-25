package bot

import (
	"context"
	"errors"
	"strings"
)

type navigationKey struct{}

// A target belongs to one update only; no navigation state survives the request.
type navigationTarget struct {
	chatID    int64
	messageID int
	used      bool
}

func navigationContext(ctx context.Context, chat int64, messageID int) context.Context {
	return context.WithValue(ctx, navigationKey{}, &navigationTarget{chatID: chat, messageID: messageID})
}

func (r *Router) show(ctx context.Context, chat int64, messageID int, text string, rows [][]Button) error {
	text = navigationText(chat, text)
	target, _ := ctx.Value(navigationKey{}).(*navigationTarget)
	if messageID == 0 && target != nil && !target.used && target.chatID == chat {
		messageID = target.messageID
	}
	err := r.ui.Show(ctx, chat, messageID, text, rows)
	if messageID > 0 && cannotEdit(err) {
		err = r.ui.Show(ctx, chat, 0, text, rows)
	}
	if err == nil && target != nil && target.chatID == chat {
		target.used = true
	}
	return err
}

func navigationText(chat int64, text string) string {
	if chat > 0 && !strings.Contains(text, "/start — главное меню") {
		text += "\n\n/start — главное меню"
	}
	return text
}

func cannotEdit(err error) bool {
	var request *requestError
	return errors.As(err, &request) && (request.reason == "message_not_found" || request.reason == "message_not_editable")
}
