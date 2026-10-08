package hubstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// A room's spec and its lock. See migration 0013.
//
//   - THE SPEC IS DESIRED, set by a person: the room.yaml the hub keeps for a room, byte for byte, with its sha256 and
//     who set it when. The hub reads it for a version, the name and a bound on its size, and for nothing else. What the
//     fields mean is internal/roomspec's, on the room.
//   - THE LOCK IS OBSERVED, posted by the room: the room.lock it last wrote, byte for byte, with when it arrived.
//   - OBSERVED NEVER OVERWRITES DESIRED. PutRoomLock does not touch room_spec and SetRoomSpec does not touch room_lock.
//   - NO CREDENTIALS. A key named like a token, a password, a secret or a key is refused in either.

// Bounds. The spec is a short file somebody typed. The lock is bigger because it carries a line for each step.
const (
	MaxRoomSpec = 64 << 10
	MaxRoomLock = 256 << 10
)

// ErrNoRoomSpec is a room with no spec set, and ErrNoRoomLock one that has not posted a lock.
var (
	ErrNoRoomSpec = errors.New("that room has no spec. `atrium rooms spec set <name> <file>` gives it one")
	ErrNoRoomLock = errors.New("that room has not posted a lock yet")
)

// RoomSpec is the desired state of a room, as stored.
type RoomSpec struct {
	Room   string    `json:"room"`
	YAML   string    `json:"yaml"`
	SHA256 string    `json:"sha256"`
	SetBy  string    `json:"set_by,omitempty"`
	SetAt  time.Time `json:"set_at"`
}

// RoomLock is the last observed state of a room, as stored.
type RoomLock struct {
	Room       string          `json:"room"`
	Lock       json.RawMessage `json:"lock"`
	ReceivedAt time.Time       `json:"received_at"`
}

// CheckRoomSpec is everything the hub asks of a spec: it fits, it is YAML, its version is 1, its name is the room's, and
// nothing in it is named like a credential.
func CheckRoomSpec(room string, raw []byte) error {
	if len(raw) == 0 {
		return errors.New("that spec is empty")
	}
	if len(raw) > MaxRoomSpec {
		return fmt.Errorf("that spec is %d bytes. a room spec is at most %d", len(raw), MaxRoomSpec)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("that spec is not YAML: %v", err)
	}
	if doc == nil {
		return errors.New("that spec has nothing in it")
	}
	if v, _ := doc["version"].(int); v != 1 {
		return fmt.Errorf("that spec says version %v. this hub keeps version 1", doc["version"])
	}
	if name, _ := doc["name"].(string); !strings.EqualFold(name, room) {
		return fmt.Errorf("that spec is named %q and this room is %q", name, room)
	}
	return noCredentials(doc)
}

// CheckRoomLock is the same for a lock: it fits, it is a JSON object, its version is 1, and it holds no credential.
func CheckRoomLock(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("that lock is empty")
	}
	if len(raw) > MaxRoomLock {
		return fmt.Errorf("that lock is %d bytes. a room lock is at most %d", len(raw), MaxRoomLock)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("that lock is not a JSON object: %v", err)
	}
	if v, _ := doc["version"].(float64); v != 1 {
		return fmt.Errorf("that lock says version %v. this hub keeps version 1", doc["version"])
	}
	return noCredentials(doc)
}

