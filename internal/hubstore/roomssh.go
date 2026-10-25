package hubstore

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/dovholuknf/atrium/internal/base/sshkey"
)

// A room's ssh login: the user other rooms log in as, and the public key this room logs in with. See migration 0019.
//
//   - A PUBLIC KEY AND A USER NAME, NOTHING ELSE. The private half never leaves the room. The hub holds what a room
//     published and relays the set of them, and sshd on each room decides who gets in.
//   - ONLY A ROOM THE HUB KNOWS HAS A ROW. It is keyed by the room row and goes with it, so removing a room removes
//     its key from the set every other room is next given.
//   - ONLY ssh-ed25519, ONE LINE. Anything with options in front, a comment's newline, or another key type is refused,
//     because what is stored is written into other machines' authorized_keys.

// ErrNoRoomSSH is a room that has published nothing.
var ErrNoRoomSSH = errors.New("that room has not published an ssh key")

// RoomSSH is what one room published.
type RoomSSH struct {
	Room string `json:"room"`
	// User is the account other rooms log in as on that room.
	User string `json:"user"`
	// Key is `ssh-ed25519 <base64>`, no comment.
	Key  string `json:"key"`
	Port int    `json:"port"`
	// On is that room's own switch: whether it lets the hub carry ssh to its sshd.
	On        bool      `json:"on"`
	UpdatedAt time.Time `json:"updated_at"`
}

var sshUserRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

// CheckSSHUser is a login name safe to write into a Host block.
func CheckSSHUser(u string) error {
	if !sshUserRE.MatchString(u) {
		return fmt.Errorf("%q is not a login name", u)
	}
	return nil
}

// CleanSSHKey reads a public key line and returns it as `ssh-ed25519 <base64>`, the comment dropped.
func CleanSSHKey(line string) (string, error) { return sshkey.Clean(line) }

// PutRoomSSH stores what a room published, replacing the last.
func (s *Store) PutRoomSSH(room, user, key string, port int, on bool) error {
	r, err := s.ByName(room)
	if err != nil {
		return err
	}
	if err := CheckSSHUser(user); err != nil {
		return refuse(err)
	}
	k, err := CleanSSHKey(key)
	if err != nil {
		return refuse(err)
	}
	if port < 1 || port > 65535 {
		return refuse(errors.New("the port is 1 to 65535"))
	}
	onN := 0
	if on {
		onN = 1
	}
	err = s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO room_ssh (room_id, login_user, pubkey, port, ssh_on, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(room_id) DO UPDATE SET login_user = excluded.login_user, pubkey = excluded.pubkey,
			   port = excluded.port, ssh_on = excluded.ssh_on, updated_at = excluded.updated_at`,
			r.ID, user, k, port, onN, ts(s.now()))
		return err
	})
	if err != nil {
		return err
	}
	s.Log(r, "ssh-published", "login "+user)
	return nil
}

// RoomSSHSet is every published key of a room the hub still knows, by room name.
func (s *Store) RoomSSHSet() ([]RoomSSH, error) {
	var out []RoomSSH
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT r.name, x.login_user, x.pubkey, x.port, x.ssh_on, x.updated_at
			 FROM room_ssh x JOIN room r ON r.id = x.room_id ORDER BY r.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v RoomSSH
			var on int
			var at string
			if err := rows.Scan(&v.Room, &v.User, &v.Key, &v.Port, &on, &at); err != nil {
				return err
			}
			v.On = on != 0
			if t, err := time.Parse(TimeFormat, at); err == nil {
				v.UpdatedAt = t
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
