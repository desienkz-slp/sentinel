package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ainoc/internal/audit"
	"ainoc/internal/observability"
)

func TestObservabilityPreservesStreaming(t *testing.T) {
	recorder := httptest.NewRecorder()
	aud := audit.New("", 10)
	h := withObservability(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("observability middleware hides http.Flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher.Flush() // A flush commits an implicit 200 even without a body.
		if !recorder.Flushed {
			t.Fatal("flush did not reach underlying writer")
		}
		w.WriteHeader(http.StatusInternalServerError) // Too late to change the status.
		_, _ = w.Write([]byte("event: step\ndata: {}\n\n"))
	}), observability.NewCollector(), aud)
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/ask", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "event: step\ndata: {}\n\n" {
		t.Fatalf("unexpected stream: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	entries := aud.Recent(10)
	if len(entries) != 1 || entries[0].ExecutionStatus != "OK" {
		t.Fatalf("audit recorded wrong committed status: %+v", entries)
	}
}

// Deliberately expose only ResponseWriter's required methods.
type nonFlushingWriter struct{ http.ResponseWriter }

func TestObservabilityDoesNotInventFlusher(t *testing.T) {
	h := withObservability(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); ok {
			t.Fatal("non-streaming writer advertises http.Flusher")
		}
		if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("Flush = %v, want ErrNotSupported", err)
		}
	}), nil, nil)
	h.ServeHTTP(nonFlushingWriter{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestObservabilityRecordsFirstStatus(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		w := &observabilityResponseWriter{ResponseWriter: recorder}
		want := http.StatusAccepted
		if implicit {
			_, _ = w.Write([]byte("body"))
			want = http.StatusOK
		} else {
			w.WriteHeader(want)
		}
		w.WriteHeader(http.StatusInternalServerError)
		if w.status != want || recorder.Code != want {
			t.Fatalf("implicit=%v: recorded=%d actual=%d want=%d", implicit, w.status, recorder.Code, want)
		}
	}
}
