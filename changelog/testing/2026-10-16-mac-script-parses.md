- **TestBoardScriptParses is one node, not two dozen.** It started node once per board script, then once more for the
  joined file, and on a macOS runner that went past its time ceiling. It now starts node once and parses each file and
  the concatenation with `vm.Script`, which parses without running as `--check` does. The run fell to about 0.07s, so
  its allow entry is deleted.
