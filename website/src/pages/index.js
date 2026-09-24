import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import CodeBlock from '@theme/CodeBlock';
import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';
import BoardMockup from '@site/src/components/BoardMockup';
import {PermMockup, TerminalMockup} from '@site/src/components/Mockups';
import styles from './index.module.css';

const questions = [
  ['Which one needs me?', 'A card that asked you something sorts above one that simply ran out of work.'],
  ['How long has it waited?', 'Every card carries its age, and the stack lists who has been blocked longest.'],
  ['What was I doing in there?', 'Every card keeps why it exists, an event log, and a recap when it finishes.'],
  ['Is it still alive?', 'Liveness is a question the operating system answers. No turn, no token.'],
];

const features = [
  {
    icon: 'gate',
    title: 'A gate on every tool call',
    body: 'A hook sends each call an agent wants to make to atrium and waits for you. Blocking hands your reason back, so a no is "do this instead", not a wall.',
    to: '/docs/permissions',
  },
  {
    icon: 'rules',
    title: 'Answer once, never again',
    body: 'Always and never become standing rules: a command prefix, a glob, or a folder. Import the allow list Claude Code already has and start with a hundred.',
    to: '/docs/permissions#standing-rules',
  },
  {
    icon: 'auto',
    title: 'Auto mode, with the receipt',
    body: 'Stop being asked for a card, the board, or the next hour. Everything is still recorded, and "what did it do?" reads it back grouped by tool.',
    to: '/docs/permissions#auto-mode',
  },
  {
    icon: 'badge',
    title: 'What each one is doing now',
    body: 'Thinking, running Bash, three subagents, and for how long. "Running Bash for 40 minutes" is a thing no status column can tell you.',
    to: '/docs/cards#the-live-badge',
  },
  {
    icon: 'term',
    title: 'Terminals atrium owns',
    body: 'claude, codex, gemini, ollama or a bare shell, under a pseudo terminal atrium holds. Attach from any browser, type, pop it into its own window.',
    to: '/docs/terminals',
  },
  {
    icon: 'switch',
    title: 'A switcher on one key',
    body: 'ctrl-shift-k, a few letters, Enter. The sessions you visited last come first, so bouncing between two is one keystroke.',
    to: '/docs/board#the-switcher',
  },
  {
    icon: 'msg',
    title: 'Say something to a running session',
    body: 'Queue a message and it arrives the next time the session can hear it, typed in or carried by a hook. Sessions ask each other through atrium too.',
    to: '/docs/messages',
  },
  {
    icon: 'bell',
    title: 'Notifications with buttons',
    body: 'A desktop notification carries approve and block, and the card\'s own mark and sound. No atrium tab has to be open.',
    to: '/docs/history#notifications',
  },
  {
    icon: 'rooms',
    title: 'Many machines, one board',
    body: 'A hub serves the board and holds nothing. Rooms run the agents and dial in over mutual TLS, zrok or OpenZiti. Restart the hub freely.',
    to: '/docs/rooms',
  },
  {
    icon: 'share',
    title: 'Lend one session to one person',
    body: 'Share a single terminal on its own address, behind an allowlist handler that refuses everything else, read-only enforced on the socket.',
    to: '/docs/overlays#lend-one-session',
  },
  {
    icon: 'files',
    title: 'Files in and out',
    body: 'Drop a file on a terminal to send it in. Click a path the agent printed to open it in your browser. Everything stays inside the card\'s directory.',
    to: '/docs/files',
  },
  {
    icon: 'history',
    title: 'A history you can search',
    body: 'Every card ever run, every decision with who asked and who answered: you, a rule, or auto mode. Export it as JSON or CSV.',
    to: '/docs/history',
  },
];

const principles = [
  ['Answer everything that does not need a model.', 'Liveness, rule matches, what an edit changes. Spending a turn on any of them is a bug.'],
  ['A hook never fails a session.', 'When atrium is down the permission hook fails open and every other hook is ignored.'],
  ['A card outlives its process.', 'It survives the runner exiting, a restart and the conversation. A pid is a reconnect hint.'],
  ['Loopback, always.', 'Reaching the board from elsewhere is an overlay\'s job. Atrium never holds an identity or proxies.'],
];

