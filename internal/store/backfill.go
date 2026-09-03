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
		if t.Worktree == "" {
			continue
		}
		git := ReadGitInfo(t.Worktree)
		// Not in a repository, or the directory has gone. Either way there is
		// nothing better to call it than whatever it is called now.
		if git.Repo == "" {
			continue
		}
		title := t.Title
		if _, override := t.Overrides["title"]; !override {
			title = TitleFor(git, t.Worktree, t.Title)
		}
		// Recomputed rather than filled in once. The naming rule itself gets
		// corrected, and a card fixed by an earlier version would otherwise
		// keep a name that version chose and this one would not. Deterministic
		// from the directory, so doing it every startup costs two file reads
		// per card and changes nothing when nothing changed.
		if t.Repo == git.Repo && t.Branch == git.Branch && t.Title == title {
			continue
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
