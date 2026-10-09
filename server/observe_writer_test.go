package server

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func captureResponse(t *testing.T, w http.ResponseWriter, handler http.HandlerFunc) RequestEvent {
	t.Helper()
	var event RequestEvent
	ObserveHandler(handler, []RequestObserver{RequestObserverFunc(func(e RequestEvent) { event = e })}).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	return event
}

func TestObservedFinalStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []int
		write   bool
		want    int
	}{
		{"empty", nil, false, 200},
		{"implicit", nil, true, 200},
		{"first-final", []int{201, 500}, true, 201},
		{"interim", []int{100, 103, 202}, false, 202},
		{"interim-implicit", []int{103}, true, 200},
		{"interim-empty", []int{103}, false, 200},
		{"switching", []int{101, 500}, false, 101},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &responseProbe{header: make(http.Header)}
			event := captureResponse(t, w, func(w http.ResponseWriter, _ *http.Request) {
				for _, status := range tc.headers {
					w.WriteHeader(status)
				}
				if tc.write {
					_, _ = w.Write([]byte("body"))
				}
			})
			if event.Status != tc.want {
				t.Fatalf("status=%d want=%d", event.Status, tc.want)
			}
			if tc.write && event.ResponseBytes != 4 {
				t.Fatalf("bytes=%d", event.ResponseBytes)
			}
			if event.Hijacked {
				t.Fatal("ordinary response marked hijacked")
			}
			for i, status := range w.statuses {
				if i == 0 {
					continue
				}
				if status >= 200 && w.statuses[0] >= 200 {
					t.Fatal("forwarded a second final status")
				}
			}
		})
	}
}

type responseProbe struct {
	header   http.Header
	statuses []int
	body     bytes.Buffer
	limit    int
	err      error
}

func (w *responseProbe) Header() http.Header    { return w.header }
func (w *responseProbe) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *responseProbe) Write(data []byte) (int, error) {
	if w.limit > 0 && len(data) > w.limit {
		data = data[:w.limit]
	}
	n, _ := w.body.Write(data)
	return n, w.err
}
func (w *responseProbe) String() string { return w.body.String() }

func TestObservedPartialWrites(t *testing.T) {
	failure := errors.New("write failure")
	w := &responseProbe{header: make(http.Header), limit: 2, err: failure}
	event := captureResponse(t, w, func(w http.ResponseWriter, _ *http.Request) {
		for range 2 {
			n, err := w.Write([]byte("long body"))
			if n != 2 || !errors.Is(err, failure) {
				t.Fatalf("write=%d, %v", n, err)
			}
		}
	})
	if event.ResponseBytes != 4 || event.Status != 200 {
		t.Fatalf("event=%+v", event)
	}
}

type readerProbe struct {
	*responseProbe
	calls int
}

func (w *readerProbe) ReadFrom(r io.Reader) (int64, error) {
	w.calls++
	n, err := io.Copy(&w.body, r)
	if w.err != nil {
		return n, w.err
	}
	return n, err
}

type readerOnly struct{ io.Reader }

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }

func TestObservedReaderFromPreservesUncommittedStatus(t *testing.T) {
	failure := errors.New("source read failed")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		status int
		err    error
	}{
		{"empty", readerOnly{strings.NewReader("")}, http.StatusCreated, nil},
		{"immediate-read-error", failedReader{failure}, http.StatusBadGateway, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan RequestEvent, 1)
			h := ObserveHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n, err := io.Copy(w, tc.reader)
				if n != 0 || !errors.Is(err, tc.err) {
					t.Errorf("copy=%d, %v; want=0, %v", n, err, tc.err)
				}
				w.WriteHeader(tc.status)
			}), []RequestObserver{RequestObserverFunc(func(e RequestEvent) { events <- e })})
			s := httptest.NewServer(h)
			defer s.Close()
			client := s.Client()
			client.Timeout = 2 * time.Second
			response, err := client.Get(s.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Errorf("wire status=%d want=%d", response.StatusCode, tc.status)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || len(body) != 0 {
				t.Errorf("body=%q, %v; want empty body", body, err)
			}
			select {
			case event := <-events:
				if event.Status != tc.status || event.ResponseBytes != 0 {
					t.Errorf("observed status=%d bytes=%d; want=%d, 0", event.Status, event.ResponseBytes, tc.status)
				}
			case <-time.After(time.Second):
				t.Fatal("request observation did not complete")
			}
		})
	}
}

