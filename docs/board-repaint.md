# Repainting the board without throwing away where you were

The board used to repaint wholesale. `setHTML` assigned `innerHTML` for a whole list, so every node under it was
destroyed and rebuilt. A browser has nowhere to put the scroll position of an element that no longer exists, so
the view went back to the top. That fired on every SSE event, and with sixteen live agents an event arrives
constantly, so scrolling down was something you could not finish doing.

Scroll was the loudest symptom and not the only one. The same swap dropped a text selection mid-drag, moved
focus, closed anything anchored to a node that had gone, and restarted every CSS transition, which is why the
board looked like it flickered under load.

## The answer that was rejected

Record `scrollTop` before the swap and put it back after. That was in `setHTML` for a while and it was a lie: it
fights the browser every frame, does nothing at all for selection or focus, and goes wrong the moment the
content above the viewport changes height. It is gone, along with `scrollParent`, which existed only to serve
it.

## Two tiers, and the second is what makes it hold

Only replace a card if the card changed, so the content updates and the container does not.

1. **The container is never rebuilt.** Rows are added, removed and reordered by key. Anything untouched keeps
   its DOM, so scroll, selection and focus survive because nothing that held them was destroyed.
2. **A row is not rebuilt either, unless its structure changed.** The fields that tick constantly are the age,
   the activity chip and the status class, and those are written into the nodes that are already there. Only a
   real change rebuilds a node: the title, the tags, the `why`, the card arriving or leaving.

**Skipping tier two swaps one bug for a quieter one.** The age changes every second, so the card being destroyed
is the card being read: the scroll position survives and the selection dies anyway. That is a fix that looks
complete and is not, and it is why `morphNode` compares text before writing it and `morphAttrs` compares an
attribute before setting it.

## How it works

`setHTML` still takes the same markup every caller was already producing. It parses that markup into a detached
element OF THE SAME TAG, never a `<template>`, because the parser's answer depends on where the markup lands and
rows outside a table are dropped on the floor. Then `morphChildren` reconciles the parsed copy against what is
on the page:

- Old children that carry a key go into a map, so a row that MOVED is found rather than rebuilt where it landed.
- New children are walked in order against a cursor. A keyed child claims its old node from the map. An unkeyed
  one matches the cursor if the cursor is also unkeyed and the same kind of node, which is what a heading or a
  wrapper is.
- A claimed node is moved into place if it is not already there, then patched: attributes in both directions,
  then its children, recursively.
- Anything the cursor never reached is gone.

`setHTML` also caches the markup string it last painted from, which is what makes the fast path fire at all. The
previous check compared against `el.innerHTML`, and that is a fresh serialization: the parser normalises the
whitespace inside a tag, so nothing on this board written across more than one line ever matched.

### What makes two nodes the same node

`morphKey`, in order: `data-morph-key`, `data-id`, `data-column`, `data-path`, `data-status`. `data-id` is the
card, which is the case that matters, and it is the precondition the whole change rested on: every row on the
board and in the stack already carried one. The terminal switcher did not, and now does.

**Keys are matched within a parent.** A card dragged from one column to another is rebuilt under its new column,
which is correct, because that is the change. What matters is that the two columns are keyed as well, so neither
is rebuilt around it.

### Attributes, not properties

`morphAttrs` writes attributes. Somebody who has typed into an input or ticked a checkbox has made that element
dirty, and the browser stops reflecting the attribute into the value once they have. So writing the attribute
updates a field nobody has touched and leaves a half-finished edit exactly where it was. Both of those are what
you want, and the file picker's ticks now survive a repaint as a side effect.

### Two escape hatches

- `data-morph-key="..."` keys markup that has no natural id. The project and pinned groups use it so that
  reordering the groups moves them rather than rewriting each one with the next one's contents.
- `data-morph-keep` marks a subtree the board has handed to something else. Its attributes still sync, its
  children are left alone. Nothing uses it yet. It is there because the first thing to want it will be a
  terminal or a canvas, and by then the reason will not be obvious.

## The listener hazard, which is new

A card that was already on the board is THE SAME ELEMENT after a repaint, with the listeners it was given the
first time still attached. `wireDragging` runs after every render, so without a guard it adds a second copy of
every handler on every event, and a `drop` handler running twice files the same move twice, seconds apart,
against ranks that have already changed.

`wireDragging` sets `__dragWired` on each node it has wired and skips it afterwards. The flag lives on the node,
so it goes when the node does, which is the only time a fresh set of listeners is wanted.

Anything else that wires listeners onto what it drew has the same hazard. Assigning `el.onclick` is safe, since
assignment replaces; `addEventListener` is not. The file picker uses assignment and needed no change.

## What checks it

```bash
bash scripts/check-board.sh
```

Two of the things it runs are about this:

- `scripts/check-morph.js` checks the SHAPE. That `setHTML` reconciles rather than assigning `innerHTML`, that
  the comparisons in tier two are still there, that a board card, a stack row and a terminal switcher row all
  still carry `data-id`, and that `wireDragging` still guards. Each of those is one list that silently goes back
  to being destroyed and rebuilt if its rule breaks, and nothing on screen says so.
- `scripts/test-morph.js` RUNS it, against a small DOM implemented in the test, because this repo has no npm
  dependencies and there is no browser in CI. What it asserts is not that the board draws. It is that a row
  nobody changed comes out the same object and was never written to, since that object is what holds the scroll
  and the selection. A reconciler that rebuilds a row it could have kept looks identical on screen and has the
  original bug.

## What is not covered

Neither check can tell you the board still looks right, and the fix is deceptive: it looks fixed on a quiet
board because a quiet board was never the problem. Test it with a list scrolled down, mid text selection, while
cards are actually changing. `atrium preview --from live` is the way to do that against real cards.
