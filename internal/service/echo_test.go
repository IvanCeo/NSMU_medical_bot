package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestEcho_Reply(t *testing.T) {
	storageError := errors.New("test storage failure")
	for _, tt := range []struct {
		name, text, want     string
		id                   int64
		repoError, wantError error
	}{
		{"plain text", "текст <b>без форматирования</b>", "текст <b>без форматирования</b>", 1, nil, nil},
		{"start", "/start", "Привет! Пока я работаю в режиме эхо. Отправь мне текст.", 1, nil, nil},
		{"invalid user", "text", "", 0, nil, domain.ErrInvalidUser},
		{"storage failure", "text", "", 1, storageError, storageError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.FixedZone("test", 3*3600))
			called := false
			repo := userRepoFunc(func(ctx context.Context, user domain.TelegramUser, createdAt time.Time) error {
				called = true
				if user.TelegramID != tt.id || user.DisplayName != "Test User" || createdAt.Location() != time.UTC || !createdAt.Equal(at) {
					t.Error("incorrect sender or timestamp passed to repository")
				}
				return tt.repoError
			})
			text, err := NewEcho(repo, func() time.Time { return at }).Reply(t.Context(), domain.TelegramUser{TelegramID: tt.id, DisplayName: "Test User"}, tt.text)
			if text != tt.want || !errors.Is(err, tt.wantError) {
				t.Fatalf("reply=%q err=%v", text, err)
			}
			if called != (tt.id > 0) {
				t.Fatal("incorrect repository call count")
			}
		})
	}
}

func TestBootstrapAdmins_ValidationAndErrors(t *testing.T) {
	storageError := errors.New("test storage failure")
	for _, tt := range []struct {
		name                 string
		ids                  []int64
		repoError, wantError error
	}{
		{"valid", []int64{1, 2}, nil, nil},
		{"empty", nil, nil, domain.ErrInvalidUser},
		{"invalid ID", []int64{-1}, nil, domain.ErrInvalidUser},
		{"storage failure", []int64{1}, storageError, storageError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			repo := adminRepoFunc(func(ctx context.Context, ids []int64, at time.Time) error {
				called = true
				if at.Location() != time.UTC {
					t.Error("admin timestamp is not UTC")
				}
				return tt.repoError
			})
			err := BootstrapAdmins(t.Context(), repo, tt.ids, time.Now())
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("bootstrap: %v", err)
			}
			if called == errors.Is(tt.wantError, domain.ErrInvalidUser) {
				t.Fatal("invalid admin list reached repository")
			}
		})
	}
}

type userRepoFunc func(context.Context, domain.TelegramUser, time.Time) error

func (f userRepoFunc) UpsertTelegramUser(ctx context.Context, user domain.TelegramUser, at time.Time) error {
	return f(ctx, user, at)
}

type adminRepoFunc func(context.Context, []int64, time.Time) error

func (f adminRepoFunc) SyncAdmins(ctx context.Context, ids []int64, at time.Time) error {
	return f(ctx, ids, at)
}
