// Job Search, browser build. This service worker runs the Go app (app.wasm)
// and answers every request for the site with it; nothing is sent to a
// server. buildweb stamps the version so each release loads its own app.wasm.
importScripts('wasm_exec.js');

const VERSION = '__VERSION__';
const SHELL = new Set(['index.html', '404.html', 'sw.js', 'wasm_exec.js', 'app.wasm']);

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

// boot starts the Go program once and resolves with its request handler. If
// it fails or the program stops, the next request starts it again.
let ready = null;
function boot() {
  if (!ready) {
    ready = new Promise((resolve, reject) => {
      self.goReady = resolve;
      const go = new Go();
      WebAssembly.instantiateStreaming(fetch('app.wasm?v=' + VERSION), go.importObject)
        .then(r => go.run(r.instance))
        .then(() => { ready = null; reject(new Error('the app stopped')); },
              err => { ready = null; reject(err); });
    });
  }
  return ready;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

function errorPage(status, title, text, err) {
  const page = '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">' +
    '<title>' + title + '</title><body style="font:16px/1.5 system-ui,sans-serif;max-width:60ch;margin:48px auto;padding:0 24px;color:#17212e">' +
    '<h1 style="font-size:24px">' + title + '</h1><p>' + text + ' (' + escapeHTML(err && err.message || err) +
    ').</p><p><a href="">Reload the page</a> to try again.</p></body>';
  return new Response(page, { status, headers: { 'Content-Type': 'text/html; charset=utf-8' } });
}

// The app did not start: nothing was read or changed.
const bootFailure = err => errorPage(503, 'Job Search could not start',
  'The app could not load in this browser, so nothing was read or changed', err);
// The app started but this one request failed.
const requestFailure = err => errorPage(500, 'Something went wrong',
  'The app could not answer this request. If you were saving a change, check whether it was saved', err);

self.addEventListener('fetch', e => {
  const scope = self.registration.scope;
  const url = e.request.url;
  if (!url.startsWith(scope)) return;
  const rest = url.slice(scope.length).split(/[?#]/)[0];
  // The shell files come from the network, except the site root, which is
  // the app's job list.
  if (SHELL.has(rest)) return;
  e.respondWith(boot().then(handle => handle(e.request).catch(requestFailure), bootFailure));
});
