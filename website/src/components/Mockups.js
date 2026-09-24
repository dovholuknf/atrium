import React from 'react';
import styles from './Mockups.module.css';

// A pending edit as the permissions pane draws it: who is asking, the diff with context dimmed, and the
// answers. The paths and code are made up.
export function PermMockup() {
  return (
    <div className={styles.perm} role="img"
      aria-label="A drawn permission request: an agent wants to edit a file, the diff is shown, with approve, always, block and never buttons.">
      <div className={styles.permHead}>
        <span className={styles.who}>fix-login</span>
        <span className={styles.tool}>Edit</span>
        <span className={styles.path}>internal/auth/session.go</span>
        <span className={styles.wait}>waiting 12s</span>
      </div>
      <pre className={styles.diff}>
        <span className={styles.ctx}>{'  func refresh(s *Session) error {\n'}</span>
        <span className={styles.ctx}>{'      if s.expired() {\n'}</span>
        <span className={styles.del}>{'-         return '}<mark>ErrExpired</mark>{'\n'}</span>
        <span className={styles.add}>{'+         return '}<mark>s.renew(ctx)</mark>{'\n'}</span>
        <span className={styles.ctx}>{'      }\n'}</span>
      </pre>
      <div className={styles.actions}>
        <span className={`${styles.btn} ${styles.approve}`}>approve</span>
        <span className={styles.btn}>always</span>
        <span className={`${styles.btn} ${styles.block}`}>block…</span>
        <span className={styles.btn}>never</span>
      </div>
    </div>
  );
}

// A supervised claude session in its browser terminal, wearing the active-work theme.
export function TerminalMockup() {
  return (
    <div className={styles.term} role="img"
      aria-label="A drawn browser terminal running an agent session that atrium supervises.">
      <div className={styles.termBar}>
        <span className={styles.termTitle}>sa01: schema</span>
        <span className={styles.termPath}>here/github/acme/api</span>
        <span className={styles.termChip}>active-work</span>
      </div>
      <pre className={styles.termBody}>
        <span className={styles.tDim}>{'● Read(internal/store/schema.go)\n'}</span>
        <span className={styles.tDim}>{'  ⎿  Read 214 lines\n\n'}</span>
        {'● The migration adds a column but never backfills it.\n'}
        {'  Adding the backfill in the same transaction.\n\n'}
        <span className={styles.tGreen}>{'● Bash(go test ./internal/store/...)\n'}</span>
        <span className={styles.tDim}>{'  ⎿  ok   internal/store   1.84s\n\n'}</span>
        <span className={styles.tMsg}>{'> [from you] also cover the empty-table case\n'}</span>
        <span className={styles.cursor}>{'> '}</span>
      </pre>
    </div>
  );
}
