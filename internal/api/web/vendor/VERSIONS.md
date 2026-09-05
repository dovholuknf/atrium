# Vendored, not fetched

The board has to work offline and over an overlay, so nothing here comes from a CDN at runtime.

| File | Package | Version | sha256 |
| --- | --- | --- | --- |
| `xterm.js` | `@xterm/xterm` | 5.5.0 | `1f991ac3b4b283ebf96e60ae23a00a52765dd3a2e46fa6fdda9f1aab032f7495` |
| `xterm.css` | `@xterm/xterm` | 5.5.0 | `ba8e6985669488981ccf40c0cefe3aba80722cb6c92de7ad628b0bd717faf2b6` |
| `xterm-addon-fit.js` | `@xterm/addon-fit` | 0.10.0 | `bdaefa370b1bfc42ee88d46fe6072400902a4d4b2d45cd93438dda9b23c97089` |
| `xterm-addon-webgl.js` | `@xterm/addon-webgl` | 0.18.0 | `9ffa9ac3ff6d47d4e6216ed1972ca8e0b5336cef744f50ebdc3f67b0ed727cdb` |
| `xterm-addon-search.js` | `@xterm/addon-search` | 0.15.0 | `3cf52d71d9deb4ba60125087434c53e3fb35bb2249db9b13987991fd2db1c7bd` |

**They move together.** A renderer addon built against a different core than the one beside it fails at
load, and the failure is silent: xterm falls back to its DOM renderer and everything still works, slowly. That
is exactly the state this repo was in before the webgl addon was added, and the symptom was one browser tab
holding a quarter of a CPU.

The versions above were not read off a manifest, because the minified bundles carry no version string. They
were established by downloading candidates and hashing them against what was already here. If you need to do
that again:

```bash
curl -sL -o xterm.tgz https://registry.npmjs.org/@xterm/xterm/-/xterm-5.5.0.tgz
tar xzf xterm.tgz package/lib/xterm.js
sha256sum package/lib/xterm.js internal/api/web/vendor/xterm.js
```

## Upgrading

Take the core and every addon from the same release wave, replace all four files, and check the terminal
actually renders on the GPU afterwards. The check is not "does it work": the DOM fallback also works. Open dev
tools and look for a `<canvas>` under `#t-screen` rather than rows of `<span>` with `xterm-dom-renderer-owner`
on the container.
