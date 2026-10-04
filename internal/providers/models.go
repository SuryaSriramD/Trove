package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type AccountModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ChatGPTModels uses the selected registration's plan token and the public
// account-specific catalog. Bundled CLI defaults are not account entitlements.
func ChatGPTModels(ctx context.Context, token string) ([]AccountModel, error) {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return chatGPTModels(ctx, client, token)
}

func chatGPTModels(ctx context.Context, client *http.Client, token string) ([]AccountModel, error) {
	if token == "" || len(token) > 64<<10 || strings.ContainsAny(token, "\x00\r\n") {
		return nil, errors.New("A selected authorized account is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, errors.New("Could not prepare model catalog request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach the account model catalog")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, errors.New("This account must authorize ChatGPT plan usage again")
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("The account model catalog is unavailable; try again later")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, errors.New("The account model catalog exceeded its response limit")
	}
	var catalog struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &catalog) != nil || catalog.Models == nil || len(catalog.Models) > 1024 {
		return nil, errors.New("The account returned an unsupported model catalog")
	}
	models := []AccountModel{}
	seen := map[string]bool{}
	validID := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	for _, model := range catalog.Models {
		if model.Visibility != "list" {
			continue
		}
		if !validID.MatchString(model.Slug) || len(model.DisplayName) > 256 || seen[model.Slug] {
			return nil, errors.New("The account returned an invalid model identity")
		}
		seen[model.Slug] = true
		name := model.DisplayName
		if name == "" {
			name = model.Slug
		}
		models = append(models, AccountModel{model.Slug, name})
	}
	return models, nil
}
