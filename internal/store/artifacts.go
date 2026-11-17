package store

// Artifact kinds, the one list per card of what its session made.
const (
	ArtifactFile   = "file"
	ArtifactReport = "report"
	ArtifactPub    = "pub"
	ArtifactImage  = "img"
	ArtifactBuild  = "bin"
	ArtifactGit    = "git"
	ArtifactLink   = "link"
)

// Artifact is one entry of a card's list. Extra is JSON: sha, delta, where.
type Artifact struct {
	TaskID  string `json:"task_id"`
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Label   string `json:"label"`
	Source  string `json:"source"`
	Bytes   int64  `json:"bytes"`
	FirstAt string `json:"first_at"`
	LastAt  string `json:"last_at"`
	Extra   string `json:"extra"`
}

// RecordArtifact adds an entry, or updates the one with the same (task, kind, ref) and keeps when it first appeared.
// An empty label or extra keeps what the entry already had.
func (s *Store) RecordArtifact(a Artifact) error {
	if a.TaskID == "" || a.Kind == "" || a.Ref == "" {
		return nil
	}
	now := ts(s.now())
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO artifact (task_id, kind, ref, label, source, bytes, first_at, last_at, extra)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(task_id, kind, ref) DO UPDATE SET
				label = CASE WHEN excluded.label = '' THEN artifact.label ELSE excluded.label END,
				source = excluded.source, bytes = excluded.bytes, last_at = excluded.last_at,
				extra = CASE WHEN excluded.extra = '' THEN artifact.extra ELSE excluded.extra END`,
			a.TaskID, a.Kind, a.Ref, a.Label, a.Source, a.Bytes, now, now, a.Extra)
		return err
	})
}

// Artifacts is a card's list, newest first.
func (s *Store) Artifacts(taskID string) ([]Artifact, error) {
	out := []Artifact{}
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT task_id, kind, ref, label, source, bytes, first_at, last_at, extra
			FROM artifact WHERE task_id = ? ORDER BY last_at DESC, ref`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Artifact
			if err := rows.Scan(&a.TaskID, &a.Kind, &a.Ref, &a.Label, &a.Source, &a.Bytes, &a.FirstAt, &a.LastAt, &a.Extra); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
