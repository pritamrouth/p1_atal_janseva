// Package store provides a Redis-backed session store for WhatsApp conversations.
// Sessions are stored as JSON with a 24-hour TTL.
// Key format:  wa:session:<e164_phone>
// Thread-safety: Per-user mutexes prevent concurrent modification of same session.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	sessionTTL = 24 * time.Hour
	keyPrefix  = "wa:session:"

	// messageKeyPrefix marks an inbound WhatsApp message ID as processed.
	// Meta delivers webhooks at-least-once, so the same message can arrive
	// more than once; the TTL covers its retry window.
	messageKeyPrefix = "wa:msg:"
	messageDedupTTL  = 7 * 24 * time.Hour
)

// Step represents the current step in the conversation flow.
type Step int

const (
	StepStart       Step = iota // 0 – show language picker
	StepLangChosen              // 1 – language chosen, waiting for PIN code
	StepLeader                  // 2 – PIN valid, waiting for leader selection
	StepMainMenu                // 3 – onboarded, waiting for a menu option
	StepComplaint               // 4 – waiting for complaint details (name/complaint/address)
	StepTrackTicket             // 5 – waiting for a ticket ID
	StepMeeting                 // 6 – waiting for meeting details (name/date/reason)
	StepMeetingSlot             // 7 – meeting details parsed, waiting for a time slot
)

// Session holds the state for a single WhatsApp user.
type Session struct {
	PhoneNumber    string `json:"phone"`
	Step           Step   `json:"step"`
	Lang           string `json:"lang"`
	Pincode        string `json:"pincode"`
	State          string `json:"state"`
	District       string `json:"district"`
	StateHindi     string `json:"state_hindi"`
	DistrictHindi  string `json:"district_hindi"`
	NagarsevakID   string `json:"nagarsevak_id"`
	NagarsevakName string `json:"nagarsevak_name"`
	NagarsevakSlug string `json:"nagarsevak_slug"`
	NagarsevakWard string `json:"nagarsevak_ward"`

	// Draft meeting details, carried from StepMeeting to StepMeetingSlot.
	PendingName   string `json:"pending_name,omitempty"`
	PendingDate   string `json:"pending_date,omitempty"`
	PendingReason string `json:"pending_reason,omitempty"`
}

// Store is a Redis-backed session store.
// Thread-safe: Uses per-user mutexes to prevent concurrent modification of same session.
type Store struct {
	rdb        *redis.Client
	mu         sync.Mutex             // Protects phoneLocks map
	phoneLocks map[string]*sync.Mutex // Per-user mutex for serializing concurrent access
}

// New returns an initialised Store and pings Redis.
func New(rdb *redis.Client) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Store{
		rdb:        rdb,
		phoneLocks: make(map[string]*sync.Mutex),
	}, nil
}

func (s *Store) getLock(phone string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mu, exists := s.phoneLocks[phone]; exists {
		return mu
	}

	// Create new per-user mutex
	mu := &sync.Mutex{}
	s.phoneLocks[phone] = mu
	return mu
}

// Get returns the session for a phone number.
// If none exists, a fresh session is returned (not yet written to Redis).
// Thread-safe: Serializes concurrent access per user.
func (s *Store) Get(ctx context.Context, phone string) (*Session, error) {
	lock := s.getLock(phone)
	lock.Lock()
	defer lock.Unlock()

	val, err := s.rdb.Get(ctx, key(phone)).Result()
	if errors.Is(err, redis.Nil) {
		return &Session{PhoneNumber: phone, Step: StepStart}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis GET %s: %w", phone, err)
	}
	var sess Session
	if err := json.Unmarshal([]byte(val), &sess); err != nil {
		slog.Warn("corrupted session, resetting", "phone", phone, "err", err)
		return &Session{PhoneNumber: phone, Step: StepStart}, nil
	}
	return &sess, nil
}

// Save persists a session with a refreshed TTL.
// Thread-safe: Serializes concurrent access per user.
func (s *Store) Save(ctx context.Context, sess *Session) error {
	lock := s.getLock(sess.PhoneNumber)
	lock.Lock()
	defer lock.Unlock()

	b, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := s.rdb.Set(ctx, key(sess.PhoneNumber), b, sessionTTL).Err(); err != nil {
		return fmt.Errorf("redis SET %s: %w", sess.PhoneNumber, err)
	}
	return nil
}

// Reset deletes a session (user typed "reset").
// Thread-safe: Serializes concurrent access per user.
func (s *Store) Reset(ctx context.Context, phone string) error {
	lock := s.getLock(phone)
	lock.Lock()
	defer lock.Unlock()

	if err := s.rdb.Del(ctx, key(phone)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis DEL %s: %w", phone, err)
	}
	return nil
}

// ClaimMessage atomically records an inbound WhatsApp message ID and reports
// whether this is the first time it has been seen. It returns false for a
// duplicate webhook delivery. The check is atomic (SET NX), so two concurrent
// deliveries of the same message cannot both be processed.
//
// An empty ID means the payload cannot be deduplicated; it is treated as new.
// Callers should fail open (process the message) when an error is returned.
func (s *Store) ClaimMessage(ctx context.Context, messageID string) (bool, error) {
	if messageID == "" {
		return true, nil
	}
	first, err := s.rdb.SetNX(ctx, messageKeyPrefix+messageID, 1, messageDedupTTL).Result()
	if err != nil {
		return false, fmt.Errorf("redis SETNX %s: %w", messageID, err)
	}
	return first, nil
}

func key(phone string) string {
	return keyPrefix + phone
}