// noCredentials refuses a key anywhere in the tree whose name says it holds a secret.
func noCredentials(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if credentialName(k) {
				return fmt.Errorf("the key %q looks like a credential. a spec or a lock never carries one", k)
			}
			if err := noCredentials(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := noCredentials(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// credentialName is a TRIPWIRE, not a guarantee. It catches the names people give a secret and cannot see one under an
// innocent name or in a value.
func credentialName(k string) bool {
	k = strings.ToLower(k)
	for _, w := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "privatekey",
		"private_key", "private-key"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	switch k {
	case "key", "auth", "authorization", "bearer", "cookie", "session":
		return true
	}
	return strings.HasSuffix(k, "_key") || strings.HasSuffix(k, "-key") ||
		strings.HasSuffix(k, "_auth") || strings.HasSuffix(k, "_cookie") || strings.HasSuffix(k, "_session")
}

// SetRoomSpec stores the spec for a room, replacing the one there. The lock is not touched.
func (s *Store) SetRoomSpec(room string, raw []byte, by string) (RoomSpec, error) {
	r, err := s.ByName(room)
	if err != nil {
		return RoomSpec{}, err
	}
	if err := CheckRoomSpec(r.Name, raw); err != nil {
		return RoomSpec{}, refuse(err)
	}
	sum := sha256.Sum256(raw)
	sp := RoomSpec{Room: r.Name, YAML: string(raw), SHA256: hex.EncodeToString(sum[:]), SetBy: strings.TrimSpace(by),
		SetAt: now()}
	err = s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room_spec (room_id, yaml, sha256, set_by, set_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(room_id) DO UPDATE SET yaml = excluded.yaml, sha256 = excluded.sha256,
			   set_by = excluded.set_by, set_at = excluded.set_at`,
			r.ID, sp.YAML, sp.SHA256, sp.SetBy, ts(sp.SetAt))
		return err
	})
	if err != nil {
		return RoomSpec{}, err
	}
	s.Log(r, "spec-set", "sha256 "+sp.SHA256[:12]+" by "+sp.SetBy)
	return sp, nil
}

// RoomSpecOf reads a room's spec, or ErrNoRoomSpec.
func (s *Store) RoomSpecOf(room string) (RoomSpec, error) {
	r, err := s.ByName(room)
	if err != nil {
		return RoomSpec{}, err
	}
	sp := RoomSpec{Room: r.Name}
	var at string
	err = s.guard(func() error {
		err := s.db.QueryRow(`SELECT yaml, sha256, set_by, set_at FROM room_spec WHERE room_id = ?`, r.ID).
			Scan(&sp.YAML, &sp.SHA256, &sp.SetBy, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrNoRoomSpec)
		}
		return err
	})
	if err != nil {
		return RoomSpec{}, err
	}
	if t, err := time.Parse(TimeFormat, at); err == nil {
		sp.SetAt = t
	}
	return sp, nil
}

// PutRoomLock stores the lock a room posted, replacing the last. The spec is not touched.
func (s *Store) PutRoomLock(room string, raw []byte) (RoomLock, error) {
	r, err := s.ByName(room)
	if err != nil {
		return RoomLock{}, err
	}
	if err := CheckRoomLock(raw); err != nil {
		return RoomLock{}, refuse(err)
	}
	lk := RoomLock{Room: r.Name, Lock: json.RawMessage(raw), ReceivedAt: now()}
	err = s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room_lock (room_id, lock_json, received_at) VALUES (?, ?, ?)
			 ON CONFLICT(room_id) DO UPDATE SET lock_json = excluded.lock_json, received_at = excluded.received_at`,
			r.ID, string(raw), ts(lk.ReceivedAt))
		return err
	})
	if err != nil {
		return RoomLock{}, err
	}
	sum := sha256.Sum256(raw)
	s.Log(r, "lock-posted", "sha256 "+hex.EncodeToString(sum[:])[:12])
	return lk, nil
}

// RoomLockOf reads a room's latest lock, or ErrNoRoomLock.
func (s *Store) RoomLockOf(room string) (RoomLock, error) {
	r, err := s.ByName(room)
	if err != nil {
		return RoomLock{}, err
	}
	lk := RoomLock{Room: r.Name}
	var raw, at string
	err = s.guard(func() error {
		err := s.db.QueryRow(`SELECT lock_json, received_at FROM room_lock WHERE room_id = ?`, r.ID).Scan(&raw, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrNoRoomLock)
		}
		return err
	})
	if err != nil {
		return RoomLock{}, err
	}
	lk.Lock = json.RawMessage(raw)
	if t, err := time.Parse(TimeFormat, at); err == nil {
		lk.ReceivedAt = t
	}
	return lk, nil
}
