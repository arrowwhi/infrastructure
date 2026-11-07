package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "123456:SECRET-token"

// fakeAPI — заглушка Bot API: handler получает имя метода и тело запроса.
func fakeAPI(t *testing.T, handler func(method string, body map[string]any) (int, string)) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasPrefix(r.URL.Path, "/bot"+testToken+"/"), r.URL.Path)

		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]

		var body map[string]any

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)

		code, resp := handler(method, body)
		w.WriteHeader(code)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultClientConfig(testToken)
	cfg.BaseURL = srv.URL
	cfg.ChatInterval = 0

	c, err := NewClient(cfg)
	require.NoError(t, err)

	return c
}

func TestSendMessage(t *testing.T) {
	c := fakeAPI(t, func(method string, body map[string]any) (int, string) {
		assert.Equal(t, "sendMessage", method)
		assert.InEpsilon(t, float64(42), body["chat_id"], 0)
		assert.Equal(t, "hi", body["text"])
		assert.NotNil(t, body["reply_markup"])

		return http.StatusOK, `{"ok":true,"result":{"message_id":7,"chat":{"id":42,"type":"private"},"date":1}}`
	})

	msg, err := c.SendMessage(context.Background(), SendMessageRequest{
		ChatID:      42,
		Text:        "hi",
		ReplyMarkup: InlineKeyboard(Row(CallbackButton("ok", "ok:1"))),
	})
	require.NoError(t, err)
	assert.Equal(t, 7, msg.MessageID)
}

func TestAPIError(t *testing.T) {
	c := fakeAPI(t, func(string, map[string]any) (int, string) {
		return http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`
	})

	err := c.EditMessageText(context.Background(), EditMessageTextRequest{ChatID: 1, MessageID: 1, Text: "x"})
	require.Error(t, err)
	assert.True(t, IsCode(err, http.StatusBadRequest))
	assert.True(t, IsMessageNotModified(err))
}

func TestRetryAfter429(t *testing.T) {
	var calls atomic.Int32

	c := fakeAPI(t, func(string, map[string]any) (int, string) {
		if calls.Add(1) == 1 {
			return http.StatusTooManyRequests, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`
		}

		return http.StatusOK, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1}}`
	})

	start := time.Now()
	_, err := c.SendMessage(context.Background(), SendMessageRequest{ChatID: 1, Text: "x"})

	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
	assert.GreaterOrEqual(t, time.Since(start), time.Second, "должен выждать retry_after")
}

func TestTokenNotLeakedInErrors(t *testing.T) {
	cfg := DefaultClientConfig(testToken)
	cfg.BaseURL = "http://127.0.0.1:1" // никто не слушает

	c, err := NewClient(cfg)
	require.NoError(t, err)

	_, err = c.GetMe(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestPerChatLimiter(t *testing.T) {
	c := fakeAPI(t, func(string, map[string]any) (int, string) {
		return http.StatusOK, `{"ok":true,"result":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1}}`
	})

	c.limiter = newLimiter(ClientConfig{GlobalRate: 1000, ChatInterval: 100 * time.Millisecond, ChatBurst: 1})

	start := time.Now()

	for range 3 {
		_, err := c.SendMessage(context.Background(), SendMessageRequest{ChatID: 5, Text: "x"})
		require.NoError(t, err)
	}

	assert.GreaterOrEqual(t, time.Since(start), 190*time.Millisecond, "сообщения в один чат разносятся по интервалу")

	start = time.Now()
	_, err := c.SendMessage(context.Background(), SendMessageRequest{ChatID: 6, Text: "x"})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 50*time.Millisecond, "другой чат не ждёт")
}

func TestUpdateHelpers(t *testing.T) {
	var u Update

	require.NoError(t, json.Unmarshal([]byte(`{"update_id":1,"callback_query":{"id":"q","data":"ok:1",
		"from":{"id":7,"first_name":"A"},"message":{"message_id":3,"chat":{"id":9,"type":"private"},"date":1}}}`), &u))

	assert.Equal(t, "callback_query", u.Type())
	assert.Equal(t, int64(9), u.Chat().ID)
	assert.Equal(t, int64(7), u.From().ID)
}
