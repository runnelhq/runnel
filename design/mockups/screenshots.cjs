// Creates the PNG screenshots of a mockup for its PR: one per state and theme.
// Requires Playwright (npm i -g playwright, or an existing installation).
//
//   node design/mockups/screenshots.cjs e4-editor-shell file,empty,offline,error
//
// Writes design/mockups/<name>-<state>-hell.png and …-dunkel.png (1280 × 800, 2× scale).
const { chromium } = require('playwright');
const path = require('path');

(async () => {
  const [name, statesArg = 'default'] = process.argv.slice(2);
  if (!name) {
    console.error('usage: node screenshots.cjs <mockup-name> [state,state,…]');
    process.exit(2);
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2 });
  for (const state of statesArg.split(',')) {
    for (const [theme, suffix] of [['light', 'hell'], ['dark', 'dunkel']]) {
      const url = 'file://' + path.join(__dirname, name + '.html') + `?theme=${theme}&state=${state}&controls=0`;
      await page.goto(url);
      await page.waitForLoadState('networkidle');
      const out = path.join(__dirname, `${name}-${state}-${suffix}.png`);
      await page.screenshot({ path: out });
      console.log(out);
    }
  }
  await browser.close();
})();
