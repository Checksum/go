package sse

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type closeNotifyingRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func newCloseNotifyingRecorder() *closeNotifyingRecorder {
	return &closeNotifyingRecorder{
		httptest.NewRecorder(),
		make(chan bool, 1),
	}
}

func (c *closeNotifyingRecorder) close() {
	c.closed <- true
}

func (c *closeNotifyingRecorder) CloseNotify() <-chan bool {
	return c.closed
}

type contextKey string

func (c contextKey) String() string {
	return "sse:" + string(c)
}

func newRequest(stream string) (*Notifier, *closeNotifyingRecorder) {
	broker := New(func(req *http.Request) string {
		return req.Context().Value(contextKey("stream")).(string)
	})
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), contextKey("stream"), stream)
	req = req.WithContext(ctx)
	recorder := newCloseNotifyingRecorder()

	go broker.ServeHTTP(recorder, req)
	return broker, recorder
}

func delay() {
	time.Sleep(200 * time.Millisecond)
}

func TestSSEHeaders(t *testing.T) {
	_, recorder := newRequest("user1")
	delay()
	recorder.close()

	assert.Equal(t, http.StatusOK, recorder.Code)

	expected := map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
		"Connection":    "keep-alive",
	}

	h := recorder.Header()

	for key, value := range expected {
		assert.Equal(t, value, h.Get(key))
	}
}

func TestResponse(t *testing.T) {
	broker, recorder := newRequest("user1")

	delay()
	broker.Send("user1", []byte("first"))
	broker.Send("user1", []byte("last"))
	delay()
	recorder.close()

	resp := recorder.Result()
	body, _ := ioutil.ReadAll(resp.Body)
	assert.Equal(t, "data: first\n\ndata: last\n\n", string(body))
}

func TestDroppedConnection(t *testing.T) {
	broker, recorder := newRequest("user1")
	delay()
	recorder.close()
	delay()

	assert.Zero(t, len(broker.clients["user1"]))
	assert.Zero(t, len(broker.clients))

	withCtx := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), contextKey("id"), "user1")
			req = req.WithContext(ctx)
			next.ServeHTTP(rw, req)
		})
	}

	server := httptest.NewServer(withCtx(http.HandlerFunc(broker.ServeHTTP)))
	defer server.Close()

	client := &http.Client{
		Timeout: 1 * time.Millisecond,
	}

	_, err := client.Get(server.URL)

	assert.Error(t, err)
	delay()
	assert.Zero(t, len(broker.clients))

}
