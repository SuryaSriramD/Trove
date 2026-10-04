package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAccountModelCatalogUsesSelectedTokenAndPublicRoute(t *testing.T) {
	client := &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.openai.com/v1/models" || r.Header.Get("Authorization") != "Bearer selected-account" || r.Method != "GET" {
			t.Fatalf("wrong catalog route or identity")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"model-z","display_name":"Selected model","visibility":"list"},{"slug":"hidden","visibility":"hidden"},{"slug":"model-a","visibility":"list"}]}`)), Header: http.Header{}}, nil
	})}
	models, err := chatGPTModels(context.Background(), client, "selected-account")
	if err != nil || len(models) != 2 || models[0].ID != "model-z" || models[1].Name != "model-a" {
		t.Fatalf("catalog lost account ordering/visibility: %+v %v", models, err)
	}
}

func TestAccountModelCatalogRejectsMalformedOrOversizedCatalog(t *testing.T) {
	for _, body := range []string{`{"data":[{"id":"bundled-default"}]}`, `{"models":[{"slug":"../../bad","visibility":"list"}]}`, `{"models":[{"slug":"duplicate","visibility":"list"},{"slug":"duplicate","visibility":"list"}]}`, strings.Repeat("x", (2<<20)+1)} {
		client := &http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})}
		if _, err := chatGPTModels(context.Background(), client, "selected-account"); err == nil {
			t.Fatal("accepted incompatible model catalog")
		}
	}
}

func TestAccountModelCatalogDoesNotExposeProviderErrorBody(t *testing.T) {
	client := &http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("secret-token-from-response")), Header: http.Header{}}, nil
	})}
	_, err := chatGPTModels(context.Background(), client, "selected-account")
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("unsafe catalog error")
	}
}
