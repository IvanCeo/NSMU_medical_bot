package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

type stubWork struct {
	repos Repositories
	err   error
}

func (w stubWork) WithinTx(ctx context.Context, fn func(Repositories) error) error {
	if w.err != nil {
		return w.err
	}
	return fn(w.repos)
}

type stubUsers struct {
	UserRepo
	user domain.User
	err  error
}

func (r stubUsers) ByTelegramID(context.Context, int64) (domain.User, error) { return r.user, r.err }
func TestApp_RepositoryFailureAndDeniedWrites(t *testing.T) {
	for _, tc := range []struct {
		name          string
		user          domain.User
		repoErr, want error
	}{
		{"storage", domain.User{}, domain.ErrStorage, domain.ErrStorage},
		{"student", domain.User{ID: 1, Role: domain.Student}, nil, domain.ErrForbidden},
		{"editor", domain.User{ID: 1, Role: domain.Editor}, nil, domain.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(stubWork{repos: Repositories{Users: stubUsers{user: tc.user, err: tc.repoErr}}}, nil, nil, Options{})
			_, e := s.CreateGroup(t.Context(), 1, "Test")
			if !errors.Is(e, tc.want) {
				t.Fatalf("got %v", e)
			}
		})
	}
	s := New(stubWork{err: domain.ErrStorage}, nil, nil, Options{})
	_, e := s.Register(t.Context(), domain.TelegramUser{TelegramID: 1})
	if !errors.Is(e, domain.ErrStorage) {
		t.Fatal(e)
	}
	_, e = s.Register(t.Context(), domain.TelegramUser{TelegramID: -1})
	if !errors.Is(e, domain.ErrInvalidUser) {
		t.Fatal(e)
	}
}
func TestParseDeadline_DSTAndInput(t *testing.T) {
	tz, e := time.LoadLocation("Europe/Berlin")
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		value string
		valid bool
	}{{"25.10.2026 02:30", false}, {"29.03.2026 02:30", false}, {"25.10.2026 03:30", true}, {"bad", false}, {"32.10.2026 12:00", false}} {
		t.Run(tc.value, func(t *testing.T) {
			_, e := ParseDeadline(tc.value, tz)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, e)
			}
		})
	}
}
func TestQuietHours_OvernightAndCalendarBoundary(t *testing.T) {
	s := New(nil, nil, nil, Options{Timezone: time.UTC})
	q := &domain.QuietHours{Start: "22:00", End: "08:00"}
	for _, tc := range []struct{ at, want string }{{"2026-10-25T23:00:00Z", "2026-10-26T08:00:00Z"}, {"2026-10-26T01:00:00Z", "2026-10-26T08:00:00Z"}, {"2026-10-26T08:00:00Z", "2026-10-26T08:00:00Z"}} {
		at, e := time.Parse(time.RFC3339, tc.at)
		if e != nil {
			t.Fatal(e)
		}
		if got := s.afterQuiet(at, q).Format(time.RFC3339); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
}
func TestAssignmentValidation_TypedInput(t *testing.T) {
	base := domain.Assignment{Kind: domain.Homework, Importance: domain.Normal, Title: "Test"}
	for _, tc := range []struct {
		name   string
		change func(*domain.Assignment)
	}{{"kind", func(a *domain.Assignment) { a.Kind = "bad" }}, {"importance", func(a *domain.Assignment) { a.Importance = "bad" }}, {"file", func(a *domain.Assignment) {
		a.Attachments = []domain.Attachment{{AttachmentInput: domain.AttachmentInput{Kind: "video", FileID: "test"}}}
	}}, {"url", func(a *domain.Assignment) { a.URL = "ftp://example.com" }}, {"empty title", func(a *domain.Assignment) { a.Title = " " }}} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.change(&a)
			if !errors.Is(validAssignment(a), domain.ErrInvalidInput) {
				t.Fatal("invalid assignment accepted")
			}
		})
	}
}
