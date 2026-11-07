package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/arrowwhi/infrastructure/telegram/api"
)

const testToken = "1:test"

// fakeTelegram — заглушка Bot API: запоминает вызовы и отдаёт очередь обновлений getUpdates.
type fakeTelegram struct {
	srv     *httptest.Server
	updates chan []api.Update
	calls   []call
	mu      sync.Mutex
}

type call struct {
	body   map[string]any
	method string
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()

	f := &fakeTelegram{updates: make(chan []api.Update, 10)}

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]

		raw, _ := io.ReadAll(r.Body)

		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		f.mu.Lock()
		f.calls = append(f.calls, call{method: method, body: body})
		f.mu.Unlock()

		var result any = true

		switch method {
		case "getMe":
			result = api.User{ID: 1, IsBot: true, FirstName: "Test", Username: "test_bot"}
		case "sendMessage":
			result = api.Message{MessageID: 1, Chat: api.Chat{ID: 1, Type: "private"}}
		case "getUpdates":
			select {
			case u := <-f.updates:
				result = u
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
				result = []api.Update{}
			}
		}

		resp, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		_, _ = w.Write(resp)
	}))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeTelegram) sent(method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []map[string]any

	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c.body)
		}
	}

	return out
}

func newTestBot(t *testing.T, f *fakeTelegram, opts ...Option) *Bot {
	t.Helper()

	cfg := DefaultConfig(testToken)
	cfg.API.BaseURL = f.srv.URL
	cfg.API.ChatInterval = 0
	cfg.PollTimeout = time.Second
	cfg.HandlerTimeout = 5 * time.Second

	b, err := New(cfg, opts...)
	require.NoError(t, err)

	b.username = "test_bot"

	return b
}

func message(chatID int64, text string) api.Update {
	return api.Update{
		UpdateID: chatID,
		Message: &api.Message{
			MessageID: 1,
			From:      &api.User{ID: chatID},
			Chat:      api.Chat{ID: chatID, Type: "private"},
			Text:      text,
		},
	}
}

func callback(chatID int64, data string) api.Update {
	return api.Update{
		UpdateID: chatID,
		CallbackQuery: &api.CallbackQuery{
			ID:      "cb1",
			From:    api.User{ID: chatID},
			Data:    data,
			Message: &api.Message{MessageID: 5, Chat: api.Chat{ID: chatID, Type: "private"}},
		},
	}
}

func TestRouting(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)

	var got []string

	b.Command("start", "Начать", func(c *Context) error {
		got = append(got, "start:"+c.Args())

		return nil
	})
	b.Callback("rate", func(c *Context) error {
		got = append(got, "rate:"+strings.Join(c.CallbackArgs(), ","))

		return nil
	})
	b.Callback("rate:fast", func(*Context) error {
		got = append(got, "rate-fast")

		return nil
	})
	b.Text(func(c *Context) error {
		got = append(got, "text:"+c.Text())

		return nil
	})

	ctx := context.Background()

	b.HandleUpdate(ctx, message(1, "/start hello"))
	b.HandleUpdate(ctx, message(1, "/START@test_bot"))
	b.HandleUpdate(ctx, message(1, "/start@other_bot")) // чужая команда → обычный текст
	b.HandleUpdate(ctx, message(1, "Дюна"))
	b.HandleUpdate(ctx, callback(1, MustCallbackData("rate", "42", "8")))
	b.HandleUpdate(ctx, callback(1, "rate:fast"))
	b.HandleUpdate(ctx, callback(1, "unknown"))
	b.HandleUpdate(ctx, message(1, "/unknown"))

	assert.Equal(t, []string{
		"start:hello",
		"start:",
		"text:/start@other_bot",
		"text:Дюна",
		"rate:42,8",
		"rate-fast",
	}, got)

	// На каждое нажатие (в том числе необработанное) бот ответил, чтобы убрать «часики».
	assert.Len(t, f.sent("answerCallbackQuery"), 3)
}

func TestDialogState(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)

	type draft struct {
		Title string `json:"title"`
	}

	var saved string

	b.Command("review", "Оставить отзыв", func(c *Context) error {
		if err := c.SetState("awaiting_title", nil); err != nil {
			return err
		}

		_, err := c.Reply("Какой фильм?")

		return err
	})
	b.OnState("awaiting_title", func(c *Context) error {
		return c.SetState("awaiting_text", draft{Title: c.Text()})
	})
	b.OnState("awaiting_text", func(c *Context) error {
		s, _, err := c.State()
		if err != nil {
			return err
		}

		var d draft
		if err = s.Decode(&d); err != nil {
			return err
		}

		saved = d.Title + ": " + c.Text()

		return c.ClearState()
	})
	b.Text(func(*Context) error {
		saved = "plain text"

		return nil
	})

	ctx := context.Background()

	b.HandleUpdate(ctx, message(7, "/review"))
	b.HandleUpdate(ctx, message(7, "Дюна"))
	b.HandleUpdate(ctx, message(8, "Другой чат")) // состояние у другого чата своё
	assert.Equal(t, "plain text", saved)

	b.HandleUpdate(ctx, message(7, "Отлично"))
	assert.Equal(t, "Дюна: Отлично", saved)

	b.HandleUpdate(ctx, message(7, "после диалога"))
	assert.Equal(t, "plain text", saved, "после ClearState сообщения снова идут в Text")

	require.Len(t, f.sent("sendMessage"), 1)
	assert.Equal(t, "Какой фильм?", f.sent("sendMessage")[0]["text"])
}

