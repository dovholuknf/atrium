# tabsOrder headless check expects the items tab

The tabsOrder check still listed the bar as it was before the Items tab landed, so moving usage up two places read as a
failure. The test now expects `items` between `chat` and `usage`. The board code was right.
