## Test plan

## @LETTER@. lean_agents lets only the named agents run

### @LETTER@1. A built-in is refused

1. Launch a worker with `lean_agents: ["codebase-steward"]`.
2. Brief it to start an `Explore` subagent, then a `general-purpose` one.

**Expected:** both are refused. The refusal for a type the hook sees names `codebase-steward` as the allowed agent.

### @LETTER@2. The listed agent runs

1. In the same worker, brief it to start `atrium:codebase-steward`.

**Expected:** it starts.

### @LETTER@3. A built-in listed by bare name runs

1. Launch with `lean_agents: ["Explore"]`. No file named Explore is needed in `~/.claude/agents`.
2. Brief it to start `Explore`, then `Plan`.

**Expected:** `Explore` starts and `Plan` is refused.

### @LETTER@4. A launch without lean_agents is unchanged

1. Launch a lean worker with no `lean_agents`, and a non-lean worker.

**Expected:** the lean one has no Agent tool as before. The non-lean one starts any subagent.
