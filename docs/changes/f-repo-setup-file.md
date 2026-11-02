Docs: none, stage 1 of a design in docs/design/repo-setup-file.md, no reference page names it yet

`.atrium/setup.toml` in a repo lists what it needs. A launching room probes `bin` (with `>=` versions), `file` and `env`
needs before the agent starts. A missing `required = true` need refuses the launch with the list. Any other missing
need is a warning block in the brief, and the brief carries `Read .atrium/setup.toml if the repo has one.`
