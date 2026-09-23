package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	handler := CORS([]string{"https://web.example.com"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct {
		origin, headers string
		status          int
	}{{"https://web.example.com", "authorization, content-type", 204}, {"https://evil.example.com", "authorization", 403}, {"https://web.example.com", "cookie", 403}} {
		r := httptest.NewRequest("OPTIONS", "/v1/me", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Access-Control-Request-Method", "GET")
		r.Header.Set("Access-Control-Request-Headers", tc.headers)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("CORS错误: %d", w.Code)
		}
	}
}
