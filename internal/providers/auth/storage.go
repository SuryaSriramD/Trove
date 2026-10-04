package auth

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type metadata struct {
	Version       int            `json:"version"`
	HostID        string         `json:"hostId"`
	Registrations []Registration `json:"registrations"`
}

func (m *Manager) save() error {
	data, err := json.Marshal(m.meta)
	if err != nil {
		return fail("STORAGE_FAILED", "Account registrations could not be saved")
	}
	f, err := os.CreateTemp(m.root, ".registrations-*")
	if err != nil {
		return fail("STORAGE_FAILED", "Account registrations could not be saved")
	}
	name := f.Name()
	defer os.Remove(name)
	err = f.Chmod(0600)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(m.root, "registrations.json"))
	}
	if err != nil {
		return fail("STORAGE_FAILED", "Account registrations could not be saved")
	}
	dir, err := os.Open(m.root)
	if err != nil {
		return fail("STORAGE_FAILED", "Account registrations could not be saved")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return fail("STORAGE_FAILED", "Account registrations could not be saved")
	}
	return nil
}

func (m *Manager) load() error {
	path := filepath.Join(m.root, "registrations.json")
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		m.meta = metadata{Version: 1, HostID: "urn:uuid:" + uuid.NewString(), Registrations: []Registration{}}
		return m.save()
	}
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 256*1024 || !ownedByCurrentUser(st) {
		return fail("STORAGE_FAILED", "Account registration file is not private")
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &m.meta) != nil || m.meta.Version != 1 || len(m.meta.Registrations) > 32 {
		return fail("STORAGE_FAILED", "Account registrations could not be read")
	}
	if len(m.meta.HostID) != 45 || m.meta.HostID[:9] != "urn:uuid:" {
		return fail("STORAGE_FAILED", "Account host identity is invalid")
	}
	if _, err := uuid.Parse(m.meta.HostID[9:]); err != nil {
		return fail("STORAGE_FAILED", "Account host identity is invalid")
	}
	seen := map[string]bool{}
	for _, r := range m.meta.Registrations {
		if _, err := uuid.Parse(r.ID); err != nil || r.Identity.RegistrationID != r.ID || !validClientID(r.Identity.ClientID) || r.Identity.Subject == "" || seen[r.ID] {
			return fail("STORAGE_FAILED", "Account registration identity is invalid")
		}
		switch r.State {
		case "signed_in", "signed_out", "reauthorization_required", "sign_out_incomplete":
		default:
			return fail("STORAGE_FAILED", "Account registration state is invalid")
		}
		key := r.Identity.Subject + "\x00" + r.Identity.ClientID
		if seen[key] {
			return fail("STORAGE_FAILED", "Duplicate account registration identity")
		}
		seen[r.ID] = true
		seen[key] = true
	}
	return nil
}
