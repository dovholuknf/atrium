# u-read-tab-text-size

read.html: html now scrolls (tokens.css body overflow:hidden overridden on this page only), A-/A+ and ctrl+=/-/0 set --read-fs, stored at localStorage atrium.readfont (default 15px, was 13.5). New headless section readTabSize (long doc scrolls by wheel and End, size survives reload, no other key written); ran with bootClean, both pass. go build passes. Screens in docs/screens/u-read-tab-text-size/. Headless Chromium draws overlay scrollbars, so the bar is checked by the overflow rule, not pixels.
