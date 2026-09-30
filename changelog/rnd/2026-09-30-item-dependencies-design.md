- docs/runtime/item-dependencies-design.md: work items that wait on other work, held on the hub. A gate clears only
  when the hub reads the work landed (its changelog entry on claude/main), a fixed check passes, or a human clears it.
  atrium_launch refuses a worker named for a blocked item. Design for r-new-item-dependencies, built by @runtime.
