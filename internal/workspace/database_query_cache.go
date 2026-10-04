package workspace

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	querySnapshotBytes = 16 << 20
	queryCacheBytes    = 64 << 20
	queryCacheEntries  = 8
	queryDerivedWork   = 100000
	querySnapshotRows  = 100000
)

type recordQueryCursor struct {
	Version            int    `json:"version"`
	Collection         string `json:"c"`
	View               string `json:"v"`
	CollectionRevision string `json:"r"`
	ViewRevision       string `json:"s"`
	EventCursor        string `json:"e"`
	ClockMinute        string `json:"t"`
	Scope              string `json:"scope"`
	Query              string `json:"query"`
	LastID             string `json:"last,omitempty"`
	Snapshot           string `json:"snapshot,omitempty"`
	Offset             int    `json:"offset,omitempty"`
}

type recordQuerySnapshot struct {
	Token    string
	Identity recordQueryCursor
	IDs      []string
	Memo     map[string]DerivedValue
	Bytes    int
	Expires  time.Time
	LastUsed time.Time
}

// This cache contains only derived-query ordering/memo data, never canonical
// records. Bounds are serialized bytes and entry counts, not a process RSS claim.
// Active requests may retain an evicted immutable entry until that request ends.
type recordQueryCache struct {
	mu      sync.Mutex
	secret  []byte
	entries map[string]*recordQuerySnapshot
	bytes   int
	closed  bool
	builds  atomic.Int64
	hits    atomic.Int64
	work    atomic.Int64
}

func (c *recordQueryCache) initializeLocked() error {
	if c.closed {
		return Fail("CONFLICT", "Workspace query cache is closed")
	}
	if c.secret == nil {
		c.secret = make([]byte, 32)
		if _, err := rand.Read(c.secret); err != nil {
			c.secret = nil
			return err
		}
		c.entries = map[string]*recordQuerySnapshot{}
	}
	return nil
}

func (c *recordQueryCache) seal(cursor recordQueryCursor) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.initializeLocked(); err != nil {
		return "", err
	}
	raw := marshal(cursor)
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *recordQueryCache) open(encoded string) (recordQueryCursor, error) {
	var cursor recordQueryCursor
	if len(encoded) > 8192 {
		return cursor, Fail("INVALID_REQUEST", "Cursor exceeds the supported size")
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return cursor, Fail("INVALID_REQUEST", "Invalid cursor; restart the query")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor, Fail("INVALID_REQUEST", "Invalid cursor")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, Fail("INVALID_REQUEST", "Invalid cursor")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.initializeLocked(); err != nil {
		return cursor, err
	}
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(raw, &cursor) != nil || cursor.Version != 2 || cursor.Offset < 0 {
		return cursor, Fail("INVALID_REQUEST", "Cursor signature or version is invalid; restart the query")
	}
	return cursor, nil
}

func sameQuerySnapshot(a, b recordQueryCursor) bool {
	a.LastID, b.LastID, a.Snapshot, b.Snapshot = "", "", "", ""
	a.Offset, b.Offset = 0, 0
	return a == b
}

func (c *recordQueryCache) pruneLocked(at time.Time) {
	for token, entry := range c.entries {
		if !at.Before(entry.Expires) {
			delete(c.entries, token)
			c.bytes -= entry.Bytes
		}
	}
}

func (c *recordQueryCache) get(token string, identity recordQueryCursor) (*recordQuerySnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.initializeLocked(); err != nil {
		return nil, err
	}
	now := time.Now()
	c.pruneLocked(now)
	entry := c.entries[token]
	if entry == nil || !sameQuerySnapshot(entry.Identity, identity) {
		return nil, Fail("CONFLICT", "Derived query snapshot expired or was evicted; restart the query")
	}
	entry.LastUsed = now
	c.hits.Add(1)
	return entry, nil
}

func (c *recordQueryCache) put(entry *recordQuerySnapshot) error {
	if entry.Bytes > querySnapshotBytes || len(entry.IDs) > querySnapshotRows {
		return Fail("RESOURCE_EXHAUSTED", "Derived query exceeds its 16 MiB or 100000 row snapshot budget; narrow the view")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.initializeLocked(); err != nil {
		return err
	}
	now := time.Now()
	c.pruneLocked(now)
	for len(c.entries) >= queryCacheEntries || c.bytes+entry.Bytes > queryCacheBytes {
		var oldest *recordQuerySnapshot
		for _, candidate := range c.entries {
			if oldest == nil || candidate.LastUsed.Before(oldest.LastUsed) {
				oldest = candidate
			}
		}
		if oldest == nil {
			break
		}
		delete(c.entries, oldest.Token)
		c.bytes -= oldest.Bytes
	}
	entry.LastUsed, entry.Expires = now, now.Add(time.Minute)
	c.entries[entry.Token] = entry
	c.bytes += entry.Bytes
	c.builds.Add(1)
	return nil
}

func (c *recordQueryCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.entries = nil
	c.secret = nil
	c.bytes = 0
}
