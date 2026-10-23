A superlean worker gets its launch prompt again. The tool list was written `--tools <list>`, and with no model or
effort after it Claude Code read the prompt as one more tool, so the worker opened at an empty prompt while the launch
said running. A claude launch whose prompt an option would take that way is now refused before anything starts, and
the refusal names the option. f-superlean-prompt.
