// ── terminal themes ─────────────────────────────────────
//
// Ported from the operator's own Windows Terminal themes, which are applied
// over OSC escapes from a PowerShell profile and have been in daily use far
// longer than this board has existed. Same names, same colors, so a session
// looks the same in here as it does in a terminal window.
//
// Converted rather than transcribed: the source is a table of `bg`, `fg`,
// `cursor`, `sel_bg`, `sel_fg` and a positional 16 color ansi array, and the
// array order is the one xterm has used since forever. Doing that by hand
// fifty two times is how one color ends up in the wrong slot.
const TERM_THEMES = {
  // The board's own palette, for a terminal that should not look pasted in.
  "atrium": {
    background: "#040B14", foreground: "#D7DBE3", cursor: "#00E3B0",
    selectionBackground: "#173A57"
  },
  "active-light": {background: "#1c5a2b", foreground: "#edf8ed", cursor: "#9affa6", selectionBackground: "#3f8250", selectionForeground: "#f4fbec", black: "#123a1e", red: "#ff9a92", green: "#90ec90", yellow: "#f2d888", blue: "#94aef2", magenta: "#ec9ce0", cyan: "#7ce4d2", white: "#e4f2e4", brightBlack: "#8aac8a", brightRed: "#ffaba2", brightGreen: "#b8f8b8", brightYellow: "#ffe6a0", brightBlue: "#b0ccff", brightMagenta: "#f4bcec", brightCyan: "#a6f8e8", brightWhite: "#f4fbec"},
  "active-work": {background: "#0f3d1a", foreground: "#e0f0e0", cursor: "#80ff90", selectionBackground: "#2a6a38", selectionForeground: "#f0f8e8", black: "#0a2010", red: "#e06868", green: "#78d878", yellow: "#e0c070", blue: "#7098e0", magenta: "#d088c8", cyan: "#68d0c0", white: "#d0e0d0", brightBlack: "#6a906a", brightRed: "#ff8888", brightGreen: "#a8f0a8", brightYellow: "#f0d890", brightBlue: "#98b8f0", brightMagenta: "#e0a8d8", brightCyan: "#90f0e0", brightWhite: "#f0f8e8"},
  "ad-hoc": {background: "#2e3440", foreground: "#d8dee9", cursor: "#88c0d0", selectionBackground: "#434c5e", selectionForeground: "#eceff4", black: "#3b4252", red: "#bf616a", green: "#a3be8c", yellow: "#ebcb8b", blue: "#81a1c1", magenta: "#b48ead", cyan: "#88c0d0", white: "#e5e9f0", brightBlack: null, brightRed: "#bf616a", brightGreen: "#a3be8c", brightYellow: "#ebcb8b", brightBlue: "#81a1c1", brightMagenta: "#b48ead", brightCyan: "#8fbcbb", brightWhite: null},
  "admin-caution": {background: "#3d2e0f", foreground: "#fff4d0", cursor: "#ffcc00", selectionBackground: "#705618", selectionForeground: "#fffae0", black: "#2a1f08", red: "#ff6b6b", green: "#cad669", yellow: "#ffaa00", blue: "#7fa9c8", magenta: "#d29be8", cyan: "#7ed4c2", white: "#fff4d0", brightBlack: null, brightRed: "#ff8a8a", brightGreen: "#dde888", brightYellow: "#ffcc44", brightBlue: "#9cc5dc", brightMagenta: "#e0b5f0", brightCyan: "#a3f0e0", brightWhite: null},
  "admin-danger": {background: "#3a1b08", foreground: "#ffe8c4", cursor: "#ff9933", selectionBackground: "#6b3514", selectionForeground: "#fff4d6", black: "#2a1206", red: "#ff5c3c", green: "#d4c46a", yellow: "#ffaa33", blue: "#88b6c2", magenta: "#d18df0", cyan: "#7bd6c4", white: "#ffe8c4", brightBlack: null, brightRed: "#ff8866", brightGreen: "#e6dd80", brightYellow: "#ffcc55", brightBlue: "#a5d0dd", brightMagenta: "#e5b0ff", brightCyan: "#a8f0e0", brightWhite: null},
  "amber-dusk": {background: "#201505", foreground: "#f5ddb8", cursor: "#e07030", selectionBackground: "#3c2808", selectionForeground: "#ffedd0", black: "#140e02", red: "#f05038", green: "#80c470", yellow: "#eaac30", blue: "#6690c8", magenta: "#bc78bc", cyan: "#70beb0", white: "#f5ddb8", brightBlack: null, brightRed: "#f07858", brightGreen: "#98d488", brightYellow: "#f0c850", brightBlue: "#86b0e0", brightMagenta: "#d494d4", brightCyan: "#8ed0c4", brightWhite: null},
  "arctic-blue": {background: "#062838", foreground: "#c0ecf8", cursor: "#40d0f0", selectionBackground: "#0e3e52", selectionForeground: "#dcf8ff", black: "#041820", red: "#ff5c6e", green: "#58d888", yellow: "#f0d050", blue: "#40d0f0", magenta: "#c070e0", cyan: "#38d4c8", white: "#c0ecf8", brightBlack: null, brightRed: "#ff88a0", brightGreen: "#80ecaa", brightYellow: "#f8e070", brightBlue: "#70e4ff", brightMagenta: "#d898f0", brightCyan: "#60e8e0", brightWhite: null},
  "blood-moon": {background: "#1c0606", foreground: "#f8d8d0", cursor: "#ff5060", selectionBackground: "#380e0e", selectionForeground: "#ffe8e0", black: "#110303", red: "#ff4444", green: "#7abf72", yellow: "#e8b860", blue: "#7098d0", magenta: "#c878c0", cyan: "#72c0b8", white: "#f8d8d0", brightBlack: null, brightRed: "#ff7070", brightGreen: "#94d48c", brightYellow: "#f0cc80", brightBlue: "#8ab0e0", brightMagenta: "#da98d8", brightCyan: "#90d4cc", brightWhite: null},
  "cocoa": {background: "#2b1e16", foreground: "#e8d8c4", cursor: "#d8a373", selectionBackground: "#46322a", selectionForeground: "#f5e9d6", black: "#1f150f", red: "#cf5560", green: "#9bbf63", yellow: "#d8a373", blue: "#6c92ab", magenta: "#b986b5", cyan: "#76b8a8", white: "#e8d8c4", brightBlack: null, brightRed: "#e87580", brightGreen: "#b2d480", brightYellow: "#e6b88a", brightBlue: "#8baac0", brightMagenta: "#d09acb", brightCyan: "#92cdbd", brightWhite: null},
  "crimson-cave": {background: "#260a0a", foreground: "#f5d5d0", cursor: "#e85050", selectionBackground: "#441414", selectionForeground: "#ffe4dc", black: "#180606", red: "#e84040", green: "#78bc70", yellow: "#e8b460", blue: "#6890c8", magenta: "#c070b8", cyan: "#70bbb0", white: "#f5d5d0", brightBlack: null, brightRed: "#f07070", brightGreen: "#92d088", brightYellow: "#f0c878", brightBlue: "#88acd8", brightMagenta: "#d890d0", brightCyan: "#8ed0c8", brightWhite: null},
  "deep-amethyst": {background: "#100520", foreground: "#e0c8f8", cursor: "#9050d8", selectionBackground: "#200e40", selectionForeground: "#f0e0ff", black: "#0a0314", red: "#ff5878", green: "#7eca6e", yellow: "#e8bc50", blue: "#6e7eee", magenta: "#be5ee4", cyan: "#5ec4cc", white: "#e0c8f8", brightBlack: null, brightRed: "#ff80a0", brightGreen: "#9adc8a", brightYellow: "#f0cc68", brightBlue: "#8ea0ff", brightMagenta: "#d484f0", brightCyan: "#82d8e0", brightWhite: null},
  "deep-fern": {background: "#0c1e0c", foreground: "#d0e8c8", cursor: "#68c060", selectionBackground: "#183018", selectionForeground: "#e8f8e4", black: "#081208", red: "#f06868", green: "#68c060", yellow: "#d8b840", blue: "#6098d0", magenta: "#c282c8", cyan: "#62c0b8", white: "#d0e8c8", brightBlack: null, brightRed: "#f08c8c", brightGreen: "#88d080", brightYellow: "#e8cc68", brightBlue: "#82b4e4", brightMagenta: "#d4a4d8", brightCyan: "#88d0cc", brightWhite: null},
  "deep-ocean": {background: "#081828", foreground: "#c0d8f5", cursor: "#4898e0", selectionBackground: "#102840", selectionForeground: "#dceeff", black: "#050f18", red: "#ff6070", green: "#76ca76", yellow: "#e8bc50", blue: "#4898e0", magenta: "#bc78d8", cyan: "#5ec4c4", white: "#c0d8f5", brightBlack: null, brightRed: "#ff8898", brightGreen: "#94da94", brightYellow: "#f0cc68", brightBlue: "#78b4ec", brightMagenta: "#d498e8", brightCyan: "#82d8dc", brightWhite: null},
  "discourse": {background: "#3b1e15", foreground: "#f0d8c4", cursor: "#e07845", selectionBackground: "#5f3225", selectionForeground: "#ffe8d4", black: "#28140d", red: "#e8516a", green: "#b9c270", yellow: "#e07845", blue: "#8fa9bd", magenta: "#cf8fc4", cyan: "#7ec4b3", white: "#f0d8c4", brightBlack: null, brightRed: "#ff7589", brightGreen: "#c8cf85", brightYellow: "#f08e62", brightBlue: "#a8c0d2", brightMagenta: "#dca6cf", brightCyan: "#9bd2c0", brightWhite: null},
  "dracula": {background: "#282a36", foreground: "#f8f8f2", cursor: "#f8f8f2", selectionBackground: "#44475a", selectionForeground: "#f8f8f2", black: "#21222c", red: "#ff5555", green: "#50fa7b", yellow: "#f1fa8c", blue: "#bd93f9", magenta: "#ff79c6", cyan: "#8be9fd", white: "#f8f8f2", brightBlack: null, brightRed: "#ff6e6e", brightGreen: "#69ff94", brightYellow: "#ffffa5", brightBlue: "#d6acff", brightMagenta: "#ff92df", brightCyan: "#a4ffff", brightWhite: null},
  "dusk-blue": {background: "#100870", foreground: "#d0c8ff", cursor: "#8870ff", selectionBackground: "#1c1298", selectionForeground: "#e8e0ff", black: "#0a0648", red: "#ff5078", green: "#5ecc6e", yellow: "#f0c840", blue: "#8870ff", magenta: "#d060e0", cyan: "#50c8d4", white: "#d0c8ff", brightBlack: null, brightRed: "#ff80a8", brightGreen: "#86dc8c", brightYellow: "#f8d860", brightBlue: "#a898ff", brightMagenta: "#e080f4", brightCyan: "#78dce4", brightWhite: null},
  "electric-purple": {background: "#1e0060", foreground: "#e8d8ff", cursor: "#b060ff", selectionBackground: "#300090", selectionForeground: "#f4ecff", black: "#140040", red: "#ff5080", green: "#70d070", yellow: "#f0d050", blue: "#7060ff", magenta: "#e040d0", cyan: "#50d0e0", white: "#e8d8ff", brightBlack: null, brightRed: "#ff80a8", brightGreen: "#90e090", brightYellow: "#f8e068", brightBlue: "#9898ff", brightMagenta: "#f068e8", brightCyan: "#78e8f0", brightWhite: null},
  "forest-night": {background: "#0a1f12", foreground: "#d4e3d4", cursor: "#7fb069", selectionBackground: "#1f3a26", selectionForeground: "#e8f0e0", black: "#0a1610", red: "#c46b6b", green: "#7fb069", yellow: "#d4a373", blue: "#5b8a72", magenta: "#a3779e", cyan: "#76a89c", white: "#c8d4c0", brightBlack: null, brightRed: "#e08585", brightGreen: "#9bcc83", brightYellow: "#e8c087", brightBlue: "#7aa890", brightMagenta: "#bf95b8", brightCyan: "#94c4b8", brightWhite: null},
  "garnet": {background: "#1e0810", foreground: "#f0d5e0", cursor: "#d06070", selectionBackground: "#381020", selectionForeground: "#ffe4ec", black: "#130408", red: "#e84060", green: "#7abf78", yellow: "#e8b860", blue: "#7098c8", magenta: "#c878c0", cyan: "#72c0b8", white: "#f0d5e0", brightBlack: null, brightRed: "#f07085", brightGreen: "#94d48e", brightYellow: "#f0cc80", brightBlue: "#8ab0d8", brightMagenta: "#d898d8", brightCyan: "#90d0c8", brightWhite: null},
  "gruvbox-dark": {background: "#282828", foreground: "#ebdbb2", cursor: "#fe8019", selectionBackground: "#504945", selectionForeground: "#ebdbb2", black: "#1d2021", red: "#e05c4f", green: "#98971a", yellow: "#d79921", blue: "#458588", magenta: "#b16286", cyan: "#689d6a", white: "#a89984", brightBlack: null, brightRed: "#fb4934", brightGreen: "#b8bb26", brightYellow: "#fabd2f", brightBlue: "#83a598", brightMagenta: "#d3869b", brightCyan: "#8ec07c", brightWhite: null},
  "imperial-purple": {background: "#180050", foreground: "#dcd0ff", cursor: "#9070ff", selectionBackground: "#280880", selectionForeground: "#ede8ff", black: "#100038", red: "#ff5878", green: "#68cc68", yellow: "#eec850", blue: "#9070ff", magenta: "#c858e0", cyan: "#50ccd8", white: "#dcd0ff", brightBlack: null, brightRed: "#ff80a0", brightGreen: "#88dc88", brightYellow: "#f8d868", brightBlue: "#a8a0ff", brightMagenta: "#e07af0", brightCyan: "#78dce8", brightWhite: null},
  "ink-dark": {background: "#06080f", foreground: "#c8d0f0", cursor: "#6878d0", selectionBackground: "#101828", selectionForeground: "#dce4ff", black: "#040508", red: "#ff5c70", green: "#74c874", yellow: "#e6ba50", blue: "#6878d0", magenta: "#b876d0", cyan: "#5cc0c0", white: "#c8d0f0", brightBlack: null, brightRed: "#ff8090", brightGreen: "#92d892", brightYellow: "#f0ca68", brightBlue: "#8898e0", brightMagenta: "#d898e0", brightCyan: "#80d4d8", brightWhite: null},
  "jade-shadow": {background: "#081808", foreground: "#cce0cc", cursor: "#5ab860", selectionBackground: "#142814", selectionForeground: "#e4f4e0", black: "#050f05", red: "#ee6868", green: "#5ab860", yellow: "#d8b440", blue: "#5e96d0", magenta: "#c080c8", cyan: "#60beb8", white: "#cce0cc", brightBlack: null, brightRed: "#f08888", brightGreen: "#82cc80", brightYellow: "#e8c860", brightBlue: "#80b2e4", brightMagenta: "#d4a0d8", brightCyan: "#86ceca", brightWhite: null},
  "khaki-night": {background: "#181a04", foreground: "#eaecb0", cursor: "#a0a840", selectionBackground: "#2e3008", selectionForeground: "#f8fadc", black: "#0e1002", red: "#e05848", green: "#7ec870", yellow: "#c0a830", blue: "#6092cc", magenta: "#b878c4", cyan: "#6cbab2", white: "#eaecb0", brightBlack: null, brightRed: "#ee7868", brightGreen: "#98d88a", brightYellow: "#dac450", brightBlue: "#80aee0", brightMagenta: "#d098d8", brightCyan: "#8cccc4", brightWhite: null},
  "matrix": {background: "#020a02", foreground: "#a4ffa4", cursor: "#00ff66", selectionBackground: "#0c280c", selectionForeground: "#e8ffe8", black: "#031003", red: "#ff5c5c", green: "#00cc44", yellow: "#cccc44", blue: "#5dbbd6", magenta: "#a05fb0", cyan: "#3fbfaa", white: "#a4ffa4", brightBlack: null, brightRed: "#ff8080", brightGreen: "#33ff66", brightYellow: "#e8e858", brightBlue: "#88d4e6", brightMagenta: "#c089d2", brightCyan: "#65d8c3", brightWhite: null},
  "mauve-purple": {background: "#280038", foreground: "#f0d0f0", cursor: "#d060d0", selectionBackground: "#420060", selectionForeground: "#fce8ff", black: "#1a0026", red: "#ff5070", green: "#70cc70", yellow: "#f0cc50", blue: "#8870f0", magenta: "#d060d0", cyan: "#58ccd8", white: "#f0d0f0", brightBlack: null, brightRed: "#ff80a0", brightGreen: "#90dc90", brightYellow: "#f8dc68", brightBlue: "#a898ff", brightMagenta: "#e880e8", brightCyan: "#80dce8", brightWhite: null},
  "midnight-blue": {background: "#050820", foreground: "#c8d5f8", cursor: "#5580f0", selectionBackground: "#0e1640", selectionForeground: "#e0eaff", black: "#030510", red: "#ff6070", green: "#78cc78", yellow: "#e8c050", blue: "#5580f0", magenta: "#c07ae0", cyan: "#60c8c8", white: "#c8d5f8", brightBlack: null, brightRed: "#ff8898", brightGreen: "#96dc96", brightYellow: "#f0d068", brightBlue: "#80a8ff", brightMagenta: "#d89cf0", brightCyan: "#84dce0", brightWhite: null},
  "midnight-plum": {background: "#140520", foreground: "#e8c8f0", cursor: "#a060d0", selectionBackground: "#240e40", selectionForeground: "#f4e0ff", black: "#0e0314", red: "#ff587c", green: "#7ecc70", yellow: "#e8bc50", blue: "#7080ec", magenta: "#c060d8", cyan: "#60c4cc", white: "#e8c8f0", brightBlack: null, brightRed: "#ff82a8", brightGreen: "#9cdc8c", brightYellow: "#f0cc68", brightBlue: "#90a0ff", brightMagenta: "#d888ec", brightCyan: "#82d8e0", brightWhite: null},
  "mint": {background: "#9ad4a8", foreground: "#12301e", cursor: "#0d5a2a", selectionBackground: "#5fb277", selectionForeground: "#0a2413", black: "#12301e", red: "#a52828", green: "#1e7a33", yellow: "#8a6410", blue: "#1f50aa", magenta: "#8f2f82", cyan: "#0e746c", white: "#2a4634", brightBlack: "#4d6a55", brightRed: "#c03434", brightGreen: "#2a9440", brightYellow: "#a5760f", brightBlue: "#2660c6", brightMagenta: "#a83a98", brightCyan: "#118a80", brightWhite: "#0a2413"},
  "molten": {background: "#1e1005", foreground: "#f8e0c0", cursor: "#ff8040", selectionBackground: "#3a2008", selectionForeground: "#fff0d8", black: "#130a02", red: "#ff5040", green: "#82c870", yellow: "#f0b030", blue: "#6898d0", magenta: "#c07abe", cyan: "#72c0b0", white: "#f8e0c0", brightBlack: null, brightRed: "#ff8060", brightGreen: "#9cd888", brightYellow: "#f8cc58", brightBlue: "#88b4e0", brightMagenta: "#d898d4", brightCyan: "#90d4c4", brightWhite: null},
  "monokai": {background: "#272822", foreground: "#f8f8f2", cursor: "#f92672", selectionBackground: "#49483e", selectionForeground: "#f8f8f2", black: "#272822", red: "#f92672", green: "#a6e22e", yellow: "#f4bf75", blue: "#66d9ef", magenta: "#ae81ff", cyan: "#a1efe4", white: "#f8f8f2", brightBlack: null, brightRed: "#f92672", brightGreen: "#a6e22e", brightYellow: "#f4bf75", brightBlue: "#66d9ef", brightMagenta: "#ae81ff", brightCyan: "#a1efe4", brightWhite: null},
  "neon-grape": {background: "#1a0d2e", foreground: "#f3e8ff", cursor: "#c084fc", selectionBackground: "#2e1655", selectionForeground: "#faf5ff", black: "#100620", red: "#ff4d8d", green: "#a3e635", yellow: "#fbbf24", blue: "#60a5fa", magenta: "#c084fc", cyan: "#22d3ee", white: "#f3e8ff", brightBlack: null, brightRed: "#ff6ba1", brightGreen: "#bef264", brightYellow: "#fcd34d", brightBlue: "#7eb5fc", brightMagenta: "#d4a9ff", brightCyan: "#5ee6f5", brightWhite: null},
  "nord": {background: "#2e3440", foreground: "#d8dee9", cursor: "#88c0d0", selectionBackground: "#434c5e", selectionForeground: "#eceff4", black: "#3b4252", red: "#bf616a", green: "#a3be8c", yellow: "#ebcb8b", blue: "#81a1c1", magenta: "#b48ead", cyan: "#88c0d0", white: "#e5e9f0", brightBlack: null, brightRed: "#bf616a", brightGreen: "#a3be8c", brightYellow: "#ebcb8b", brightBlue: "#81a1c1", brightMagenta: "#b48ead", brightCyan: "#8fbcbb", brightWhite: null},
  "obsidian-grove": {background: "#051505", foreground: "#c8e8c0", cursor: "#50d050", selectionBackground: "#0e280e", selectionForeground: "#e4fae0", black: "#030e03", red: "#f06060", green: "#50d050", yellow: "#d8b840", blue: "#5898d0", magenta: "#c080c8", cyan: "#5cc0b8", white: "#c8e8c0", brightBlack: null, brightRed: "#f08888", brightGreen: "#80e478", brightYellow: "#e8cc60", brightBlue: "#80b4e4", brightMagenta: "#d8a0d8", brightCyan: "#84d4ce", brightWhite: null},
  "ocean-deep": {background: "#0d2440", foreground: "#cfe1f5", cursor: "#5cc4ff", selectionBackground: "#1e3a5f", selectionForeground: "#eaf3ff", black: "#091a30", red: "#ff6b7a", green: "#8ad48a", yellow: "#ffd166", blue: "#5cc4ff", magenta: "#c890ff", cyan: "#5fd1c1", white: "#cfe1f5", brightBlack: null, brightRed: "#ff8b95", brightGreen: "#a4dfa4", brightYellow: "#ffe188", brightBlue: "#7fd4ff", brightMagenta: "#dbb0ff", brightCyan: "#82e0d3", brightWhite: null},
  "old-gold": {background: "#1a1600", foreground: "#f5f0b8", cursor: "#d4aa20", selectionBackground: "#322c00", selectionForeground: "#fffff0", black: "#100e00", red: "#e85848", green: "#82c470", yellow: "#d4aa20", blue: "#6698d0", magenta: "#c080c8", cyan: "#70bfb8", white: "#f5f0b8", brightBlack: null, brightRed: "#f08070", brightGreen: "#9cd48a", brightYellow: "#e8cc50", brightBlue: "#86b2e0", brightMagenta: "#d4a0d8", brightCyan: "#8ed4cc", brightWhite: null},
  "orange-coral": {background: "#6b2a1b", foreground: "#f0d8c4", cursor: "#e07845", selectionBackground: "#5f3225", selectionForeground: "#ffe8d4", black: "#28140d", red: "#e8516a", green: "#b9c270", yellow: "#e07845", blue: "#8fa9bd", magenta: "#cf8fc4", cyan: "#7ec4b3", white: "#f0d8c4", brightBlack: null, brightRed: "#ff7589", brightGreen: "#c8cf85", brightYellow: "#f08e62", brightBlue: "#a8c0d2", brightMagenta: "#dca6cf", brightCyan: "#9bd2c0", brightWhite: null},
  "orange-marigold": {background: "#c47a14", foreground: "#3a2400", cursor: "#3a2400", selectionBackground: "#ffcf7a", selectionForeground: "#3a2400", black: "#200e02", red: "#6e0000", green: "#0f4415", yellow: "#3f3000", blue: "#082f66", magenta: "#4a1160", cyan: "#003b42", white: "#281505", brightBlack: null, brightRed: "#820000", brightGreen: "#0f4415", brightYellow: "#4a3800", brightBlue: "#0a3a7a", brightMagenta: "#571571", brightCyan: "#00474f", brightWhite: null},
  "orange-tangerine": {background: "#74310e", foreground: "#ffe2b8", cursor: "#ff8c1a", selectionBackground: "#7a3812", selectionForeground: "#fff1d6", black: "#321204", red: "#ff5a3a", green: "#c8c768", yellow: "#ff9933", blue: "#7fb2c4", magenta: "#d68fe8", cyan: "#7ad1bb", white: "#ffe2b8", brightBlack: null, brightRed: "#ff8866", brightGreen: "#dde088", brightYellow: "#ffb15a", brightBlue: "#a3cedc", brightMagenta: "#e6b3ee", brightCyan: "#a6e8d6", brightWhite: null},
  "pull-requests": {background: "#0a2a6a", foreground: "#e0e8f8", cursor: "#90b8ff", selectionBackground: "#3a5098", selectionForeground: "#f0f4f8", black: "#081a40", red: "#e07080", green: "#80d088", yellow: "#e0c070", blue: "#80a8ff", magenta: "#c080d0", cyan: "#70c8e0", white: "#c8d0e0", brightBlack: "#6888b8", brightRed: "#ff8898", brightGreen: "#a0e0a8", brightYellow: "#f0d890", brightBlue: "#a0c0ff", brightMagenta: "#e0a0e0", brightCyan: "#90e0f0", brightWhite: "#f0f4f8"},
  "pumpkin": {background: "#4a1f08", foreground: "#ffe2b8", cursor: "#ff8c1a", selectionBackground: "#7a3812", selectionForeground: "#fff1d6", black: "#321204", red: "#ff5a3a", green: "#c8c768", yellow: "#ff9933", blue: "#7fb2c4", magenta: "#d68fe8", cyan: "#7ad1bb", white: "#ffe2b8", brightBlack: null, brightRed: "#ff8866", brightGreen: "#dde088", brightYellow: "#ffb15a", brightBlue: "#a3cedc", brightMagenta: "#e6b3ee", brightCyan: "#a6e8d6", brightWhite: null},
  "pure-blue": {background: "#080890", foreground: "#c8d4ff", cursor: "#6888ff", selectionBackground: "#1010b8", selectionForeground: "#e0e8ff", black: "#060660", red: "#ff5070", green: "#60cc70", yellow: "#f0c840", blue: "#6888ff", magenta: "#c060e8", cyan: "#50c8d8", white: "#c8d4ff", brightBlack: null, brightRed: "#ff80a0", brightGreen: "#88dc90", brightYellow: "#f8d860", brightBlue: "#90b0ff", brightMagenta: "#d888f4", brightCyan: "#78dce8", brightWhite: null},
  "rosewood": {background: "#2c1216", foreground: "#f0d8d8", cursor: "#ff8d8d", selectionBackground: "#4b1f25", selectionForeground: "#ffe6e6", black: "#1a0a0c", red: "#ff5a6e", green: "#a8c285", yellow: "#e8b66e", blue: "#7fa3c2", magenta: "#c790c8", cyan: "#7dbfb5", white: "#f0d8d8", brightBlack: null, brightRed: "#ff7a8d", brightGreen: "#bbd09a", brightYellow: "#f0c987", brightBlue: "#9cb9d2", brightMagenta: "#d8a9d8", brightCyan: "#9ed3c9", brightWhite: null},
  "shadow-realm": {background: "#0c0520", foreground: "#ddc8f8", cursor: "#9060f0", selectionBackground: "#1c1040", selectionForeground: "#eedfff", black: "#070314", red: "#ff5070", green: "#80cc70", yellow: "#e8c050", blue: "#7080f0", magenta: "#c060e8", cyan: "#60c8d0", white: "#ddc8f8", brightBlack: null, brightRed: "#ff7898", brightGreen: "#9cdc8c", brightYellow: "#f0d068", brightBlue: "#9098ff", brightMagenta: "#d888f4", brightCyan: "#80dce4", brightWhite: null},
  "solarized-dark": {background: "#002b36", foreground: "#93a1a1", cursor: "#93a1a1", selectionBackground: "#073642", selectionForeground: "#eee8d5", black: "#073642", red: "#dc322f", green: "#859900", yellow: "#b58900", blue: "#268bd2", magenta: "#d33682", cyan: "#2aa198", white: "#eee8d5", brightBlack: null, brightRed: "#cb4b16", brightGreen: "#586e75", brightYellow: "#657b83", brightBlue: "#839496", brightMagenta: "#6c71c4", brightCyan: "#93a1a1", brightWhite: null},
  "synthwave": {background: "#241b2f", foreground: "#ffffff", cursor: "#ff7edb", selectionBackground: "#3d2c4e", selectionForeground: "#ffffff", black: "#1a1027", red: "#fe4450", green: "#72f1b8", yellow: "#fede5d", blue: "#36f9f6", magenta: "#ff7edb", cyan: "#03edf9", white: "#ffffff", brightBlack: null, brightRed: "#ff5874", brightGreen: "#94f3c5", brightYellow: "#fff35c", brightBlue: "#52f6f4", brightMagenta: "#ff8eda", brightCyan: "#41e9f5", brightWhite: null},
  "tangent": {background: "#5a0f1a", foreground: "#f8e0e0", cursor: "#ff90a0", selectionBackground: "#8a2a38", selectionForeground: "#f8e8e8", black: "#300810", red: "#ff7888", green: "#a8c878", yellow: "#e8b858", blue: "#80a0e8", magenta: "#e090c0", cyan: "#60c8c0", white: "#e8c8c8", brightBlack: "#b07880", brightRed: "#ff98a8", brightGreen: "#c0e080", brightYellow: "#f8d080", brightBlue: "#a0b8ff", brightMagenta: "#f0a0d0", brightCyan: "#80e0e0", brightWhite: "#f8e8e8"},
  "tarnished": {background: "#1c1808", foreground: "#f0eab8", cursor: "#c8a030", selectionBackground: "#342e10", selectionForeground: "#fffce0", black: "#100e04", red: "#e45848", green: "#80c070", yellow: "#c8a030", blue: "#6494cc", magenta: "#bc7cc4", cyan: "#6ebcb4", white: "#f0eab8", brightBlack: null, brightRed: "#f07868", brightGreen: "#98d08a", brightYellow: "#e0c050", brightBlue: "#84b0e0", brightMagenta: "#d09cd8", brightCyan: "#8cd0c8", brightWhite: null},
  "teal-dusk": {background: "#102e35", foreground: "#d9eaea", cursor: "#5fd7b8", selectionBackground: "#1f4a55", selectionForeground: "#ecf6f6", black: "#0a1f24", red: "#ff6e6e", green: "#5fd7b8", yellow: "#e6c466", blue: "#6fb5d6", magenta: "#c694e8", cyan: "#7fc6c0", white: "#d9eaea", brightBlack: null, brightRed: "#ff8e8e", brightGreen: "#86e3c8", brightYellow: "#f0d488", brightBlue: "#8fc8e3", brightMagenta: "#d6abef", brightCyan: "#9bd4cf", brightWhite: null},
  "terracotta": {background: "#3b1e15", foreground: "#f0d8c4", cursor: "#e07845", selectionBackground: "#5f3225", selectionForeground: "#ffe8d4", black: "#28140d", red: "#e8516a", green: "#b9c270", yellow: "#e07845", blue: "#8fa9bd", magenta: "#cf8fc4", cyan: "#7ec4b3", white: "#f0d8c4", brightBlack: null, brightRed: "#ff7589", brightGreen: "#c8cf85", brightYellow: "#f08e62", brightBlue: "#a8c0d2", brightMagenta: "#dca6cf", brightCyan: "#9bd2c0", brightWhite: null},
  "worktrees": {background: "#141414", foreground: "#e8e8e8", cursor: "#d0d0d0", selectionBackground: "#404060", selectionForeground: "#f8f8f8", black: "#1a1a1a", red: "#e05050", green: "#80c080", yellow: "#e0b070", blue: "#7090d0", magenta: "#c080c0", cyan: "#60c0c0", white: "#c8c8c8", brightBlack: "#505050", brightRed: "#ff7080", brightGreen: "#a0e0a0", brightYellow: "#f0c890", brightBlue: "#90b0f0", brightMagenta: "#e0a0e0", brightCyan: "#90e0e0", brightWhite: "#f8f8f8"},
  "wrought-iron": {background: "#1a0e08", foreground: "#f0d5b8", cursor: "#c06040", selectionBackground: "#341c10", selectionForeground: "#ffe8d0", black: "#100804", red: "#e84838", green: "#7cc070", yellow: "#e8a830", blue: "#6490c8", magenta: "#ba76ba", cyan: "#6ebcac", white: "#f0d5b8", brightBlack: null, brightRed: "#f07060", brightGreen: "#94d088", brightYellow: "#f0c050", brightBlue: "#84ace0", brightMagenta: "#d092d2", brightCyan: "#8cccc0", brightWhite: null}
};

