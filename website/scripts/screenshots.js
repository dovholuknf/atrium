// Screenshots of the landing page at desktop and phone width, light and dark, into website/screenshots/.
// Needs the built site served, e.g. `npm run build && npx docusaurus serve --port 3031`, and Playwright on
// NODE_PATH. Usage: node scripts/screenshots.js [base-url]

const path = require('path');
const {chromium} = require('playwright');

const base = process.argv[2] || 'http://localhost:3031/atrium/';
const out = path.join(__dirname, '..', 'screenshots');

const shots = [
  {name: 'desktop', viewport: {width: 1440, height: 900}, scale: 1},
  {name: 'phone', viewport: {width: 390, height: 844}, scale: 2},
];

(async () => {
  const browser = await chromium.launch();
  for (const theme of ['dark', 'light']) {
    for (const s of shots) {
      const ctx = await browser.newContext({viewport: s.viewport, deviceScaleFactor: s.scale});
      await ctx.addInitScript((t) => localStorage.setItem('theme', t), theme);
      const page = await ctx.newPage();
      await page.goto(base, {waitUntil: 'networkidle'});
      await page.emulateMedia({reducedMotion: 'reduce'});
      const file = path.join(out, `landing-${s.name}-${theme}.png`);
      await page.screenshot({path: file, fullPage: true});
      await page.screenshot({path: path.join(out, `landing-${s.name}-${theme}-fold.png`)});
      console.log(file);
      await ctx.close();
    }
  }
  await browser.close();
})();
