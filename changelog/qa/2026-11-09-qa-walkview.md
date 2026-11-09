# qa-walkview

`TestBoardScriptParses` and `TestNoTwoBoardScriptsDeclareTheSameName` failed because the walk lives on its own page
(`walk.html`, its own global scope) and both tests read every file in `web/js` as if `index.html` loaded it.

Fix: the tests now read each page's own script list. A file loaded only by `walk.html` is not dead code, and the walk
page's scripts are parsed and checked for duplicate names on their own, so coverage of both pages stays.