// The operator's own repo to theme map, carried over so a session in a repo
// gets the color they already associate with it. A starting point, not a
// rule: choosing a theme on a card overrides this and is remembered.
const REPO_THEMES = {
  "appetizer": "active-work",
  "atrium": "active-light",
  "desktop-edge-win": "dracula",
  "docusaurus-shared": "matrix",
  "dotfiles": "tangent",
  "sdk-golang": "terracotta",
  "sterling": "orange-coral",
  "ziti": "teal-dusk",
  "ziti-console": "deep-amethyst",
  "ziti-doc": "imperial-purple",
  "ziti-openwrt": "ocean-deep",
  "ziti-sdk-c": "neon-grape",
  "ziti-sdk-csharp": "nord",
  "ziti-sdk-py": "deep-ocean",
  "ziti-tunnel-sdk-c": "gruvbox-dark",
  "ziti-tv": "mauve-purple",
  "zrok": "electric-purple"
};

// ── themes somebody brought ─────────────────────────────
//
// The table above is what atrium ships. This is what the operator added, and
// it comes from the daemon rather than from this file.
//
// WHY THE DAEMON AND NOT `localStorage`, when the grouping expression two
// screens away is refused exactly that. The argument is in `store/termtheme.go`
// and it turns on what the value IS: an expression is compiled and run, a
// colour is six hex digits and cannot become code. And a terminal theme has a
// requirement grouping does not, which settles it: the NAME is already stored
// on the card and has been for a long time, so a palette kept in one browser
// would give a card a colour in that browser and the project default in every
// other one.
//
// A brought theme with a shipped theme's name WINS. That is the point rather
// than an accident: somebody who says "your dracula is wrong" wants theirs, and
// deleting theirs puts atrium's back.
let BROUGHT_THEMES = Object.create(null);

