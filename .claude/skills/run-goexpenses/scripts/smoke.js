// Logs in, creates an expense type, saves a post with a changed date and
// screenshots /posts. Copy and adapt for the flow you are checking.
// Usage: node smoke.js [baseUrl] [screenshotDir]
const { chromium } = require('playwright');
const base = process.argv[2] || 'http://localhost:8091';
const out = process.argv[3] || '.';
(async () => {
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await (await b.newContext({ viewport: { width: 1400, height: 1000 } })).newPage();
  // AdSense and Google Analytics always fail offline; ignore them.
  p.on('requestfailed', r => !/googlesyndication|google-analytics|googletagmanager/.test(r.url()) && console.log('FAILED', r.url()));
  p.on('pageerror', e => console.log('PAGE ERROR', e.message));

  await p.goto(base + '/login');
  await p.fill('input[name=username]', 'tester');
  await p.fill('input[name=password]', 'Lozinka12345!');
  await Promise.all([p.waitForNavigation(), p.click('form button[type=submit]')]);

  await p.goto(base + '/expenses');
  await p.fill('form[action="/expenses/save"] input[name=description]', 'EPS (Struja)');
  await Promise.all([p.waitForNavigation(), p.click('form[action="/expenses/save"] button[type=submit]')]);

  await p.goto(base + '/posts');
  await p.fill('#description', 'Smoke');
  await p.fill('#date', '2026-11-03');
  await p.fill('#amount', '1000');
  await Promise.all([p.waitForNavigation(), p.click('form[action="/posts/save"] button[type=submit]')]);
  console.log('after save:', p.url(), 'date field:', await p.inputValue('#date'));
  await p.screenshot({ path: out + '/posts.png' });
  console.log('screenshot:', out + '/posts.png');
  await b.close();
})();
