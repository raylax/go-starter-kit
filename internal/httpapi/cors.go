package httpapi

import (
	"net/http"
	"strings"
)

// CORS 只允许精确配置的来源，不接受 Cookie 凭据；预检不需要会话。
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")
			if origin != "" {
				if !allowed[origin] {
					Problem(w, 403, "origin not allowed")
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				if origin == "" || !allowed[origin] {
					Problem(w, 403, "origin not allowed")
					return
				}
				switch r.Header.Get("Access-Control-Request-Method") {
				case "GET", "POST", "PUT", "PATCH", "DELETE":
				default:
					Problem(w, 403, "method not allowed")
					return
				}
				for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
					h = strings.ToLower(strings.TrimSpace(h))
					if h != "" && h != "authorization" && h != "content-type" {
						Problem(w, 403, "header not allowed")
						return
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
