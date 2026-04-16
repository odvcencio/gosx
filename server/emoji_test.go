package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmojiCodesHandlerIncludesSlackishAliases(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_gosx/emoji-codes.json", nil)
	rec := httptest.NewRecorder()

	EmojiCodesHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	codes := decodeEmojiRows(t, rec.Body.Bytes())

	for name, want := range map[string]string{
		"simple_smile": "🙂",
		"slight_smile": "🙂",
		"thumbs_up":    "👍",
		"red_heart":    "❤️",
	} {
		if got := codes[name]; got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestEmojiCodesHandlerGzipIncludesSlackishAliases(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_gosx/emoji-codes.json", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	EmojiCodesHandler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", rec.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}

	codes := decodeEmojiRows(t, body)
	if got := codes["simple_smile"]; got != "🙂" {
		t.Fatalf("simple_smile = %q, want 🙂", got)
	}
}

func decodeEmojiRows(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var rows [][]string
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode emoji rows: %v", err)
	}
	codes := make(map[string]string, len(rows))
	for _, row := range rows {
		if len(row) >= 2 {
			codes[row[0]] = row[1]
		}
	}
	return codes
}
