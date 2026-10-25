package bot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanCeo/NSMU_medical_bot/internal/repository/sqlite"
	"github.com/IvanCeo/NSMU_medical_bot/internal/service"
)

func TestBot_EchoWithSQLiteAndFakeTelegram(t *testing.T) {
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	users := sqlite.NewUsers(db)
	if err := service.BootstrapAdmins(t.Context(), users, []int64{1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	replies := make(chan string, 1)
	var delivered atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		var response string
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			response = `{"ok":true,"result":{"id":99,"is_bot":true,"first_name":"Test","username":"TestBot"}}`
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			if delivered.Swap(true) {
				<-r.Context().Done()
				return
			}
			response = `{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"from":{"id":1,"first_name":"Test"},"chat":{"id":1,"type":"private"},"text":"<b>echo</b>"}}]}`
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			replies <- r.FormValue("text")
			response = `{"ok":true,"result":{"message_id":2}}`
		default:
			t.Error("unexpected Telegram method")
		}
		if _, err := io.WriteString(w, response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := newTelegram(ctx, "test-only-token", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- New(client, service.NewEcho(users, time.Now), discardLogger()).Run(ctx) }()
	select {
	case text := <-replies:
		if text != "<b>echo</b>" {
			t.Fatalf("echo = %q", text)
		}
	case <-ctx.Done():
		t.Fatal("echo was not received")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bot did not stop")
	}
}
