package service

import (
	"context"
	"fmt"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type Echo struct {
	users echoUsers
	now   func() time.Time
}

type echoUsers interface {
	UpsertTelegramUser(context.Context, domain.TelegramUser, time.Time) error
}

func NewEcho(users echoUsers, now func() time.Time) *Echo {
	return &Echo{users: users, now: now}
}

func (s *Echo) Reply(ctx context.Context, user domain.TelegramUser, text string) (string, error) {
	if user.TelegramID <= 0 {
		return "", fmt.Errorf("telegram ID must be positive: %w", domain.ErrInvalidUser)
	}
	if err := s.users.UpsertTelegramUser(ctx, user, s.now().UTC()); err != nil {
		return "", fmt.Errorf("save echo sender: %w", err)
	}
	if text == "/start" {
		return "Привет! Пока я работаю в режиме эхо. Отправь мне текст.", nil
	}
	return text, nil
}

func BootstrapAdmins(ctx context.Context, repo AdminRepo, ids []int64, at time.Time) error {
	if len(ids) == 0 {
		return fmt.Errorf("admin list must not be empty: %w", domain.ErrInvalidUser)
	}
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("admin Telegram ID must be positive: %w", domain.ErrInvalidUser)
		}
	}
	if err := repo.SyncAdmins(ctx, ids, at.UTC()); err != nil {
		return fmt.Errorf("bootstrap admins: %w", err)
	}
	return nil
}
