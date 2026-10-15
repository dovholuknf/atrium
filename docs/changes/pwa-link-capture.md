# pwa-link-capture

The desktop board has a web app manifest (`/manifest.webmanifest`, scope `/`, `display: standalone`,
`launch_handler: focus-existing`, `handle_links: preferred`). The board registers `launchQueue.setConsumer`
(`captureLaunchLinks` in `js/terminal-links.js`). A URL delivered to the app window goes through `openAtriumLink`,
the one router for board links, so `#term=<id>` and a card's readable path land on the card. Without `launchQueue`
nothing is registered. The phone board keeps its own manifest, scope `/m/`, which is the nearer scope for that page.

## Which origin to install

The hub board (`http://127.0.0.1:7778`) and a room's own board (`:7781`) are different origins, and so are
`127.0.0.1` and `localhost`. An app captures only links on the origin it was installed from, and each origin serves the
manifest. Install from the origin your links use. Board links are written as `http://127.0.0.1:7778/#term=<id>`, so
install from **`http://127.0.0.1:7778`**, not `localhost`. An app installed from `localhost:7778` never matches them.

## What is and is not verified

- `launch_handler` and `launchQueue` ship in Chromium since 110 (Chrome for Developers, "Launch Handler API").
- Chrome 139 and later (Windows, Mac, Linux) open an in-scope link in the installed app by default, unless the user
  opted out in the app's settings ("Navigation management into installed PWAs").
- `handle_links` is a proposal that the Chrome documentation does not mention. It is in the manifest as a hint and
  is harmless if ignored. Do not rely on it.
- No flag is needed on a current Chrome. The old `#enable-user-link-capturing-pwa` and
  `#enable-desktop-pwas-link-capturing` flags belong to earlier versions. Brave is not documented here: whether it
  tracks Chrome 139's behavior, and whether `brave://app-settings` then shows an "Open supported links" section, is
  NOT verified. Try the steps and see.
- **Open risk:** the Chromium documents describe capturing for navigations started inside the browser. A link
  clicked in another program (Windows Terminal) is opened by the OS in the default browser, and Chromium issue
  330282442 reports that such links do not open the PWA. If that holds in Brave, the click still opens a tab, and
  the manifest alone cannot fix it.

## Test plan

## @LETTER@. A board link opens in the installed app

### @LETTER@1. Install

1. Close and uninstall the current shortcut app: `brave://apps`, right-click it, Uninstall.
2. Open `http://127.0.0.1:7778` in Brave. Use the menu, Save and share, **Install atrium**. Not "Create shortcut".
3. Open `brave://app-settings`, pick atrium, and turn on any "Open supported links" or link-capture setting it shows.
   If there is none, note that.

**Expected:** atrium opens in its own window with the board.

### @LETTER@2. Click a link from outside

1. Leave the app window open. In Windows Terminal click `http://127.0.0.1:7778/#term=<a card id>`.

**Expected:** the existing app window comes forward and attaches that card, with no new tab. If a tab opens instead,
that is the open risk above.

### @LETTER@3. A link inside the browser

1. In a Brave tab, paste the same link.

**Expected:** with capturing on it opens in the app window and lands on the card.