// The palette's fields, in the daemon's order, so the editor draws its boxes
// from the thing that validates them. Same rule as the skin picker: a page with
// its own list of fields is the list that silently drifts.
let THEME_SLOTS = [];

// The two tables merged, built once and thrown away when either changes.
let themeTable = null;

// A NULL PROTOTYPE, AND THAT IS NOT TIDINESS.
//
// `TERM_THEMES` was an object literal, so `TERM_THEMES["constructor"]` answered
// a function rather than nothing, and `themeFor` tested the answer for
// truthiness. A card whose theme was set to `constructor`, `valueOf` or
// `toString` therefore handed xterm a function to read colours off, and every
// one of them came back undefined. It took no brought theme to do it: it has
// been reachable from the fixture dialog since there was one.
//
// Now the table inherits nothing, so a name that was never put in it is
// missing, which is what "not a theme" should have meant all along. The daemon
// refuses those names as well, and the two are independent on purpose: this one
// covers the shipped table, which the daemon never sees.
function allThemes() {
  if (!themeTable) {
    themeTable = Object.assign(Object.create(null), TERM_THEMES, BROUGHT_THEMES);
  }
  return themeTable;
}

// One theme by name, or null. The only way this page looks a theme up.
function themeNamed(name) {
  const n = String(name || "").trim();
  if (!n) return null;
  const t = allThemes()[n];
  return t && typeof t === "object" ? t : null;
}

