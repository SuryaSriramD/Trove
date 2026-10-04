package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppDocumentsCannotBeFramedAndAttachmentPolicySurvives(t *testing.T) {
	for _, attachment := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		preventAppFraming(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if attachment {
				w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
			}
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(recorder, httptest.NewRequest("GET", "http://wails.localhost/", nil))
		want := "frame-ancestors 'none'"
		if attachment {
			want = "sandbox; default-src 'none'"
		}
		if recorder.Header().Get("Content-Security-Policy") != want || recorder.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal(recorder.Header())
		}
	}
}
