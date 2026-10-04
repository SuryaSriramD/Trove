package main

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"testing"
	"time"
	"trove.local/phase0/internal/clipper"
	"trove.local/phase0/internal/workspace"
)

func TestDesktopClipperExplicitSessionRevocationAndShutdown(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.ctx = context.Background()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.completeStartup(nil)
	defer a.shutdown(context.Background())
	status, err := a.ClipperStatus()
	if err != nil || status == "" {
		t.Fatal(err)
	}
	if _, exists := clippers.Load(a); exists {
		t.Fatal("listener opened implicitly")
	}
	start := func() clipper.Pairing {
		t.Helper()
		raw, err := a.BeginClipperPairing("mmdbnaapeedpfjhafldjcacmanhjmjbb", []string{"inbox"})
		if err != nil {
			t.Fatal(err)
		}
		var p clipper.Pairing
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	closed := func(p clipper.Pairing) {
		t.Helper()
		u, _ := url.Parse(p.Endpoint)
		connection, err := net.DialTimeout("tcp", u.Host, time.Second)
		if err == nil {
			connection.Close()
			t.Fatal("revoked listener still open")
		}
	}
	first := start()
	second := start()
	closed(first)
	a.RevokeClipperPairing()
	closed(second)
	third := start()
	a.shutdown(context.Background())
	closed(third)
	if _, exists := clippers.Load(a); exists {
		t.Fatal("session survived shutdown")
	}
}
