# qa-walktoast

A toast that stayed on screen after the walk's new-finding steps sat over the walk-done ask's "finish anyway" button
and took its clicks, so `HEADLESS_UNITS=walk` failed on claude-sg4. Toasts live 9 seconds and CI was faster.

Fix: inside `dialog#ask` a toast now lets clicks through and hides its copy and dismiss buttons.
