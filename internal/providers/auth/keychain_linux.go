//go:build linux

package auth

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

func nativeSecretStore(root string) (SecretStore, error) {
	store := newSecretServiceStore(root, connectSecretBus)
	// Capability checking opens only an empty encrypted session. No stored items,
	// secrets, keyring prompts or authentication flows are requested here.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	session, err := store.session(ctx)
	if err != nil {
		return nil, fail("CREDENTIAL_STORE_UNAVAILABLE", "An available desktop Secret Service with encrypted sessions is required on Linux")
	}
	session.close()
	return store, nil
}
func connectSecretBus(ctx context.Context) (*dbus.Conn, error) {
	// Ubuntu's per-user bus is local and authenticated. Do not honor arbitrary
	// DBUS_SESSION_BUS_ADDRESS values, TCP buses, or shell-based autolaunch.
	root := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	for p := root; p != filepath.Dir(p); p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe desktop bus directory")
		}
	}
	st, err := os.Lstat(root)
	if err != nil || !ownedByCurrentUser(st) || st.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("desktop bus directory is not private")
	}
	socket := filepath.Join(root, "bus")
	st, err = os.Lstat(socket)
	if err != nil || st.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(st) {
		return nil, fmt.Errorf("desktop bus socket is unavailable")
	}
	raw, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			raw.Close()
		}
	}()
	unixConn, valid := raw.(*net.UnixConn)
	if !valid {
		return nil, fmt.Errorf("local desktop bus required")
	}
	control, err := unixConn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	var peerErr error
	if err = control.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil || peerErr != nil || peer == nil || peer.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("desktop bus owner mismatch")
	}
	deadline := time.Now().Add(3 * time.Second)
	if d, exists := ctx.Deadline(); exists && d.Before(deadline) {
		deadline = d
	}
	raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { raw.SetDeadline(time.Now()) })
	defer stop()
	conn, err := dbus.NewConn(raw)
	if err != nil {
		return nil, err
	}
	if err = conn.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Geteuid()))}); err != nil {
		conn.Close()
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		conn.Close()
		return nil, err
	}
	stop()
	raw.SetDeadline(time.Time{})
	ok = true
	return conn, nil
}