// The picker's contents, brought ones first.
//
// A name that is in both lists appears once, under `yours`, because that is the
// one that would be used. Showing it twice would be a list where two identical
// entries do different things, which is worse than either.
function themeOptionsHTML(now) {
  const opt = n => `<option value="${esc(n)}"${n === now ? " selected" : ""}>${esc(n)}</option>`;
  const brought = Object.keys(BROUGHT_THEMES).sort();
  const shipped = Object.keys(TERM_THEMES).sort().filter(n => brought.indexOf(n) < 0);
  return `<option value=""${now ? "" : " selected"}>use the project default</option>` +
    (brought.length ? `<optgroup label="yours">${brought.map(opt).join("")}</optgroup>` : "") +
    `<optgroup label="atrium ships these">${shipped.map(opt).join("")}</optgroup>`;
}

// Asked for at boot and again whenever the daemon says they changed.
//
// A failure is silent and leaves the shipped table alone. The board is usable
// without brought themes and unusable without a terminal, so this must never be
// the thing that stops the page coming up.
async function loadThemes() {
  let out;
  try { out = await api("/v1/themes"); } catch (e) { return; }
  const next = Object.create(null);
  for (const t of (out.themes || [])) {
    if (t && t.name && t.palette) next[t.name] = t.palette;
  }
  BROUGHT_THEMES = next;
  THEME_SLOTS = out.slots || [];
  themeTable = null;
}

