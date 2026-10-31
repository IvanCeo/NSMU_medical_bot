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

func TestTelegram_MVPRequestsAndSafeDeliveryErrors(t *testing.T) {
	code := 0
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		methods = append(methods, method)
		reply := `{"ok":true,"result":{"message_id":7,"chat":{"id":1,"type":"private"}}}`
		switch method {
		case "getMe":
			reply = `{"ok":true,"result":{"id":99,"is_bot":true,"username":"TestBot"}}`
		case "getChat":
			reply = `{"ok":true,"result":{"id":-100,"type":"supergroup"}}`
		case "getChatMember":
			reply = `{"ok":true,"result":{"status":"administrator","user":{"id":1}}}`
		case "answerCallbackQuery":
			reply = `{"ok":true,"result":true}`
		case "sendMessage":
			if code != 0 {
				reply = fmt.Sprintf(`{"ok":false,"error_code":%d,"description":"secret-token","parameters":{"retry_after":12}}`, code)
			}
			if r.FormValue("chat_id") == "-100" && !strings.Contains(r.FormValue("reply_markup"), "start=task_42") {
				t.Error("digest deep link missing")
			}
			if strings.Contains(r.FormValue("text"), "Creation topic") {
				if !strings.HasPrefix(r.FormValue("text"), "Новое задание\n") || !strings.Contains(r.FormValue("reply_markup"), "Creation topic") || !strings.Contains(r.FormValue("reply_markup"), "view:42") {
					t.Error("creation notice prefix, topic or callback missing")
				}
			}
		}
		if _, e := io.WriteString(w, reply); e != nil {
			t.Error(e)
		}
	}))
	defer server.Close()
	client, e := newTelegram(t.Context(), "test-token", server.URL+"/bot%s/%s", server.Client())
	if e != nil {
		t.Fatal(e)
	}
	reset := func() { client.rate = newRateLimiter() }
	if e := client.Show(t.Context(), 1, 0, "Test", [][]Button{{{Text: "Task", Data: "view:42"}}}); e != nil {
		t.Fatal(e)
	}
	reset()
	if e := client.Show(t.Context(), 1, 7, "Changed", [][]Button{{{Text: "Task", Data: "view:42"}}}); e != nil {
		t.Fatal(e)
	}
	reset()
	if e := client.Answer(t.Context(), "callback", "Готово"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []domain.AttachmentKind{domain.Document, domain.Photo} {
		if e := client.SendFile(t.Context(), 1, domain.Attachment{AttachmentInput: domain.AttachmentInput{Kind: kind, FileID: "test-file"}}); e != nil {
			t.Fatal(e)
		}
		reset()
	}
	p, e := client.CheckBinding(t.Context(), -100, 1)
	if e != nil || !p.BotIsAdministrator || !p.ActorIsAdministrator {
		t.Fatalf("permissions=%+v err=%v", p, e)
	}
	receipt, e := client.SendNotification(t.Context(), domain.Outgoing{ChatID: -100, Kind: "group", Text: "Digest", Buttons: []domain.OutgoingButton{{Label: "Task", AssignmentID: 42}}})
	if e != nil || receipt.MessageID != 7 {
		t.Fatal(e)
	}
	reset()
	_, e = client.SendNotification(t.Context(), domain.Outgoing{ChatID: 1, Kind: "private", EventKind: domain.AssignmentCreatedDelivery, Text: "Creation topic", Buttons: []domain.OutgoingButton{{Label: "Creation topic", AssignmentID: 42}}})
	if e != nil {
		t.Fatal(e)
	}
	reset()
	for _, value := range []int{403, 429, 500} {
		code = value
		_, e := client.SendNotification(t.Context(), domain.Outgoing{ChatID: 1, Kind: "private", Text: "Test"})
		var f *domain.DeliveryFailure
		if !errors.As(e, &f) || f.Code != fmt.Sprint(value) || f.Permanent != (value == 403) || f.RetryAfter != 12*time.Second || strings.Contains(e.Error(), "secret") {
			t.Fatalf("failure=%+v err=%v", f, e)
		}
		reset()
	}
	if len(methods) < 12 {
		t.Fatal("missing adapter requests")
	}
}
func TestRateLimiter_SharedGlobalAndChatSpacing(t *testing.T) {
	r := newRateLimiter()
	ctx := t.Context()
	if e := r.Wait(ctx, 1, false); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if e := r.Wait(ctx, 2, false); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Fatal("global limiter did not wait")
	}
	deadline, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if e := r.Wait(deadline, 1, false); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("chat spacing not cancelable: %v", e)
	}
	r.mu.Lock()
	gap := time.Until(r.chats[1])
	r.mu.Unlock()
	if gap < 800*time.Millisecond {
		t.Fatal("private interval too short")
	}
	if e := r.Wait(ctx, -100, true); e != nil {
		t.Fatal(e)
	}
	r.mu.Lock()
	groupGap := time.Until(r.chats[-100])
	r.mu.Unlock()
	if groupGap < 3*time.Second {
		t.Fatal("group interval too short")
	}
}
