package store

// Naming cards that were created before atrium asked git.
//
// Run once at startup rather than lazily on registration. Registration is on
// the hot path of every hook of every session, and a card whose directory is
// in no repository would otherwise re-answer that question forever. Bounded by
// the number of cards, and each answer is two small file reads.

// BackfillGitInfo fills in repo and branch, and renames what atrium had only
// guessed at.
//
// A title the operator overrode is left alone, because an override beating an
// observed value is the rule the whole card is built on. A title that is still
// whatever atrium last guessed is fair game: it was a guess, it was wrong, and
// nobody chose it.
func (s *Store) BackfillGitInfo() (int, error) {
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	fixed := 0
	for _, t := range tasks {
		if t.Worktree == "" || t.Repo != "" {
			continue
		}
		git := ReadGitInfo(t.Worktree)
		if git.Repo == "" {
			continue
		}
		title := t.Title
		if _, override := t.Overrides["title"]; !override {
			title = TitleFor(git.Repo, git.Branch, t.Worktree, t.Title)
		}
		err := s.guard(func() error {
			_, err := s.db.Exec(
				`UPDATE task SET repo = ?, branch = ?, title = ? WHERE id = ?`,
				git.Repo, git.Branch, title, t.ID)
			return err
		})
		if err != nil {
			return fixed, err
		}
		fixed++
	}
	return fixed, nil
}
