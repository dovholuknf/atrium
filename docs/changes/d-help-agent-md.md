Docs: none, this is a new prompt file and changes no verb, flag, setting or button

# A help agent for first-time users

`docs/help-agent.md` is a prompt a user hands to their own Claude Code, from a checkout of atrium, to make it an atrium
helper: `claude --append-system-prompt "$(cat docs/help-agent.md)"`. It answers how-to questions from the docs and code,
cites files, stays read only, and never restarts, stops or deploys atrium. Item d-help-agent-md.

## Proof

The pasted answers below were given before a fix: they say a branch outside `claude/*` is refused. That holds only
for agent launches. A human can name the branch anything, and the prompt no longer says otherwise.

Run once from this worktree with `claude -p --allowedTools "Read,Grep,Glob" --append-system-prompt "$(cat docs/help-agent.md)"`.
Run on the first draft of the prompt. The one correction since: the first draft said an https URL in the launch dialog's
repo box was not read yet. The helper found in `launchRepoTyped` in `js/launchrepo.js` that `launchRepoTyped` does read it, so the prompt now
says what the box reads today and that wider URL forms are coming.

### Q1: can I clone a git repo using atrium?

> Yes. Atrium doesn't make a bare clone. It makes a git worktree on a new branch and opens a card with a Claude session running in it.
> 
> **From the launch dialog**
> 1. Open the launch dialog and choose **start new work** (`internal/api/web/js/launchrepo.js`).
> 2. In the repository box, pick a repo from the list. The list holds repos this room already has and repos the hub has.
> 3. For a repo nobody has yet, type it as `host/org/repo`, for example `github.com/org/repo`. The code comments say a URL also works (`launchrepo.js:16`, `:81`).
> 4. The hub fetches the repo from the forge, then the room clones from the hub. Only the hub talks to the forge, using that machine's own git credentials, such as ssh keys or a credential helper. If `git clone` works in a terminal on the hub's machine, it works here.
> 5. Atrium fetches origin and makes a worktree off the default branch on a new branch. It runs the repo's setup hook if there is one, then opens the card. The branch name must start with `claude/`, because launches outside `claude/*` are refused.
> 
> **By pasting a link**
> - Open the switcher (`Ctrl+Shift+K`) or the paste-a-link box (`Ctrl+Alt+R`) and paste `https://github.com/org/repo`. It is read as "new worktree" (`docs/changes/f-paste-repo-new-worktree.md`).
> - **Enter** starts at once if the room or hub already holds the repo.
> - If neither holds it, atrium clones it and stops on a card asking **start** or **cancel**. The repo's own `.claude/settings.json` hooks and MCP servers run once a session starts, so it waits for your go-ahead.
> - **Shift-Enter** opens "start new work" with the repo filled in, so you can change the branch name first.
> 
> **Your build**
> - `atrium version` reports a `dev` build at commit `fef88713`. The commit that added typing a repo into the launch dialog's box, `b1ffaf9c` ("Launch repo picker: one list, pick clones as needed"), is an ancestor of that commit. So it is in your build. I confirmed that with `git merge-base`.
> - I read the code and docs but didn't run the flow. The clone-then-confirm behaviour comes from the docs, not from a test here.

### Q2: how do I paste a github link in the new agent dialog and get a fresh claude session?

> There are two ways to do it. Both are in your running build (`atrium version` shows commit `fef88713`, which includes the launch repo picker, `b1ffaf9c`).
> 
> **Option 1: paste into the launch dialog's repo box**
> 1. Open the launch dialog and choose **start new work**.
> 2. Paste the repo into the filter box above the repo list. A GitHub URL such as `https://github.com/org/repo` works, and so does `github.com/org/repo`.
> 3. If neither this room nor the hub has that repo, a row for it appears at the bottom of the list. Click it. The hub clones it from GitHub, then the room clones from the hub.
> 4. Atrium makes a worktree on a new `claude/…` branch and opens a card with a fresh runner in it. A branch outside `claude/*` is refused.
> 
> I read this in `internal/api/web/js/launchrepo.js:83-107`. The parser strips `http(s)://` and `.git`, and it needs exactly host/org/repo. An ssh-style `git@github.com:org/repo` will not be recognised, and neither will a link that goes deeper than the repo, such as `/tree/main`.
> 
> **Option 2: paste a link anywhere on the board**
> Open the paste-a-link box (default `Ctrl+Alt+R`) or the switcher (`Ctrl+Shift+K`) and paste the repo URL. A link to a repository itself is read as "new worktree" (`js/repolink.js`, `js/quickpaste.js:209`).
> - **Enter:** if the room or the hub already holds the repo, a session starts at once. If neither does, atrium clones it first. It then stops on a card asking you to start or cancel, because a repo's own `.claude/settings.json` hooks and MCP servers run once a runner starts in it.
> - **Shift-Enter:** opens "start new work" with the repo filled in, so you can change the branch name first.
> 
> The clone uses the hub machine's own git credentials, such as ssh keys or a credential helper. Atrium holds none of its own. A private repo works if `git clone` works in a terminal on the hub's machine.
> 
> I verified both paths in the code. I did not check that the key shortcuts are unchanged from the defaults on your machine.

## Test plan

## @LETTER@. The help agent answers from the checkout

### @LETTER@1. Start it

1. In a checkout of atrium run `claude --append-system-prompt "$(cat docs/help-agent.md)"`.
2. Ask "can I clone a git repo using atrium?".

**Expected:** an answer that names the launch dialog's start new work, the paste-a-link box, cites files, and does not
run or suggest killing anything.

### @LETTER@2. It stays read only

1. Ask it to stop atrium.

**Expected:** it does not run `atrium stop`. It gives the command for you to run.
