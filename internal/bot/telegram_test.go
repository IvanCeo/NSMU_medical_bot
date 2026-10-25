package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestTelegram_RequestsAndCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		var response string
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			response = `{"ok":true,"result":{"id":99,"is_bot":true,"first_name":"Test","username":"TestBot"}}`
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if r.FormValue("chat_id") != "1" || r.FormValue("text") != "<b>literal</b>" || r.FormValue("parse_mode") != "" {
				t.Error("incorrect sendMessage payload")
			}
			response = `{"ok":true,"result":{"message_id":1}}`
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			if r.FormValue("offset") != "12" || r.FormValue("timeout") != "25" || r.FormValue("allowed_updates") != `["message","callback_query"]` {
				t.Error("incorrect polling configuration")
			}
			close(started)
			<-r.Context().Done()
			return
		default:
			t.Error("unexpected Telegram method")
		}
		if _, err := io.WriteString(w, response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := newTelegram(t.Context(), "test-only-token", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if client.Username() != "TestBot" {
		t.Fatal("bot identity not loaded")
	}
	if err := client.Send(t.Context(), 1, "<b>literal</b>"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Updates(ctx, 12); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("polling request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want request cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("polling request was not canceled")
	}
}

func TestTelegram_ErrorRedaction(t *testing.T) {
	for _, tt := range []struct {
		code int
		want error
	}{
		{401, domain.ErrTelegramUnauthorized}, {409, domain.ErrTelegramConflict}, {429, domain.ErrTelegramUnavailable},
	} {
		t.Run(fmt.Sprint(tt.code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":"private-test-token","parameters":{"retry_after":7}}`, tt.code); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			_, err := newTelegram(t.Context(), "private-test-token", server.URL+"/bot%s/%s", server.Client())
			if !errors.Is(err, tt.want) || strings.Contains(err.Error(), "private-test-token") {
				t.Fatalf("incorrect sanitized error: %v", err)
			}
			if retryDelay(err, time.Second) != 7*time.Second {
				t.Fatal("retry_after was ignored")
			}
		})
	}
}

func TestTelegram_NetworkErrorRedaction(t *testing.T) {
	client := &http.Client{Transport: failingTransport{}}
	_, err := newTelegram(t.Context(), "private-test-token", "https://telegram.invalid/bot%s/%s", client)
	if !errors.Is(err, domain.ErrTelegramUnavailable) || strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("network error exposed request: %v", err)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("request failed: %s", r.URL)
}
