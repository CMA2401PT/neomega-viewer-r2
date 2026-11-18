package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalDropRejectsNonLocalRequests(t *testing.T) {
	for _, tc := range []struct {
		name, remote, host, origin string
	}{
		{"remote", "192.0.2.1:1234", "localhost:2412", "http://localhost:2412"},
		{"foreign Host", "127.0.0.1:1234", "example.com:2412", "http://example.com:2412"},
		{"foreign Origin", "127.0.0.1:1234", "localhost:2412", "http://localhost:3000"},
		{"missing Origin", "127.0.0.1:1234", "localhost:2412", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/files/local-drop", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			localDrop(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
}
