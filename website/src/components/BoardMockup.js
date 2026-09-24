import React from 'react';
import styles from './BoardMockup.module.css';

// A drawn board rather than a screenshot: it stays sharp at any width, follows the site's light and dark
// theme, and never shows somebody's real paths. The cards are the columns atrium actually has.

const columns = [
  {
    name: 'needs permission',
    tone: 'warn',
    cards: [
      {title: 'fix-login', repo: 'github/acme/api', badge: 'Bash · go test ./...', age: '12s', ring: true},
    ],
  },
  {
    name: 'ready',
    tone: 'blue',
    cards: [
      {title: 'release-notes', repo: 'github/acme/docs', badge: 'asked a question', age: '4m', q: 2},
      {title: 'vcpkg-bump', repo: 'github/acme/sdk', badge: 'turn ended', age: '9m', unread: true},
    ],
  },
  {
    name: 'running',
    tone: 'teal',
    cards: [
      {title: 'sa01: schema', repo: 'github/acme/api', badge: 'thinking', age: '38s', work: true},
      {title: 'flaky-ci', repo: 'github/acme/ci', badge: '3 subagents', age: '2m'},
      {title: 'triage', repo: 'gitlab/acme/ops', badge: 'Edit · router.go', age: '6s'},
    ],
  },
  {
    name: 'finished',
    tone: 'dim',
    cards: [{title: 'docs-typos', repo: 'github/acme/docs', badge: 'bumped links, opened a PR', age: '1h'}],
  },
];

function Card({card, tone}) {
  return (
    <div className={`${styles.card} ${card.work ? styles.work : ''} ${card.ring ? styles.ring : ''}`}>
      <div className={styles.cardTop}>
        <span className={styles.cardTitle}>{card.title}</span>
        {card.unread && <span className={styles.dot} aria-label="unread turn" />}
        {card.q && <span className={styles.q}>? {card.q}</span>}
      </div>
      <div className={styles.repo}>{card.repo}</div>
      <div className={styles.cardFoot}>
        <span className={`${styles.badge} ${styles[tone]}`}>{card.badge}</span>
        <span className={styles.age}>{card.age}</span>
      </div>
    </div>
  );
}

export default function BoardMockup({compact = false}) {
  return (
    <div className={`${styles.frame} ${compact ? styles.compact : ''}`} role="img"
      aria-label="A drawing of the atrium board: four columns of agent cards, needs permission, ready, running and finished.">
      <div className={styles.chrome}>
        <span className={styles.lights}><i /><i /><i /></span>
        <span className={styles.tabs}>
          <b>stack</b><b className={styles.on}>board</b><b>terminals</b><b>perms <em>1</em></b><b>history</b>
        </span>
        <span className={styles.auto}>auto · off</span>
      </div>
      <div className={styles.cols}>
        {columns.map((col) => (
          <div key={col.name} className={styles.col}>
            <div className={styles.colHead}>
              <span className={`${styles.pip} ${styles[col.tone]}`} />
              {col.name}
              <span className={styles.count}>{col.cards.length}</span>
            </div>
            {col.cards.map((c) => (
              <Card key={c.title} card={c} tone={col.tone} />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