function Icon({name}) {
  const paths = {
    gate: 'M12 3l7 3v5c0 4.5-3 8.3-7 10-4-1.7-7-5.5-7-10V6l7-3zm-3 9l2 2 4-4',
    rules: 'M5 6h14M5 12h10M5 18h6M17 15l2 2 3-4',
    auto: 'M13 3L5 14h6l-1 7 8-11h-6l1-7z',
    badge: 'M12 7v5l3 2M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
    term: 'M4 5h16v14H4zM7 10l3 2-3 2M12 15h5',
    switch: 'M4 7h13l-3-3M20 17H7l3 3',
    msg: 'M4 5h16v11H9l-5 4V5zM8 10h8M8 13h5',
    bell: 'M6 16V11a6 6 0 1112 0v5l2 2H4l2-2zM10 20a2 2 0 004 0',
    rooms: 'M3 10l9-6 9 6M5 10v9h14v-9M9 19v-5h6v5',
    share: 'M8 12a3 3 0 11-6 0 3 3 0 016 0zM22 6a3 3 0 11-6 0 3 3 0 016 0zM22 18a3 3 0 11-6 0 3 3 0 016 0zM7.7 10.7l8.6-3.4M7.7 13.3l8.6 3.4',
    files: 'M6 3h8l4 4v14H6zM14 3v4h4M12 11v6M9 14l3 3 3-3',
    history: 'M3 12a9 9 0 103-6.7L3 8M3 3v5h5M12 8v4l3 2',
  };
  return (
    <svg className={styles.icon} viewBox="0 0 24 24" aria-hidden="true">
      <path d={paths[name]} />
    </svg>
  );
}

function Hero() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <div className={styles.heroGlow} aria-hidden="true" />
      <div className="container">
        <div className={styles.heroText}>
          <span className={styles.kicker}>atrium {siteConfig.customFields.release} · one machine · one person · no cloud</span>
          <h1 className={styles.title}>
            <span className={styles.line}>Every agent you run,</span>{' '}
            <span className={`${styles.line} accent-text`}>on one board you can answer.</span>
          </h1>
          <p className={styles.lede}>
            Half a dozen agents in half a dozen terminals, none aware of the others. Atrium is the hall they all open
            onto: it shows which one needs you, gates every tool call they make, and lets you step into any of their
            terminals from a browser.
          </p>
          <div className={styles.ctas}>
            <Link className={`${styles.cta} ${styles.ctaPrimary}`} to="/docs/quick-start">
              Get started
            </Link>
            <Link className={styles.cta} to="/docs/story">
              Why it exists
            </Link>
          </div>
        </div>
        <div className={styles.heroBoard}>
          <BoardMockup />
        </div>
      </div>
    </header>
  );
}

