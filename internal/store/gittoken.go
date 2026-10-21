package store

import "time"

// GitToken is what the room keeps of a card's git token: the card and a SHA-256 of the secret, never the token.
type GitToken struct {
	Card string
	Hash string
	At   time.Time
}

// SaveGitToken keeps the hash of a card's token so the forwarder still knows it after a room restart. The runner it was
// handed to may be kept by the pty host.
func (s *Store) SaveGitToken(card, hash string, at time.Time) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO card_git_token (card, token_hash, minted_at) VALUES (?, ?, ?)
			 ON CONFLICT(card) DO UPDATE SET token_hash = excluded.token_hash, minted_at = excluded.minted_at`,
			card, hash, ts(at))
		return err
	})
}

// DeleteGitToken forgets a card's token.
func (s *Store) DeleteGitToken(card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM card_git_token WHERE card = ?`, card)
		return err
	})
}

// GitTokens lists every token hash kept.
func (s *Store) GitTokens() ([]GitToken, error) {
	var out []GitToken
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT card, token_hash, minted_at FROM card_git_token`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g GitToken
			var at string
			if err := rows.Scan(&g.Card, &g.Hash, &at); err != nil {
				return err
			}
			g.At, _ = parseTS(at)
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, err
}