func TestObservedReaderFrom(t *testing.T) {
	for _, fast := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			p := &responseProbe{header: make(http.Header)}
			failure := errors.New("copy failure")
			if fail {
				p.err = failure
			}
			var w http.ResponseWriter = p
			rf := &readerProbe{responseProbe: p}
			if fast {
				w = rf
			}
			event := captureResponse(t, w, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				n, err := io.Copy(w, readerOnly{strings.NewReader("payload")})
				if n != 7 || fail != errors.Is(err, failure) {
					t.Fatalf("copy=%d, %v", n, err)
				}
			})
			if event.Status != http.StatusAccepted || event.ResponseBytes != 7 || p.String() != "payload" {
				t.Fatalf("status=%d bytes=%d body=%q", event.Status, event.ResponseBytes, p.String())
			}
			if fast && rf.calls != 1 {
				t.Fatalf("fast-path calls=%d", rf.calls)
			}
		}
	}
}

func TestObservedNestedMetadata(t *testing.T) {
	var events []RequestEvent
	observer := RequestObserverFunc(func(e RequestEvent) { events = append(events, e) })
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		MarkObservedRequest(r, "page", "GET /room/{id}")
		_, _ = w.Write([]byte("page"))
	})
	inner := ObserveHandler(leaf, []RequestObserver{observer})
	outer := ObserveHandler(inner, []RequestObserver{observer})
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/room/123", nil))
	if len(events) != 2 {
		t.Fatalf("notifications=%d", len(events))
	}
	for _, event := range events {
		if event.Kind != "page" || event.Pattern != "GET /room/{id}" || event.ResponseBytes != 4 {
			t.Fatalf("event=%+v", event)
		}
	}
	if events[0].Request != events[1].Request {
		t.Fatal("nested wrapper created another request copy")
	}
}

type controllerProbe struct {
	*responseProbe
	flushErr, hijackErr         error
	flushed, duplex             bool
	readDeadline, writeDeadline time.Time
	conn                        net.Conn
}

func (w *controllerProbe) FlushError() error { w.flushed = true; return w.flushErr }
func (w *controllerProbe) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, nil, w.hijackErr
}
func (w *controllerProbe) SetReadDeadline(d time.Time) error  { w.readDeadline = d; return nil }
func (w *controllerProbe) SetWriteDeadline(d time.Time) error { w.writeDeadline = d; return nil }
func (w *controllerProbe) EnableFullDuplex() error            { w.duplex = true; return nil }

type unwrapProbe struct{ http.ResponseWriter }

func (w unwrapProbe) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestObservedResponseController(t *testing.T) {
	left, right := net.Pipe()
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	p := &controllerProbe{responseProbe: &responseProbe{header: make(http.Header)}, conn: left}
	deadline := time.Now().Add(time.Minute)
	event := captureResponse(t, unwrapProbe{p}, func(w http.ResponseWriter, _ *http.Request) {
		c := http.NewResponseController(w)
		if err := c.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		if err := c.SetWriteDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		if err := c.EnableFullDuplex(); err != nil {
			t.Fatal(err)
		}
		if err := c.Flush(); err != nil {
			t.Fatal(err)
		}
		conn, _, err := c.Hijack()
		if err != nil || conn != left {
			t.Fatalf("hijack=%v, %v", conn, err)
		}
	})
	if !p.flushed || !p.duplex || p.readDeadline != deadline || p.writeDeadline != deadline {
		t.Fatal("lost controller operation")
	}
	if !event.Hijacked || event.Status != 200 {
		t.Fatalf("event=%+v", event)
	}
}