function Questions() {
  return (
    <section className={styles.section}>
      <div className="container">
        <p className={styles.eyebrow}>The four questions</p>
        <h2 className={styles.h2}>Stop alt-tabbing to ask them.</h2>
        <div className={styles.qgrid}>
          {questions.map(([q, a], i) => (
            <div key={q} className={styles.qcard}>
              <span className={styles.qnum}>0{i + 1}</span>
              <h3>{q}</h3>
              <p>{a}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Gate() {
  return (
    <section className={`${styles.section} ${styles.split}`}>
      <div className="container">
        <div className={styles.splitGrid}>
          <div>
            <p className={styles.eyebrow}>Permissions</p>
            <h2 className={styles.h2}>See the change, not just the file.</h2>
            <p className={styles.p}>
              A pending edit arrives as a real diff, unchanged context dimmed and the changed words picked out.
              Every request says which agent is asking, because the same command means different things from
              different sessions.
            </p>
            <ul className={styles.ticks}>
              <li><b>always</b> and <b>never</b> become rules. The most specific one wins.</li>
              <li>A <b>folder</b> rule covers relative commands run inside it, like <code>go test ./...</code>.</li>
              <li><b>Auto mode</b> never overrides a never rule or a shelved card.</li>
            </ul>
            <Link className={styles.more} to="/docs/permissions">How the permission chain decides →</Link>
          </div>
          <PermMockup />
        </div>
      </div>
    </section>
  );
}

function Features() {
  return (
    <section className={styles.section}>
      <div className="container">
        <p className={styles.eyebrow}>What it gives you</p>
        <h2 className={styles.h2}>Built for the hours you are not watching.</h2>
        <div className={styles.fgrid}>
          {features.map((f) => (
            <Link key={f.title} to={f.to} className={styles.fcard}>
              <Icon name={f.icon} />
              <h3>{f.title}</h3>
              <p>{f.body}</p>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

function Modes() {
  return (
    <section className={`${styles.section} ${styles.split}`}>
      <div className="container">
        <p className={styles.eyebrow}>Two ways in</p>
        <h2 className={styles.h2}>Keep your terminal, or let atrium hold it.</h2>
        <div className={styles.modes}>
          <div className={styles.mode}>
            <span className={styles.modeTag}>watch and gate</span>
            <h3>Your terminal, atrium's eyes</h3>
            <p>
              Start claude wherever you like. Its hooks report in, it gets a card on the board and the stack, and
              every tool call is gated. Atrium never touches the terminal, so nothing about how you work changes.
            </p>
            <CodeBlock language="powershell">{'atrium join     # or wire the hooks once from the board'}</CodeBlock>
          </div>
          <div className={`${styles.mode} ${styles.modeWork}`}>
            <span className={`${styles.modeTag} ${styles.modeTagWork}`}>supervised</span>
            <h3>Atrium's terminal, anywhere</h3>
            <p>
              Launch from the board and atrium runs the agent under a pseudo terminal it owns. Attach from any
              browser, type into it, pop it out, restart it onto the same card, and let messages be typed in for you.
            </p>
            <TerminalMockup />
          </div>
        </div>
        <Link className={styles.more} to="/docs/modes">Which one to pick →</Link>
      </div>
    </section>
  );
}

function Principles() {
  return (
    <section className={styles.section}>
      <div className="container">
        <p className={styles.eyebrow}>Rules it keeps</p>
        <h2 className={styles.h2}>Small on purpose.</h2>
        <div className={styles.pgrid}>
          {principles.map(([t, b]) => (
            <div key={t} className={styles.pcard}>
              <h3>{t}</h3>
              <p>{b}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function QuickStart() {
  return (
    <section className={`${styles.section} ${styles.split}`}>
      <div className="container">
        <div className={styles.splitGrid}>
          <div>
            <p className={styles.eyebrow}>Quick start</p>
            <h2 className={styles.h2}>Running in a minute.</h2>
            <ol className={styles.steps}>
              <li>Install a package from the release, or build from source.</li>
              <li>Open <code>http://localhost:7778</code>.</li>
              <li><b>rooms → hooks</b> writes the Claude Code reporting hooks for you.</li>
              <li>Add the permission gate hook, and every tool call waits for you.</li>
              <li><b>perms → import rules from claude</b> brings your allow list across.</li>
            </ol>
            <Link className={styles.more} to="/docs/install">Every install path →</Link>
          </div>
          <div className={styles.qs}>
            <Tabs groupId="os">
              <TabItem value="win" label="Windows">
                <CodeBlock language="powershell">
                  {'# per user, no admin\n.\\scripts\\atrium-service.ps1 install\n.\\scripts\\atrium-service.ps1 status\n\n# or run it by hand\natrium daemon'}
                </CodeBlock>
              </TabItem>
              <TabItem value="linux" label="Linux">
                <CodeBlock language="bash">
                  {'sudo dpkg -i atrium_0.0.1_amd64.deb\n# or: sudo dnf install ./atrium-0.0.1-1.x86_64.rpm\nsystemctl --user status atrium'}
                </CodeBlock>
              </TabItem>
              <TabItem value="mac" label="macOS">
                <CodeBlock language="bash">
                  {'tar -xzf atrium_v0.0.1_darwin_arm64.tar.gz\ncp atrium_v0.0.1_darwin_arm64/atrium ~/.local/bin/\ncd atrium_v0.0.1_darwin_arm64\nATRIUM_EXE=$HOME/.local/bin/atrium scripts/atrium-service.sh install'}
                </CodeBlock>
              </TabItem>
              <TabItem value="src" label="From source">
                <CodeBlock language="bash">{'go build -o build.claude/ ./...\n./build.claude/atrium daemon'}</CodeBlock>
              </TabItem>
            </Tabs>
          </div>
        </div>
      </div>
    </section>
  );
}

function Story() {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.story}>
          <p className={styles.eyebrow}>Where it came from</p>
          <h2 className={styles.h2}>It started as a chat window. The board is what it learned.</h2>
          <p className={styles.p}>
            The first atrium was a terminal you typed prompts into, relayed to agents that waited in a loop. It
            worked, and it answered the wrong question. The one that cost time was "what do I have running, and
            which one needs me". Everything since is the answer to that, one problem at a time.
          </p>
          <Link className={`${styles.cta} ${styles.ctaPrimary}`} to="/docs/story">
            Read the story
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home() {
  return (
    <Layout
      title="One board for every coding agent"
      description="Atrium puts every coding agent session on one board: what needs you, a gate on every tool call, and the agents' terminals in your browser.">
      <Hero />
      <main>
        <Questions />
        <Gate />
        <Features />
        <Modes />
        <Principles />
        <QuickStart />
        <Story />
      </main>
    </Layout>
  );
}
