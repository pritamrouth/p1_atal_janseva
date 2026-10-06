// Package inactivity expires idle WhatsApp sessions.
// After InactivityTimeout of silence the session is deleted from Redis so the
// next message starts fresh, and a callback receives the last-known snapshot.
package inactivity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	InactivityTimeout = 90 * time.Second
	ScanInterval      = 10 * time.Second

	activeKeyPrefix  = "wa:active:"
	sessionKeyPrefix = "wa:session:"
	activeTTL        = InactivityTimeout + 30*time.Second
)

// SessionSnapshot is the minimal session data passed to TimeoutFunc.
// JSON tags must match the Session struct tags in store package.
type SessionSnapshot struct {
	Lang           string `json:"lang"`
	Pincode        string `json:"pincode"`
	NagarsevakID   string `json:"nagarsevak_id"`
	NagarsevakWard string `json:"nagarsevak_ward"`
}

// TimeoutFunc is called when a user's inactivity timer fires.
// snap contains the last-known session state before it was deleted.
type TimeoutFunc func(ctx context.Context, phone string, snap SessionSnapshot)

// Monitor manages inactivity tracking.
type Monitor struct {
	rdb     *redis.Client
	onTimer TimeoutFunc
}

// New creates a Monitor. Call Start() to launch the background goroutine.
func New(rdb *redis.Client, onTimer TimeoutFunc) *Monitor {
	return &Monitor{rdb: rdb, onTimer: onTimer}
}

// Touch records "user phone just sent a message" — resets their deadline.
func (m *Monitor) Touch(ctx context.Context, phone string) {
	key := activeKeyPrefix + phone
	now := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := m.rdb.Set(ctx, key, now, activeTTL).Err(); err != nil {
		slog.Warn("inactivity.Touch redis SET failed", "phone", phone, "err", err)
	}
}

// Cancel removes the inactivity entry for a phone (e.g. user reset manually).
func (m *Monitor) Cancel(ctx context.Context, phone string) {
	m.rdb.Del(ctx, activeKeyPrefix+phone)
}

// Start launches the background scanner goroutine. It runs until ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	slog.Info("inactivity monitor started",
		"timeout", InactivityTimeout,
		"scan_interval", ScanInterval,
	)
	go m.loop(ctx)
}

func (m *Monitor) loop(ctx context.Context) {
	ticker := time.NewTicker(ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("inactivity monitor stopped")
			return
		case <-ticker.C:
			m.scan(ctx)
		}
	}
}

func (m *Monitor) scan(ctx context.Context) {
	var cursor uint64
	pattern := activeKeyPrefix + "*"

	for {
		keys, nextCursor, err := m.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("inactivity scan SCAN error", "err", err)
			}
			return
		}

		for _, key := range keys {
			m.checkKey(ctx, key)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

func (m *Monitor) checkKey(ctx context.Context, key string) {
	val, err := m.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return
	}
	if err != nil {
		slog.Warn("inactivity checkKey GET", "key", key, "err", err)
		return
	}

	lastNano, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		m.rdb.Del(ctx, key)
		return
	}

	elapsed := time.Since(time.Unix(0, lastNano))
	if elapsed < InactivityTimeout {
		return
	}

	phone := key[len(activeKeyPrefix):]

	slog.Info("inactivity timeout firing",
		"phone", phone,
		"idle", elapsed.Round(time.Second),
	)

	// Read the full snapshot BEFORE deleting the session.
	snap := m.readSnapshot(ctx, phone)

	// Remove active-tracking key first (prevents double-fire).
	m.rdb.Del(ctx, key)

	// Remove the session so the next message starts fresh.
	sessionKey := sessionKeyPrefix + phone
	if delErr := m.rdb.Del(ctx, sessionKey).Err(); delErr != nil && !errors.Is(delErr, redis.Nil) {
		slog.Warn("inactivity: failed to delete session", "phone", phone, "err", delErr)
	}

	// Fire the callback with the full snapshot.
	cbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	m.onTimer(cbCtx, phone, snap)
}

// readSnapshot reads and parses the session stored in Redis into a SessionSnapshot.
// Returns a zero-value snapshot if the key is missing or unparseable.
func (m *Monitor) readSnapshot(ctx context.Context, phone string) SessionSnapshot {
	raw, err := m.rdb.Get(ctx, sessionKeyPrefix+phone).Result()
	if err != nil {
		return SessionSnapshot{}
	}
	var s SessionSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return SessionSnapshot{}
	}
	return s
}
