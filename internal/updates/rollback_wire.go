package updates

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const rollbackProtocolVersion = 1
const rollbackFrameLimit = 4096

// RollbackTarget is supplied by the native reviewed restore boundary. It is never
// accepted from renderer arguments or inferred from the child's response.
type RollbackTarget struct {
	WorkspacePath     string `json:"workspacePath"`
	WorkspaceID       string `json:"workspaceId"`
	UserVersion       int    `json:"userVersion"`
	StorageGeneration int    `json:"storageGeneration"`
}

type rollbackMessage struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Nonce   string `json:"nonce"`
	PID     int    `json:"pid"`
	RollbackTarget
}

func validRollbackTarget(t RollbackTarget) bool {
	return t.WorkspacePath != "" && len(t.WorkspacePath) <= 3072 && filepath.IsAbs(t.WorkspacePath) && filepath.Clean(t.WorkspacePath) == t.WorkspacePath && !strings.ContainsRune(t.WorkspacePath, 0) && t.WorkspaceID != "" && len(t.WorkspaceID) <= 128 && !strings.ContainsRune(t.WorkspaceID, 0) && t.UserVersion >= 3 && t.StorageGeneration >= 1
}
func newRollbackNonce() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func validRollbackMessage(m rollbackMessage) bool {
	if m.Version != rollbackProtocolVersion || m.PID <= 0 || !validRollbackTarget(m.RollbackTarget) || len(m.Nonce) != 64 || strings.ToLower(m.Nonce) != m.Nonce {
		return false
	}
	if _, err := hex.DecodeString(m.Nonce); err != nil {
		return false
	}
	return m.Kind == "request" || m.Kind == "ready" || m.Kind == "commit" || m.Kind == "committed"
}
func writeRollbackMessage(w io.Writer, m rollbackMessage) error {
	if !validRollbackMessage(m) {
		return errors.New("invalid rollback handshake")
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > rollbackFrameLimit {
		return errors.New("rollback handshake exceeds bounds")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	if _, err = io.Copy(w, bytes.NewReader(header[:])); err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(raw))
	return err
}
func readRollbackMessage(r io.Reader) (rollbackMessage, error) {
	var out rollbackMessage
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return out, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > rollbackFrameLimit {
		return out, errors.New("rollback handshake exceeds bounds")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return out, err
	}
	if !utf8.Valid(raw) {
		return out, errors.New("invalid rollback handshake encoding")
	}
	// The wire is a flat object. Reject duplicate keys before typed decoding so a
	// second authority field cannot silently override the first reviewed value.
	scan := json.NewDecoder(bytes.NewReader(raw))
	token, err := scan.Token()
	if err != nil || token != json.Delim('{') {
		return out, errors.New("invalid rollback handshake")
	}
	seen := map[string]bool{}
	for scan.More() {
		token, err = scan.Token()
		if err != nil {
			return out, err
		}
		key, ok := token.(string)
		if !ok || seen[key] || len(seen) >= 8 {
			return out, errors.New("invalid rollback handshake fields")
		}
		seen[key] = true
		var value json.RawMessage
		if err = scan.Decode(&value); err != nil {
			return out, err
		}
		if len(value) == 0 || value[0] == '{' || value[0] == '[' {
			return out, errors.New("invalid rollback handshake value")
		}
	}
	if _, err = scan.Token(); err != nil {
		return out, err
	}
	if _, err = scan.Token(); err != io.EOF {
		return out, errors.New("trailing rollback handshake data")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&out); err != nil || len(seen) != 8 || !validRollbackMessage(out) {
		return rollbackMessage{}, errors.New("invalid rollback handshake")
	}
	return out, nil
}
func matchesRollbackMessage(m rollbackMessage, kind, nonce string, pid int, target RollbackTarget) bool {
	return validRollbackMessage(m) && m.Kind == kind && m.Nonce == nonce && m.PID == pid && m.RollbackTarget == target
}