// Which theme a session uses.
//
// The card wins, since that is what was chosen for it. Failing that the
// project name picks one, so two worktrees of the same repo look alike and a
// different repo does not, which is the whole reason to color them.
// NOT THE CURRENT GROUPING. This asked `grouper()` for the card's group and
// treated it as the project, which was true only while grouping was by
// project. Switch the board to piles and every group name is `pull-requests`
// or `tangent`, the repo map matches none of them, and every terminal on the
// board turns default at once. How you are slicing the board right now is not
// a fact about which repository a session is in.
//
// `repo` when the card has one, since a launcher that resolved a URL knows it
// outright, and the path rule otherwise.
// A NAME NOBODY DEFINED FALLS BACK, and a theme deleted from the editor is
// exactly that. The card keeps the name it was given, because a delete that
// rewrites every card mentioning a theme is a delete that edits work.
function themeFor(t) {
  if (!t) return TERM_THEMES.atrium;
  const named = themeNamed(t.theme);
  if (named) return named;

  const project = String(t.repo || "").trim() || defaultProjectOf(t);
  return themeNamed(REPO_THEMES[repoLeaf(project)]) || TERM_THEMES.atrium;
}

// The last path segment of a project name, which is what the repo map is
// keyed by. `dovholuknf/dotfiles` is `dotfiles`.
function repoLeaf(name) {
  const parts = String(name || "").split("/").filter(Boolean);
  return parts.length ? parts[parts.length - 1] : "";
}