func TestObservedHijackStatus(t *testing.T) {
	for _, fail := range []bool{false, true} {
		failure := errors.New("hijack failure")
		p := &controllerProbe{responseProbe: &responseProbe{header: make(http.Header)}}
		if fail {
			p.hijackErr = failure
		}
		event := captureResponse(t, p, func(w http.ResponseWriter, _ *http.Request) {
			_, _, err := http.NewResponseController(w).Hijack()
			if fail != errors.Is(err, failure) {
				t.Fatalf("hijack error=%v", err)
			}
		})
		if event.Hijacked == fail {
			t.Fatalf("hijacked=%v failed=%v", event.Hijacked, fail)
		}
		want := 0
		if fail {
			want = 200
		}
		if event.Status != want {
			t.Fatalf("status=%d want=%d", event.Status, want)
		}
	}
}

func TestObservedUnsupportedController(t *testing.T) {
	p := &responseProbe{header: make(http.Header)}
	captureResponse(t, p, func(w http.ResponseWriter, _ *http.Request) {
		c := http.NewResponseController(w)
		if err := c.Flush(); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("flush=%v", err)
		}
		if _, _, err := c.Hijack(); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("hijack=%v", err)
		}
		if err := w.(http.Pusher).Push("/asset", nil); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("push=%v", err)
		}
	})
}

type pushProbe struct {
	*responseProbe
	target string
	opts   *http.PushOptions
}

func (w *pushProbe) Push(target string, opts *http.PushOptions) error {
	w.target, w.opts = target, opts
	return nil
}

func TestObservedPushAndFlush(t *testing.T) {
	p := &pushProbe{responseProbe: &responseProbe{header: make(http.Header)}}
	opts := &http.PushOptions{Method: "GET"}
	captureResponse(t, p, func(w http.ResponseWriter, _ *http.Request) {
		if err := w.(http.Pusher).Push("/asset", opts); err != nil {
			t.Fatal(err)
		}
	})
	if p.target != "/asset" || p.opts != opts {
		t.Fatal("push arguments changed")
	}
	w := httptest.NewRecorder()
	event := captureResponse(t, w, func(w http.ResponseWriter, _ *http.Request) { w.(http.Flusher).Flush() })
	if !w.Flushed || event.Status != 200 {
		t.Fatal("ordinary Flush was lost")
	}
	failure := errors.New("flush failure")
	captureResponse(t, &controllerProbe{responseProbe: p.responseProbe, flushErr: failure}, func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); !errors.Is(err, failure) {
			t.Fatalf("flush error=%v", err)
		}
	})
}

func TestObservedHijackedBytes(t *testing.T) {
	events := make(chan RequestEvent, 1)
	h := ObserveHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		w.WriteHeader(500) // The underlying HTTP owner has already been hijacked.
		_, _ = rw.WriteString("connection payload")
		_ = rw.Flush()
	}), []RequestObserver{RequestObserverFunc(func(e RequestEvent) { events <- e })})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	conn, err := net.DialTimeout("tcp", s.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(conn)
	if err != nil || string(data) != "connection payload" {
		t.Fatalf("connection=%q, %v", data, err)
	}
	select {
	case event := <-events:
		if !event.Hijacked || event.Status != 0 || event.ResponseBytes != 0 {
			t.Fatalf("event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not complete")
	}
}

func TestObservedCompressedBytes(t *testing.T) {
	app := New()
	var event RequestEvent
	app.UseObserver(RequestObserverFunc(func(e RequestEvent) { event = e }))
	body := strings.Repeat("response body ", 1024)
	app.Mount("GET /body", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
	r := httptest.NewRequest("GET", "/body", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("response was not compressed")
	}
	if event.ResponseBytes != int64(w.Body.Len()) || event.ResponseBytes >= int64(len(body)) {
		t.Fatalf("bytes=%d encoded=%d raw=%d", event.ResponseBytes, w.Body.Len(), len(body))
	}
}
