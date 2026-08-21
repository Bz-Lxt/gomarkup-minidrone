package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"minidrone/internal/store"
)

func TestFromBuild(t *testing.T) {
	b := &store.Build{
		ID: "x", Pipeline: "p", Number: 3, State: store.StateSuccess,
		StartedAt: time.Now().Add(-time.Second), EndedAt: time.Now(),
	}
	p := FromBuild(b)
	if p.BuildID != "x" || p.Number != 3 || p.DurationMS <= 0 {
		t.Fatalf("负载不符: %+v", p)
	}
}

func TestSendPostsJSON(t *testing.T) {
	var got Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(2 * time.Second)
	n.Send(context.Background(), []string{srv.URL}, Payload{BuildID: "abc", State: store.StateSuccess})
	if got.BuildID != "abc" {
		t.Fatalf("服务端未收到通知: %+v", got)
	}
}