// Choosing what a terminal looks like.
//
// Applied to the live terminal immediately as well as stored, so the choice
// is judged by looking at it rather than by reading a name. xterm takes a new
// theme on the running instance, so nothing has to be torn down.
// The card's mark, drawn the way a notification will draw it.
//
// The same canvas that produces the PNG, shown at the size the operating
// system uses. A glyph that renders as a box on the notification renders as a
// box here, which is worth finding out while you are still typing it.
function paintIconPreview() {
  const el = document.getElementById("d-icon-preview");
  const input = document.getElementById("d-icon");
  const clear = document.getElementById("d-icon-clear");
  if (!el || !input) return;

  // A card with a picture shows the picture, and its glyph field goes away:
  // there is one mark, and offering to edit a glyph that is not being used
  // would be a control with no effect.
  const img = current && iconIsImage(current.icon);
  input.hidden = !!img;
  if (clear) clear.hidden = !img;
  if (img) {
    // Cache-busted, or replacing a picture shows the old one until something
    // else evicts it. The URL is stable by design, which is what makes this
    // necessary.
    el.innerHTML = `<img alt="" src="${iconURLFor(current.id)}?v=${Date.now()}">`;
    return;
  }
  el.innerHTML = `<img alt="" src="${iconDataURL("atrium", input.value.trim())}">`;
}

async function uploadIcon(input) {
  const file = input.files && input.files[0];
  input.value = "";
  if (!file || !current) return;

  const form = new FormData();
  form.append("file", file, file.name);
  try {
    const res = await api(`/v1/tasks/${current.id}/icon`, { method: "POST", body: form });
    current.icon = res.icon;
  } catch (e) {
    toast("that did not go up", e.message);
    return;
  }
  paintIconPreview();
  toast("icon set", file.name);
  refresh();
}

async function clearIcon() {
  if (!current) return;
  try {
    await api(`/v1/tasks/${current.id}/icon`, { method: "DELETE" });
  } catch (e) {
    toast("could not clear it", e.message);
    return;
  }
  current.icon = "";
  document.getElementById("d-icon").value = "";
  paintIconPreview();
  refresh();
}

// The mark a card wears on a desktop notification, asked for rather than
// edited in the dialog.
//
// Kept because the terminal cog offers it too, where there is no dialog open
// and opening one to change a single glyph is the wrong size of gesture.
//
// Whatever is typed is drawn to a canvas and sent as a PNG. It is never
// inserted anywhere, so an icon cannot be markup however it was set, including
// by a source that filled the card in.
async function pickIcon(id, current) {
  const pick = await askUser({
    title: "notification icon",
    body: "One character, or an emoji. Shown on desktop notifications from " +
          "this card, and remembered on it, so it is the same tomorrow and in " +
          "another browser. Windows draws it small: a single bold glyph reads, " +
          "a word does not.",
    input: true,
    value: current || "",
    placeholder: "empty means the atrium mark",
    buttons: [{ label: "cancel", value: null }, { label: "use it", value: true, style: "go" }]
  });
  if (pick === null) return;
  try {
    await patchTask(id, { icon: String(pick).trim() });
  } catch (e) {
    toast("that did not stick", e.message);
    return;
  }
  refresh();
}

// Trying a theme ON the terminal, rather than choosing one from a list of
// names.
//
// The dialog this replaces was the worst control on the board, for two
// reasons. Its datalist filtered on the field's contents and the field opened
// pre-filled with the theme already set, so the only suggestion offered was
// the one already in use and you had to delete the text before the list
// appeared. Nothing said so.
//
// The second reason is the real one and no dialog could have fixed it:
// `tarnished`, `cocoa` and `blood-moon` are all plausible and none of them
// tells you what a terminal will look like. A theme is sixteen ANSI colors
// and a background, and the only honest preview is a real session rendered in
// it, with the output already on screen.
//
// So it is a `<select>` on the bar, not in a dialog. A closed select fires
// `change` on every arrow key, which is exactly the asked-for behavior:
// arrow down, see it, arrow again. Nothing is written until you leave the
// control, and escape puts back what was there.

// What the theme was before this started, so escape has something to restore.
let themeBefore = null;

function pickTheme() {
  const sel = document.getElementById("t-theme");
  if (!sel || !termTask) return;
  themeBefore = termTask.theme || "";

  // Rebuilt each time. Both tables are already in memory, so this costs
  // nothing, and it keeps the list honest after a theme was brought, edited or
  // deleted from somewhere else.
  sel.innerHTML = themeOptionsHTML(themeBefore);
  sel.value = themeBefore;
  document.getElementById("t-theme-wrap").hidden = false;
  sel.focus();
}

// Applied to the running terminal and nowhere else. xterm takes a new theme on
// a live instance, so nothing is torn down and the scrollback is untouched,
// which is the whole point: you are judging it against real output.
function previewTheme(name) {
  if (!term || !termTask) return;
  const t = themeFor(Object.assign({}, termTask, { theme: name }));
  term.options.theme = t;
  paintPaneBg(t);
}

// The pane takes the terminal's own background.
//
// xterm paints the character grid and nothing outside it, and the grid is a
// whole number of cells, so there is always a strip left over: the padding,
// the margin, and the partial row and column at the far edge. That strip was
// the board's dark blue, which around a session themed dark red read as a
// frame nobody drew on purpose.
//
// Set as a variable on the pane rather than as a style on one element, so the
// screen, the bar and the pane's own rounded corner all agree without three
// places knowing the color.
function paintPaneBg(theme) {
  const pane = document.querySelector(".term-pane");
  if (!pane) return;
  const bg = (theme && theme.background) || "";
  // A THEME GOES ON AND COMES OFF AS ONE THING.
  //
  // `--term-fg` and `--term-line` were left behind here while `--term-bg` and
  // the rest were removed, so a pane with nothing attached kept the FOREGROUND
  // of whatever was last on it. Anything drawing a pair with a fallback each
  // then took one colour from a dead theme and the other from the board, which
  // is how the restart banner ended up as pale text on a near-white pill for
  // the second time. `.termwait` says the rest of it.
  //
  // The class is the switch every one of those rules reads. There are two
  // states, themed and not, and nothing may be half way between them.
  pane.classList.toggle("themed", !!bg);
  if (!bg) {
    ["--term-bg", "--term-fg", "--term-edge", "--term-line",
     "--term-thumb", "--term-thumb-hi"].forEach(v => pane.style.removeProperty(v));
    const bare = document.getElementById("term-layout");
    if (bare) {
      bare.style.removeProperty("--term-line");
      bare.style.removeProperty("--term-thumb-hi");
    }
    return;
  }
  // The pane's own outline, in the colour the tab beside it is using. Same
  // source as `--tabc` where the switcher card is rendered, so the two cannot
  // drift apart.
  pane.style.setProperty("--term-edge", theme.cursor || theme.foreground || "");
  pane.style.setProperty("--term-bg", bg);
  // The scrollbar too, from colors the theme already carries.
  //
  // `selectionBackground` is the right one: every theme defines it, and it is
  // by construction a mid tone that reads against that theme's background,
  // which is exactly what a thumb has to be. Picking one here, or lightening
  // the background by some percentage, would invent a color for fifty two
  // palettes that already answered the question.
  //
  // The cursor for the hover state, which is the loudest color a theme owns
  // and is meant to be found at a glance.
  pane.style.setProperty("--term-thumb", theme.selectionBackground || theme.foreground || "");
  pane.style.setProperty("--term-thumb-hi", theme.cursor || theme.foreground || "");
  // The drawer's chrome too. It sits ON the theme background, so board-blue
  // borders and icons over a dark red pane read as a control panel from a
  // different application bolted onto the terminal.
  //
  // Same source as the thumb, and for the same reason: `selectionBackground`
  // is by construction a tone that reads against that theme's background, and
  // is what a rule or a border wants to be.
  pane.style.setProperty("--term-fg", theme.foreground || "");
  pane.style.setProperty("--term-line", theme.selectionBackground || theme.foreground || "");

  // And on the layout, for the chrome BESIDE the pane rather than inside it.
  // The drag handle is between the list and the terminal and belongs to
  // neither, and it was the last board-blue thing left on a themed view.
  const lay = document.getElementById("term-layout");
  if (lay) {
    lay.style.setProperty("--term-line", theme.selectionBackground || theme.foreground || "");
    lay.style.setProperty("--term-thumb-hi", theme.cursor || theme.foreground || "");
  }
}

