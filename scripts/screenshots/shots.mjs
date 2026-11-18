// Takes every app screenshot used by docs/ and the README, from the app served in server mode
// against the demo data made by gen_mock.py. Run through run.sh, which sets all of that up.
//
// Usage: node shots.mjs <out-dir> [base-url]
// Writes <name>.png at 2880×1800 (1440×900 CSS pixels at 2×); run.sh scales them down and converts to WebP.
import { chromium } from 'playwright';

const [out, base = 'http://localhost:18765/'] = process.argv.slice(2);
if (!out) throw new Error('usage: node shots.mjs <out-dir> [base-url]');

/** The session every History-style shot opens: the one with the failing test runs and a subagent. */
const PROJECT = 'acme-storefront';
const SESSION = 'The checkout total is off by one cent';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));

const wait = (ms = 900) => page.waitForTimeout(ms);
const shot = async (name) => {
  await wait(1500);
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log('shot', name);
};
const nav = async (label) => {
  await page.locator('button[snSidebarItem], button.sn-list-item', { hasText: label }).first().click();
  await wait();
};
/** Picks the first option of a settings <select> whose text matches. */
const pick = async (label, re) => {
  await nav('Sources & settings');
  const sel = page.getByLabel(label, { exact: true });
  const opt = (await sel.locator('option').allTextContents()).find((t) => re.test(t));
  if (!opt) throw new Error(`no ${label} option matches ${re}`);
  await sel.selectOption({ label: opt });
  await wait(1500);
};
const look = async (skin, scheme) => {
  await pick('Look', skin);
  await pick('Color scheme', scheme);
};

const folder = (name) => page.locator(`sn-tree-item[aria-level="1"][aria-label="${name}"]`);

/** History with the demo session open: only its project expanded, sorted by cost, the first turn's steps shown. */
const openSession = async () => {
  await nav('History');
  await folder(PROJECT).locator(':scope > .row').click();
  await wait();
  // Close every other project folder, so the tree shows just this project's sessions.
  for (const item of await page.locator('sn-tree-item[aria-level="1"][aria-expanded="true"]').all()) {
    if ((await item.getAttribute('aria-label')) !== PROJECT) await item.locator(':scope > .row > .expander').click();
  }
  await page.locator('cz-segmented', { hasText: 'Cost' }).first().getByText('Cost', { exact: true }).click();
  await wait();
  await folder(PROJECT).locator(`sn-tree-item[aria-label^="${SESSION}"]`).first().click();
  await wait(1500);
  // Expand the first turn, unless it is still open from an earlier pass (the button toggles).
  const turn = page.locator('button', { hasText: 'Worked for' }).first();
  if (!(await turn.locator('span.rotate-90').count())) await turn.click();
};

await page.goto(base);
await wait(3000);

// ---- macOS 26, light: the main set ----
await look(/^macOS/, /Light/i);
await openSession();
await shot('history');
await page.locator('text=npm test').first().click();
await shot('step-detail');
await page.keyboard.press('Escape');
await wait();
for (const [label, name] of [['Session analysis', 'analysis'], ['Cost explained', 'costs'], ['Overview', 'overview'], ['Calendar', 'calendar']]) {
  await nav(label);
  await shot(name);
}
await page.locator('text=TODAY').first().click();
await shot('calendar-day');
await nav('Sources & settings');
await shot('settings');

await openSession();
await page.getByText('Insights (', { exact: false }).first().click();
await wait();
await page.getByText('Show whole session').first().click().catch(() => {});
await wait();
await shot('insights');

// ---- macOS 26, dark ----
await look(/^macOS/, /Dark/i);
await openSession();
await shot('history-dark');
await nav('Session analysis');
await shot('analysis-dark');

// ---- Windows 11 ----
await look(/Windows 11/, /Light/i);
await openSession();
await shot('windows-history');
await look(/Windows 11/, /Dark/i);
await nav('Overview');
await shot('windows-overview-dark');

// ---- Linux, GNOME / GTK ----
await look(/^GNOME/, /Light/i);
await openSession();
await shot('linux-history');
await look(/^GNOME/, /Dark/i);
await openSession();
await shot('linux-history-dark');
await nav('Session analysis');
await shot('linux-analysis-dark');

// Back to following the system, as a new install does.
await look(/^System/, /^System/);

// Nothing personal may end up in a screenshot: the fake HOME is shown as ~, projects live under /Users/demo.
const text = await page.evaluate(() => document.body.innerText);
if (/\/private\/|\/tmp\/|scratchpad/.test(text)) errors.push('a real filesystem path is visible in the UI');

await browser.close();
if (errors.length) {
  console.error('errors:\n' + errors.join('\n'));
  process.exit(1);
}
