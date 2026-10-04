package main

import "testing"

func TestDeepLinksRejectUnsafeInputsAndRetainColdLaunch(t *testing.T) {
	page := "019a1234-5678-7000-8000-000000000002"
	block := "019a1234-5678-7000-8000-000000000003"
	workspace := "019a1234-5678-7000-8000-000000000001"
	valid := "trove://page/" + page + "/block/" + block + "?workspace=" + workspace
	for _, raw := range []string{valid, "trove://page/" + page, "trove://page/" + page + "#" + block} {
		if err := validateDeepLink(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"https://page/" + page, "trove://user@page/" + page,
		"trove://page:80/" + page, "trove://page/" + page + "?path=/private/data",
		valid + "&workspace=" + workspace, "trove://page/" + page + "#fragment",
		"trove://page/" + page + "?workspace=", "trove://page/" + page + "%2fblock%2f" + block} {
		if validateDeepLink(raw) == nil {
			t.Fatalf("accepted invalid link %q", raw)
		}
	}
	app := NewApp()
	app.openURL(valid)
	app.openURL("not a link")
	if got := app.PendingDeepLink(); got != valid {
		t.Fatalf("lost cold-launch link: %q", got)
	}
	if got := app.PendingDeepLink(); got != "" {
		t.Fatalf("replayed link: %q", got)
	}
}