func TestPanicAndErrorHandler(t *testing.T) {
	f := newFakeTelegram(t)

	var reported []error

	b := newTestBot(t, f, WithErrorHandler(func(_ *Context, err error) {
		reported = append(reported, err)
	}))

	b.Command("panic", "", func(*Context) error { panic("boom") })
	b.Command("fail", "", func(*Context) error { return errors.New("db down") })

	b.HandleUpdate(context.Background(), message(1, "/panic"))
	b.HandleUpdate(context.Background(), message(1, "/fail"))

	require.Len(t, reported, 2)
	assert.Contains(t, reported[0].Error(), "panic: boom")
	assert.EqualError(t, reported[1], "db down")
}

func TestMiddlewareOrder(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)

	var trace []string

	mw := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(c *Context) error {
				trace = append(trace, name)

				return next(c)
			}
		}
	}

	b.Use(mw("first"), mw("second"))
	b.Command("x", "", func(c *Context) error {
		trace = append(trace, "handler:"+c.Route())

		return nil
	})

	b.HandleUpdate(context.Background(), message(1, "/x"))
	assert.Equal(t, []string{"first", "second", "handler:command:x"}, trace)
}

func TestDispatcherKeepsPerChatOrder(t *testing.T) {
	var (
		mu  sync.Mutex
		got = map[int64][]int64{}
	)

	d := newDispatcher(4, 8, func(u api.Update) {
		time.Sleep(time.Millisecond)

		mu.Lock()
		got[u.Message.Chat.ID] = append(got[u.Message.Chat.ID], u.UpdateID)
		mu.Unlock()
	})
	d.start()

	for i := int64(0); i < 50; i++ {
		for _, chat := range []int64{1, 2, -100} {
			u := message(chat, "x")
			u.UpdateID = i
			require.NoError(t, d.enqueue(context.Background(), u))
		}
	}

	d.stop()

	for chat, ids := range got {
		require.Len(t, ids, 50, "chat %d", chat)

		for i, id := range ids {
			assert.Equal(t, int64(i), id, "chat %d: обновления одного чата обрабатываются по порядку", chat)
		}
	}

	require.ErrorIs(t, d.enqueue(context.Background(), message(1, "x")), errStopped)
}

func TestPollingEndToEnd(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)

	handled := make(chan string, 2)

	b.Command("start", "Начать", func(c *Context) error {
		handled <- c.Text()

		return nil
	})

	f.updates <- []api.Update{message(1, "/start"), message(2, "/start")}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- b.Run(ctx) }()

	for range 2 {
		select {
		case <-handled:
		case <-time.After(3 * time.Second):
			t.Fatal("updates were not handled")
		}
	}

	// Второй getUpdates подтверждает первые обновления через offset.
	require.Eventually(t, func() bool { return len(f.sent("getUpdates")) >= 2 }, 3*time.Second, 10*time.Millisecond)
	assert.InEpsilon(t, float64(3), f.sent("getUpdates")[1]["offset"], 0)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}

	require.Len(t, f.sent("setMyCommands"), 1, "меню команд публикуется при старте")
}

func TestWebhookHandler(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)
	b.cfg.Webhook.Secret = "s3cret"

	handled := make(chan struct{}, 1)

	b.Text(func(*Context) error {
		handled <- struct{}{}

		return nil
	})

	b.dispatcher.start()
	defer b.dispatcher.stop()

	body, _ := json.Marshal(message(1, "hi"))

	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "без секрета — 401")

	req = httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(string(body)))
	req.Header.Set(secretHeader, "s3cret")

	rec = httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook update was not handled")
	}
}

func TestCallbackDataLimit(t *testing.T) {
	_, err := CallbackData("p", strings.Repeat("x", 70))
	require.Error(t, err)

	data, err := CallbackData("rate", "42", "8")
	require.NoError(t, err)
	assert.Equal(t, "rate:42:8", data)
}

func TestWebhookModeRequiresSecret(t *testing.T) {
	f := newFakeTelegram(t)
	b := newTestBot(t, f)
	b.cfg.Mode = ModeWebhook

	err := b.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TELEGRAM_WEBHOOK_SECRET")
}
