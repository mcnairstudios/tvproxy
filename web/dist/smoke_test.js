// Smoke test for app.js - exercises every buildCrudPage renderer
// Catches TDZ errors, reference errors, and basic runtime failures
// Run: node web/dist/smoke_test.js

const errors = [];
let fetchCallLog = [];

function makeEl(tag) {
  const el = {
    nodeType: 1, tagName: tag.toUpperCase(), children: [], childNodes: [], style: {}, dataset: {},
    className: '', innerHTML: '', textContent: '', value: '', checked: false, disabled: false,
    open: false, selected: false, type: '',
    appendChild(c) { if (c) { this.children.push(c); this.childNodes.push(c); } return c; },
    removeChild(c) { this.children = this.children.filter(x => x !== c); return c; },
    setAttribute(k, v) { this[k] = v; },
    getAttribute(k) { return this[k]; },
    addEventListener() {},
    removeEventListener() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    remove() {},
    focus() {},
    click() {},
    get lastChild() { return this.children[this.children.length - 1] || null; },
    closest() { return null; },
    contains() { return false; },
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    scrollIntoView() {},
    get parentElement() { return null; },
  };
  if (tag === 'select') {
    el.options = [];
    const origAppend = el.appendChild.bind(el);
    el.appendChild = function(c) { if (c) this.options.push(c); return origAppend(c); };
  }
  return el;
}

global.document = {
  createElement(tag) { return makeEl(tag); },
  createTextNode(t) { return { nodeType: 3, textContent: String(t) }; },
  getElementById() { return makeEl('div'); },
  addEventListener() {},
  removeEventListener() {},
  querySelector() { return null; },
  body: { appendChild() {} },
};

global.window = { location: { hostname: 'localhost', hash: '', origin: 'http://localhost' }, _testPages: null };
global.localStorage = { getItem() { return null; }, setItem() {}, removeItem() {} };
const realSetTimeout = setTimeout;
global.setTimeout = (fn, ms) => realSetTimeout(fn, ms || 0);
global.clearTimeout = () => {};
global.confirm = () => true;
global.MutationObserver = class { observe() {} disconnect() {} };

global.fetch = async (path) => {
  fetchCallLog.push(path);
  var body = '[]';
  if (typeof path === 'string' && path.indexOf('/api/wireguard/status') !== -1) {
    body = JSON.stringify({ state: 'unconfigured', config: {} });
  }
  return {
    ok: true,
    status: 200,
    text: async () => body,
    json: async () => JSON.parse(body),
  };
};

// Patch app.js to expose pages — inject a hook right before the IIFE closes
const fs = require('fs');
const originalCode = fs.readFileSync(__dirname + '/app.js', 'utf8');

// We'll use a different approach: re-create buildCrudPage calls directly
// by extracting the function and calling it with test configs.
// Instead, let's just verify TDZ safety by simulating what buildCrudPage does.

// Actually, simplest approach: temporarily modify the code to expose pages
const patchedCode = originalCode.replace(
  '  init();\n})();',
  '  init();\n  if (typeof global !== "undefined") global._testPages = pages;\n})();'
);

// Execute the patched code
try {
  new Function(patchedCode)();
} catch (err) {
  console.error('FAIL: App failed to load:', err.message);
  process.exit(1);
}

realSetTimeout(async () => {
  console.log('App loaded OK');

  const pages = global._testPages;
  if (!pages) {
    console.error('FAIL: Could not access pages object');
    process.exit(1);
  }

  // Test each page renderer
  const pageNames = [
    'm3u-accounts',
    'channels',        // has groupBy — this is the key one to test
    'channel-groups',
    'epg-sources',
    'stream-profiles',
    'hdhr-devices',
    'logos',
    'users',
    'recordings',
    'wireguard',
  ];

  for (const name of pageNames) {
    const renderer = pages[name];
    if (!renderer || typeof renderer !== 'function') {
      console.log('  SKIP: ' + name + ' (not a function)');
      continue;
    }

    const container = makeEl('div');
    fetchCallLog = [];
    try {
      await renderer(container);
      // Check rendered DOM for error messages (buildCrudPage catches errors internally)
      function findText(el) {
        if (!el) return '';
        let t = el.textContent || '';
        if (el.children) el.children.forEach(c => { t += ' ' + findText(c); });
        return t;
      }
      const text = findText(container);
      if (text.indexOf('Failed to load') !== -1) {
        const msg = text.match(/Failed to load:\s*([^\n]+)/);
        console.error('  FAIL: ' + name + ' — ' + (msg ? msg[1].trim() : 'render error'));
        errors.push(name + ': ' + (msg ? msg[1].trim() : 'render error'));
      } else {
        console.log('  OK: ' + name);
      }
    } catch (err) {
      console.error('  FAIL: ' + name + ' — ' + err.message);
      errors.push(name + ': ' + err.message);
    }
  }

  if (errors.length > 0) {
    console.error('\n' + errors.length + ' page(s) failed:');
    errors.forEach(e => console.error('  - ' + e));
    process.exit(1);
  }

  console.log('\nAll page smoke tests passed.');
  process.exit(0);
}, 200);
