package main

import "net/http"

// Provider embeds retain their own origin. App documents must never become a
// provider-controlled descendant frame; attachment handlers keep their stricter
// sandbox policy when they override this default.
func preventAppFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
