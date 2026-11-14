// Registers the test user through the UI (CSRF is handled by the page).
const { chromium } = require('playwright');
const base = process.argv[2] || 'http://localhost:8091';
(async () => {
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await (await b.newContext()).newPage();
  await p.goto(base + '/register');
  await p.fill('input[name=name]', 'Test');
  await p.fill('input[name=username]', 'tester');
  await p.fill('input[name=email]', 'tester@example.com');
  await p.fill('input[name=password]', 'Lozinka12345!');
  await Promise.all([p.waitForNavigation(), p.click('form button[type=submit]')]);
  await b.close();
})();