// Answered, not abandoned.
//
// Nothing happens on blur. A theme is judged by looking at the terminal, and
// looking at the terminal means clicking it, so a picker that committed and
// closed on blur closed itself the first time you tried to use it.
async function keepTheme() {
  const wrap = document.getElementById("t-theme-wrap");
  const sel = document.getElementById("t-theme");
  if (!sel || !wrap || wrap.hidden) return;
  wrap.hidden = true;
  const name = sel.value;
  if (!termTask || name === themeBefore) return;

  try {
    await patchTask(termTask.id, { theme: name });
  } catch (e) {
    toast("that did not stick", e.message);
    previewTheme(themeBefore);
    return;
  }
  termTask.theme = name;
  toast("theme set", name || "back to the project's own");
}

function cancelTheme() {
  const wrap = document.getElementById("t-theme-wrap");
  if (!wrap || wrap.hidden) return;
  wrap.hidden = true;
  previewTheme(themeBefore);
  if (termTask) termTask.theme = themeBefore;
}

// ── bringing a theme, and editing one ───────────────────
//
// The colours themselves, rather than which of them a card wears.
//
// The two are deliberately different controls in different places. Choosing is
// done on the terminal bar, against real output, and is per card. Editing is a
// dialog, because it is twenty one boxes and because what it changes is shared:
// every card wearing that name gets the edit, in every browser.

// Which theme the editor loaded, so save knows whether the name in the box is a
// rename or the same theme again.
let themeEditFrom = "";

// What the attached terminal was wearing before the editor opened. The editor
// paints the live terminal as you type, which is the only honest preview there
// is, and closing without saving has to put it back.
let themeEditRestore = null;

function openThemeEditor(name) {
  const dlg = document.getElementById("theme");
  if (!dlg) return;
  const pick = document.getElementById("th-pick");
  // Loaded from the merged table, so editing a shipped theme means starting
  // from its colours rather than from twenty one empty boxes. That is what
  // somebody means by "this one, but the green".
  pick.innerHTML = themeOptionsHTML(name || "");
  pick.value = themeNamed(name) ? name : "";
  themeEditRestore = term ? term.options.theme : null;
  editTheme(pick.value);
  if (!dlg.open) dlg.showModal();
}

// Loads one theme's colours into the boxes.
//
// An empty name is a blank theme, which is how somebody writes one from
// scratch rather than from a scheme.
function editTheme(name) {
  themeEditFrom = name || "";
  const nameBox = document.getElementById("th-name");
  if (nameBox) nameBox.value = themeEditFrom;
  fillThemeSlots(themeNamed(name) || {});
  const del = document.getElementById("th-delete");
  // Only a brought theme can be deleted. A shipped one is in the build and the
  // button would be a control that fails on press.
  if (del) del.hidden = !(themeEditFrom && (themeEditFrom in BROUGHT_THEMES));
  paintThemeSample();
}

// The boxes, drawn from the daemon's list of slots.
//
// Two inputs per colour and they stay in step both ways: the swatch is how a
// colour is judged and nudged, the hex is how one is pasted in. A required slot
// that is empty is seeded with the background or the foreground rather than
// with black, so a theme written from scratch is legible from the first
// keystroke instead of being a black square until the last box is filled.
function fillThemeSlots(palette) {
  const grid = document.getElementById("th-colours");
  if (!grid) return;
  // A page that could not reach the daemon has no slots. Falling back to the
  // shipped `atrium` theme's own field names keeps the editor drawable rather
  // than empty, and saving still goes through the daemon's validator.
  const slots = THEME_SLOTS.length
    ? THEME_SLOTS
    : Object.keys(TERM_THEMES["active-work"]).map(n => ({ name: n, required: false }));
  grid.innerHTML = slots.map(s => {
    const v = String(palette[s.name] || "");
    // The swatch input has no empty state and will show black for one, which
    // would read as a decision. It gets a stand-in and the text box stays
    // empty, which is what actually gets saved.
    const swatch = v || (s.name === "background" ? "#000000" : "#cccccc");
    return `<div class="thslot">
      <label for="th-c-${esc(s.name)}" title="${esc(s.name)}${s.required ? "" : ", optional"}"
        >${esc(s.name)}${s.required ? "" : " ?"}</label>
      <input type="color" id="th-s-${esc(s.name)}" data-slot="${esc(s.name)}"
        value="${esc(swatch)}">
      <input type="text" id="th-c-${esc(s.name)}" data-slot="${esc(s.name)}" spellcheck="false"
        maxlength="7" placeholder="${s.required ? "#1a2b3c" : "optional"}" value="${esc(v)}">
    </div>`;
  }).join("");

  // WHICH SLOT COMES BACK THROUGH THE DOM, not through an inline handler.
  //
  // The names here are the daemon's own and hold nothing surprising, so this is
  // the house rule rather than a live hazard: HTML escaping is not JavaScript
  // escaping, and the moment one of these strings comes from somewhere else an
  // inline handler is a quoted string somebody can leave.
  //
  // Nothing stacks. `innerHTML` above replaces the elements these are attached
  // to every time, so the old listeners go with the old nodes.
  grid.querySelectorAll('input[type="color"]').forEach(el =>
    el.addEventListener("input", () => themeSwatch(el.dataset.slot, el.value)));
  grid.querySelectorAll('input[type="text"]').forEach(el =>
    el.addEventListener("input", () => themeHex(el.dataset.slot, el.value)));
}

// The swatch moved, so the hex follows it.
function themeSwatch(name, value) {
  const box = document.getElementById("th-c-" + name);
  if (box) box.value = String(value || "").toLowerCase();
  themeEdited();
}

// The hex was typed, so the swatch follows it, but only once it is a colour.
//
// Half a hex is not an error, it is somebody in the middle of typing one. So a
// partial value marks the box and moves nothing, and the mark clears itself
// when the sixth digit lands.
function themeHex(name, value) {
  const v = String(value || "").trim().toLowerCase();
  const box = document.getElementById("th-c-" + name);
  const good = /^#[0-9a-f]{6}$/.test(v);
  if (box) box.classList.toggle("bad", !!v && !good);
  if (good) {
    const sw = document.getElementById("th-s-" + name);
    if (sw) sw.value = v;
  }
  themeEdited();
}

// Anything changed: repaint the block, and the live terminal with it.
//
// HALF-TYPED COLOURS ARE DROPPED RATHER THAN SENT. xterm parses what it is
// given, and handing it `#1a2` on the way to `#1a2b3c` is a throw inside the
// terminal for every keystroke in between. What is dropped keeps its old value
// on screen until the sixth digit lands, which is also the calmer thing to
// watch.
function themeEdited() {
  paintThemeSample();
  if (!term) return;
  const typed = readThemeSlots(), p = {};
  for (const k in typed) {
    const c = okColour(typed[k], "");
    if (c) p[k] = c;
  }
  if (!p.background || !p.foreground) return;
  term.options.theme = p;
  paintPaneBg(p);
}

// The boxes, back as a palette. Empty stays empty: an absent optional colour is
// a thing the board fills in for itself and is not the same as black.
function readThemeSlots() {
  const out = {};
  document.querySelectorAll('#th-colours input[type="text"]').forEach(el => {
    const v = String(el.value || "").trim().toLowerCase();
    if (v) out[el.dataset.slot] = v;
  });
  return out;
}

// A COLOUR IS SIX HEX DIGITS HERE TOO, and this is the only place in the page
// that has to say so out loud.
//
// Everything else takes its colours from the daemon, which validated them.
// These come from a text box being typed into, and they go into a `style`
// attribute. `esc` stops the attribute being broken out of and does not stop
// `red;background:url(...)` from being a second declaration inside it. Nothing
// executes either way, but the rule this feature is built on is that a colour
// cannot be anything but a colour, and a preview that quietly accepts CSS is
// the exception that teaches the next person the rule is soft.
function okColour(v, fallback) {
  return /^#[0-9a-f]{6}$/.test(String(v || "").toLowerCase()) ? String(v).toLowerCase() : fallback;
}

// What it looks like, without needing a session attached. See `.thsample`.
function paintThemeSample() {
  const el = document.getElementById("th-sample");
  if (!el) return;
  const p = readThemeSlots();
  const bg = okColour(p.background, "#000000"), fg = okColour(p.foreground, "#cccccc");
  el.style.background = bg;
  el.style.color = fg;
  const at = (n, fb) => okColour(p[n], fb);
  const block = n => `<span class="sw" style="color:${at(n, fg)}">&#9608;</span>`;
  const row = names => names.map(block).join("");
  const say = (n, s) => `<span style="color:${at(n, fg)}">${esc(s)}</span>`;
  el.innerHTML =
    say("green", "you@atrium") + ":" + say("blue", "~/src/atrium") +
    say("magenta", " (main)") + "$ go test ./...\n" +
    "ok  " + say("cyan", "internal/store") + "\t" + say("yellow", "0.41s") + "\n" +
    say("red", "FAIL") + "  internal/api\tsee " + say("brightBlue", "the log") + "\n" +
    row(["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"]) + "\n" +
    row(["brightBlack", "brightRed", "brightGreen", "brightYellow",
         "brightBlue", "brightMagenta", "brightCyan", "brightWhite"]) +
    `\n<span style="background:${at("selectionBackground", fg)};` +
    `color:${at("selectionForeground", bg)}">selected text</span>` +
    ` <span style="background:${at("cursor", fg)};color:${bg}">&nbsp;</span> cursor`;
}

async function saveTheme() {
  const name = String(document.getElementById("th-name").value || "").trim().toLowerCase();
  if (!name) {
    tellUser("atrium", "give it a name first. that is how a card asks for it.");
    return;
  }
  let saved;
  try {
    saved = await api("/v1/themes/" + encodeURIComponent(name), {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(readThemeSlots())
    });
  } catch (e) {
    // The daemon's refusal names the slot and the value, which is the whole
    // use of it. Shown rather than summarised.
    tellUser("atrium", e.message);
    return;
  }
  await loadThemes();
  // The terminal is already wearing this, but it was wearing the boxes rather
  // than the theme. Re-applied from the table so what is on screen is what was
  // stored, including whatever the daemon normalised.
  themeEditRestore = null;
  if (termTask) previewTheme(termTask.theme || "");
  editTheme(saved.name);
  toast("theme saved", saved.name === themeEditFrom
    ? saved.name
    : saved.name + ". pick it on a terminal to use it");
}

async function removeTheme() {
  const name = themeEditFrom;
  if (!name) return;
  const sure = await askUser({
    title: "delete " + name,
    body: (name in TERM_THEMES)
      ? "Yours goes and atrium's own " + name + " comes back. Cards set to it keep the name."
      : "Cards set to it keep the name and fall back to their project's colour.",
    buttons: [{ label: "keep it", value: null }, { label: "delete", value: true, style: "no" }]
  });
  if (!sure) return;
  try {
    await api("/v1/themes/" + encodeURIComponent(name), { method: "DELETE" });
  } catch (e) {
    tellUser("atrium", e.message);
    return;
  }
  await loadThemes();
  themeEditRestore = null;
  if (termTask) previewTheme(termTask.theme || "");
  editTheme(name in TERM_THEMES ? name : "");
  toast("theme deleted", name);
}

// A settings.json off disk, read here rather than uploaded, so the same route
// handles a paste and a file and there is one thing to get right.
function themeFilePicked(input) {
  const file = input.files && input.files[0];
  input.value = "";
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    document.getElementById("th-import").value = String(reader.result || "");
    bringThemes();
  };
  reader.readAsText(file);
}

async function bringThemes() {
  const box = document.getElementById("th-import");
  const text = String(box.value || "").trim();
  if (!text) {
    tellUser("atrium", "paste a windows terminal settings.json, or pick the file.");
    return;
  }
  const force = document.getElementById("th-force").checked;
  let out;
  try {
    out = await api("/v1/themes/import" + (force ? "?force=1" : ""), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: text
    });
  } catch (e) {
    tellUser("atrium", e.message);
    return;
  }
  await loadThemes();
  const got = out.imported || [], kept = out.kept || [];
  // WHAT DID NOT COME IN IS THE HALF WORTH SAYING. A settings.json is somebody's
  // whole collection and the ones that were skipped were skipped for a reason
  // each, which is on the row. A count alone sends them back to the file to
  // guess which.
  if (got.length) {
    box.value = "";
    editTheme(got[0]);
  }
  const lines = kept.map(k => k.name + ": " + k.why).join("\n");
  if (!got.length) {
    tellUser("atrium", "nothing was brought in.\n\n" + (lines || "no scheme in that was usable."));
    return;
  }
  toast(got.length + " theme" + (got.length === 1 ? "" : "s") + " brought in",
    got.slice(0, 4).join(", ") + (got.length > 4 ? " and more" : "") +
    (kept.length ? ". " + kept.length + " skipped" : ""));
  if (kept.length) tellUser("atrium", "brought in: " + got.join(", ") + "\n\nskipped:\n" + lines);
}

// CLOSING WITHOUT SAVING PUTS THE TERMINAL BACK.
//
// The editor paints the attached terminal as you type, which is the only
// preview worth having. `close` fires for every way out, including escape, so
// without this a dialog dismissed by leaning on escape leaves a session wearing
// a palette that was never stored and comes back different on the next reload.
(() => {
  const dlg = document.getElementById("theme");
  if (!dlg) return;
  dlg.addEventListener("close", () => {
    if (!themeEditRestore) return;
    if (term) {
      term.options.theme = themeEditRestore;
      paintPaneBg(themeEditRestore);
    }
    themeEditRestore = null;
  });
})();

