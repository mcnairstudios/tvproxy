(function() {
  'use strict';

  var jsErrorLog = [];
  var jsErrorFlushTimer = null;
  var jsErrorEnabled = false;
  function reportJSError(msg, source, line, col, err) {
    if (!jsErrorEnabled) return;
    jsErrorLog.push({ msg: String(msg), source: (source || '').split('/').pop(), line: line || 0, col: col || 0, stack: err && err.stack ? err.stack.split('\n').slice(0, 3).join(' | ') : '' });
    if (!jsErrorFlushTimer) jsErrorFlushTimer = setTimeout(flushJSErrors, 2000);
  }
  function flushJSErrors() {
    jsErrorFlushTimer = null;
    if (jsErrorLog.length === 0) return;
    var batch = jsErrorLog.splice(0, 20);
    var token = localStorage.getItem('access_token') || '';
    fetch('/api/frontend-errors', { method: 'POST', headers: { 'Content-Type': 'application/json', 'Authorization': token ? 'Bearer ' + token : '' }, body: JSON.stringify({ errors: batch }) }).catch(function() {});
  }
  window.onerror = function(msg, source, line, col, err) { reportJSError(msg, source, line, col, err); };
  window.addEventListener('unhandledrejection', function(e) { reportJSError('Unhandled rejection: ' + (e.reason && e.reason.message || e.reason || '?'), '', 0, 0, e.reason); });

  function createDVRTracker(isLive, duration) {
    var buffered = 0;
    var dur = duration || 0;
    var live = isLive;

    return {
      getPos: function(videoCurrentTime) {
        return videoCurrentTime || 0;
      },

      updateBuffered: function(b) { buffered = b; },
      getBuffered: function() { return buffered; },

      setDuration: function(d) {
        if (d > 0) { dur = d; live = false; }
      },
      isLive: function() { return live; },

      reset: function() {},

      getDisplay: function(videoCurrentTime) {
        var pos = videoCurrentTime || 0;
        var total = live ? buffered : (dur || buffered);
        var pct = total > 0 ? Math.min(100, Math.max(0, (pos / total) * 100)) : 0;
        return { pos: pos, total: total, pct: pct };
      },
    };
  }

  const state = {
    user: null,
    accessToken: localStorage.getItem('access_token'),
    refreshToken: localStorage.getItem('refresh_token'),
    currentPage: 'dashboard',
  };

  const api = {
    _etags: {},

    async request(method, path, body, fetchOpts) {
      const headers = { 'Content-Type': 'application/json' };
      if (state.accessToken) {
        headers['Authorization'] = 'Bearer ' + state.accessToken;
      }
      const opts = Object.assign({ method, headers }, fetchOpts);
      if (body) opts.body = JSON.stringify(body);

      let resp = await fetch(path, opts);

      if (resp.status === 401 && state.refreshToken && path !== '/api/auth/refresh') {
        const refreshed = await api.refreshToken();
        if (refreshed) {
          headers['Authorization'] = 'Bearer ' + state.accessToken;
          opts.headers = headers;
          resp = await fetch(path, opts);
        } else {
          auth.logout();
          return null;
        }
      }

      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: resp.statusText }));
        throw new Error(err.error || 'Request failed');
      }

      var responseEtag = resp.headers.get('ETag');
      if (responseEtag && method === 'GET') api._etags[path] = responseEtag;

      const text = await resp.text();
      return text ? JSON.parse(text) : null;
    },

    get(path, fetchOpts) { return this.request('GET', path, null, fetchOpts); },
    post(path, body) { return this.request('POST', path, body); },
    put(path, body) { return this.request('PUT', path, body); },
    del(path) { return this.request('DELETE', path); },

    async getConditional(path, etag) {
      var headers = {};
      if (state.accessToken) headers['Authorization'] = 'Bearer ' + state.accessToken;
      if (etag) headers['If-None-Match'] = etag;
      var resp = await fetch(path, { method: 'GET', headers: headers });
      if (resp.status === 401 && state.refreshToken) {
        var refreshed = await api.refreshToken();
        if (refreshed) {
          headers['Authorization'] = 'Bearer ' + state.accessToken;
          resp = await fetch(path, { method: 'GET', headers: headers });
        }
      }
      if (resp.status === 304) return { status: 304, data: null, etag: etag };
      if (!resp.ok) return { status: resp.status, data: null, etag: null };
      var newEtag = resp.headers.get('ETag');
      if (newEtag) api._etags[path] = newEtag;
      var text = await resp.text();
      return { status: 200, data: text ? JSON.parse(text) : null, etag: newEtag || etag };
    },

    async refreshToken() {
      try {
        const resp = await fetch('/api/auth/refresh', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: state.refreshToken }),
        });
        if (!resp.ok) return false;
        const data = await resp.json();
        state.accessToken = data.access_token;
        localStorage.setItem('access_token', data.access_token);
        if (data.refresh_token) {
          state.refreshToken = data.refresh_token;
          localStorage.setItem('refresh_token', data.refresh_token);
        }
        return true;
      } catch {
        return false;
      }
    },
  };

  const auth = {
    async login(username, password) {
      const resp = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}));
        throw new Error(err.error || 'Login failed');
      }
      const data = await resp.json();
      state.accessToken = data.access_token;
      state.refreshToken = data.refresh_token;
      localStorage.setItem('access_token', data.access_token);
      localStorage.setItem('refresh_token', data.refresh_token);
      auth.invalidateCaches();
      await auth.fetchUser();
    },

    async fetchUser() {
      try {
        state.user = await api.get('/api/auth/me');
        if (state.user && state.user.is_admin) {
          api.get('/api/settings').then(function(settings) {
            jsErrorEnabled = (Array.isArray(settings) ? settings : []).some(function(s) { return s.key === 'debug_enabled' && s.value === 'true'; });
          }).catch(function() {});
        }
      } catch {
        state.user = null;
      }
    },

    invalidateCaches() {
      channelsCache.invalidate();
      channelGroupsCache.invalidate();
      streamsCache.invalidate();
      epgCache.invalidate();
      logosCache.invalidate();
    },

    logout() {
      api.post('/api/auth/logout').catch(() => {});
      state.user = null;
      state.accessToken = null;
      state.refreshToken = null;
      localStorage.removeItem('access_token');
      localStorage.removeItem('refresh_token');
      auth.invalidateCaches();
      render();
    },

    isLoggedIn() {
      return !!state.accessToken && !!state.user;
    },
  };

  const toast = {
    show(message, type = 'info') {
      let container = document.querySelector('.toast-container');
      if (!container) {
        container = document.createElement('div');
        container.className = 'toast-container';
        document.body.appendChild(container);
      }
      const el = document.createElement('div');
      el.className = 'toast toast-' + type;
      el.textContent = message;
      container.appendChild(el);
      setTimeout(() => el.remove(), 4000);
    },
    success(msg) { this.show(msg, 'success'); },
    error(msg) { this.show(msg, 'error'); },
    info(msg) { this.show(msg, 'info'); },
  };

  var _favoriteIds = null;

  function loadFavorites() {
    return api.get('/api/favorites').then(function(ids) {
      _favoriteIds = new Set(ids || []);
      return _favoriteIds;
    }).catch(function() {
      _favoriteIds = new Set();
      return _favoriteIds;
    });
  }

  function favoriteButton(itemId) {
    var span = document.createElement('span');
    span.style.cssText = 'cursor:pointer;font-size:18px;user-select:none;';
    var updating = false;

    function render() {
      if (_favoriteIds && _favoriteIds.has(itemId)) {
        span.textContent = '\u2B50';
      } else {
        span.textContent = '\u2606';
        span.style.color = '#888';
      }
    }

    function init() {
      if (_favoriteIds) {
        render();
      } else {
        span.textContent = '\u2606';
        span.style.color = '#888';
        loadFavorites().then(render);
      }
    }

    span.onclick = function(e) {
      e.stopPropagation();
      if (updating) return;
      updating = true;
      var isFav = _favoriteIds && _favoriteIds.has(itemId);
      if (isFav) {
        api.del('/api/favorites/' + itemId).then(function() {
          _favoriteIds.delete(itemId);
          render();
          toast.success('Removed from favorites');
        }).catch(function() {
          toast.error('Failed to update favorite');
        }).finally(function() { updating = false; });
      } else {
        api.post('/api/favorites/' + itemId).then(function() {
          if (!_favoriteIds) _favoriteIds = new Set();
          _favoriteIds.add(itemId);
          render();
          toast.success('Added to favorites');
        }).catch(function() {
          toast.error('Failed to update favorite');
        }).finally(function() { updating = false; });
      }
    };

    init();
    return span;
  }

  class DataCache {
    constructor({ loader, searchKeys, label, storageKey, etagEndpoint }) {
      this._loader = loader;
      this._searchKeys = searchKeys;
      this._storageKey = storageKey ? 'tvproxy_' + storageKey : null;
      this._etagEndpoint = etagEndpoint || null;
      this._etag = null;
      this._data = null;
      this._index = null;
      this._promise = null;
      this.label = label || 'Data';
      this.state = 'idle'; // idle | loading | ready
      this.count = 0;
    }

    _loadFromStorage() {
      if (!this._storageKey) return false;
      try {
        const raw = localStorage.getItem(this._storageKey);
        if (!raw) return false;
        this._data = JSON.parse(raw);
        this._buildIndex();
        if (this._etagEndpoint) {
          this._etag = localStorage.getItem(this._storageKey + '_etag') || null;
        }
        this.state = 'ready';
        this.count = this._data.length;
        DataCache._notify();
        return true;
      } catch { return false; }
    }

    _saveToStorage() {
      if (!this._storageKey || !this._data) return;
      if (this._data.length === 0) {
        localStorage.removeItem(this._storageKey);
        localStorage.removeItem(this._storageKey + '_etag');
        this._etag = null;
        return;
      }
      try {
        localStorage.setItem(this._storageKey, JSON.stringify(this._data));
        if (this._etagEndpoint && this._etag) {
          localStorage.setItem(this._storageKey + '_etag', this._etag);
        }
      } catch {}
    }

    async getAll() {
      if (this._data) return this._data;
      if (this._promise) return this._promise;
      if (this._loadFromStorage()) {
        this._refreshInBackground();
        return this._data;
      }
      this.state = 'loading';
      DataCache._notify();
      this._promise = (async () => {
        try { this._data = await this._loader(); } catch { this._data = []; }
        this._buildIndex();
        if (this._etagEndpoint) this._etag = api._etags[this._etagEndpoint] || null;
        this._saveToStorage();
        this.state = 'ready';
        this.count = this._data.length;
        this._promise = null;
        DataCache._notify();
        return this._data;
      })();
      return this._promise;
    }

    _refreshInBackground() {
      (async () => {
        try {
          if (this._etagEndpoint && this._etag && this._data && this._data.length > 0) {
            var check = await api.getConditional(this._etagEndpoint, this._etag);
            if (check.status === 304) return;
          }
          var fresh = await this._loader();
          this._data = fresh;
          this._buildIndex();
          if (this._etagEndpoint) this._etag = api._etags[this._etagEndpoint] || null;
          this._saveToStorage();
          this.count = this._data.length;
          DataCache._notify();
        } catch {}
      })();
    }

    _buildIndex() {
      const keys = this._searchKeys;
      this._index = new Array(this._data.length);
      for (let i = 0; i < this._data.length; i++) {
        const parts = [];
        for (let k = 0; k < keys.length; k++) {
          const val = this._data[i][keys[k]];
          if (val != null) parts.push(String(val));
        }
        this._index[i] = parts.join(' ').toLowerCase();
      }
    }

    search(query, limit) {
      if (!this._data || !query) return [];
      limit = limit || 50;
      const q = query.toLowerCase();
      const startsWith = [];
      const contains = [];
      for (let i = 0; i < this._data.length; i++) {
        if (this._index[i].indexOf(q) === 0) {
          startsWith.push(this._data[i]);
        } else if (this._index[i].indexOf(q) !== -1) {
          contains.push(this._data[i]);
        }
        if (startsWith.length + contains.length >= limit * 2) break;
      }
      return startsWith.concat(contains).slice(0, limit);
    }

    invalidate() {
      this._data = null;
      this._index = null;
      this._promise = null;
      this._etag = null;
      this.state = 'idle';
      this.count = 0;
      if (this._storageKey) {
        try {
          localStorage.removeItem(this._storageKey);
          localStorage.removeItem(this._storageKey + '_etag');
        } catch {}
      }
      DataCache._notify();
    }
  }
  DataCache._listeners = [];
  DataCache.onChange = function(fn) { DataCache._listeners.push(fn); return fn; };
  DataCache.offChange = function(fn) { DataCache._listeners = DataCache._listeners.filter(function(f) { return f !== fn; }); };
  DataCache._notify = function() { DataCache._listeners.forEach(function(fn) { fn(); }); };

  const epgCache = new DataCache({
    label: 'EPG',
    loader: async () => {
      const [epgData, sources] = await Promise.all([
        api.get('/api/epg/data'),
        api.get('/api/epg/sources').catch(() => []),
      ]);
      const nameMap = {};
      sources.forEach(s => { nameMap[s.id] = s.name; });
      const filtered = epgData.filter(e => nameMap[e.epg_source_id]);
      filtered.forEach(e => { e._display_name = nameMap[e.epg_source_id] + '/' + e.name; });
      return filtered;
    },
    searchKeys: ['_display_name', 'channel_id'],
    storageKey: 'epg',
    etagEndpoint: '/api/epg/data',
  });

  const logosCache = new DataCache({
    label: 'Logos',
    loader: () => api.get('/api/logos'),
    searchKeys: ['name', 'url'],
    storageKey: 'logos',
    etagEndpoint: '/api/logos',
  });

  const streamsCache = new DataCache({
    label: 'Streams',
    loader: async () => {
      const [streams, accounts, satipSources] = await Promise.all([
        api.get('/api/streams'),
        api.get('/api/m3u/accounts').catch(() => []),
        api.get('/api/satip/sources').catch(() => []),
      ]);
      const nameMap = {};
      accounts.forEach(a => { nameMap[a.id] = a.name; });
      const satipMap = {};
      satipSources.forEach(s => { satipMap[s.id] = s.name; });
      streams.forEach(s => {
        if (s.satip_source_id) {
          s._display_name = (satipMap[s.satip_source_id] || 'SAT>IP') + '/' + s.name;
        } else {
          s._display_name = (nameMap[s.m3u_account_id] || '') + '/' + s.name;
        }
      });
      for (var k in streamGroupsCache) delete streamGroupsCache[k];
      return streams;
    },
    searchKeys: ['_display_name', 'group'],
    storageKey: 'streams',
    etagEndpoint: '/api/streams',
  });


  const channelsCache = new DataCache({
    label: 'Channels',
    loader: async () => {
      const [channels, nowMap] = await Promise.all([
        api.get('/api/channels', { cache: 'no-store' }),
        api.get('/api/epg/now?full=true').catch(() => ({})),
      ]);
      channels.forEach(ch => {
        var prog = ch.tvg_id && nowMap[ch.tvg_id];
        if (prog && typeof prog === 'object') {
          ch._now_playing = prog.title || '';
          ch._now_program = prog;
        } else {
          ch._now_playing = (typeof prog === 'string' ? prog : '') || '';
          ch._now_program = null;
        }
      });
      return channels;
    },
    searchKeys: ['name', 'tvg_id'],
  });

  const channelGroupsCache = new DataCache({
    label: 'Groups',
    loader: () => api.get('/api/channel-groups', { cache: 'no-store' }),
    searchKeys: ['name'],
  });

  const streamProfilesCache = new DataCache({
    label: 'Profiles',
    loader: () => api.get('/api/stream-profiles'),
    searchKeys: ['name'],
  });

  function navigate(page) {
    state.currentPage = page;
    render();
  }

  function matchesSearch(text, query) {
    var words = query.split(/\s+/).filter(Boolean);
    for (var i = 0; i < words.length; i++) {
      if (text.indexOf(words[i]) === -1) return false;
    }
    return true;
  }

  function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

  function setupGridKeyJump(grid) {
    function onKey(e) {
      if (!document.body.contains(grid)) { document.removeEventListener('keydown', onKey); return; }
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.isContentEditable) return;
      if (e.key.length !== 1 || e.ctrlKey || e.metaKey || e.altKey) return;
      var letter = e.key.toUpperCase();
      var cards = grid.querySelectorAll('[data-sort-name]');
      for (var i = 0; i < cards.length; i++) {
        var name = (cards[i].dataset.sortName || '').toUpperCase();
        if (name >= letter) {
          cards[i].scrollIntoView({ behavior: 'smooth', block: 'center' });
          break;
        }
      }
    }
    document.addEventListener('keydown', onKey);
  }

  function h(tag, attrs, ...children) {
    const el = document.createElement(tag);
    if (attrs) {
      for (const [k, v] of Object.entries(attrs)) {
        if (k.startsWith('on') && typeof v === 'function') {
          el.addEventListener(k.slice(2).toLowerCase(), v);
        } else if (k === 'className') {
          el.className = v;
        } else if (k === 'innerHTML') {
          el.innerHTML = v;
        } else {
          el.setAttribute(k, v);
        }
      }
    }
    for (const child of children.flat()) {
      if (child == null) continue;
      el.appendChild(typeof child === 'string' ? document.createTextNode(child) : child);
    }
    return el;
  }

  function fmtLocalDateTime(iso) {
    if (!iso) return '-';
    return new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', timeZoneName: 'short' });
  }

  function fmtUTC(iso) {
    if (!iso) return '';
    return new Date(iso).toLocaleString('en-GB', { timeZone: 'UTC', day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }) + ' UTC';
  }

  function showModal(title, bodyEl, onSave, saveLabel) {
    const overlay = h('div', { className: 'modal-overlay' });
    const modal = h('div', { className: 'modal' },
      h('div', { className: 'modal-header' },
        h('h3', null, title),
        h('button', { className: 'modal-close', onClick: () => overlay.remove() }, '\u00d7'),
      ),
      h('div', { className: 'modal-body' }, bodyEl),
      h('div', { className: 'modal-footer' },
        h('button', { className: 'btn btn-secondary', onClick: () => overlay.remove() }, 'Cancel'),
        onSave ? h('button', { className: 'btn btn-primary', onClick: async (e) => {
          e.target.disabled = true;
          try {
            await onSave();
            overlay.remove();
          } catch (err) {
            toast.error(err.message);
            e.target.disabled = false;
          }
        } }, saveLabel || 'Save') : null,
      ),
    );
    overlay.appendChild(modal);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) overlay.remove(); });
    function onEscape(e) { if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', onEscape); } }
    document.addEventListener('keydown', onEscape);
    document.body.appendChild(overlay);
    const firstInput = bodyEl.querySelector('input, select, textarea');
    if (firstInput) firstInput.focus();
    return overlay;
  }

  function confirmDialog(message) {
    return new Promise((resolve) => {
      const body = h('p', null, message);
      const overlay = showModal('Confirm', body, () => resolve(true), 'Confirm');
      overlay.querySelector('.btn-secondary').addEventListener('click', () => resolve(false));
    });
  }

  function renderLoginPage() {
    const app = document.getElementById('app');
    app.innerHTML = '';

    const errorEl = h('div', { className: 'error-msg' });
    const usernameInput = h('input', { type: 'text', placeholder: 'Username', id: 'login-user' });
    const passwordInput = h('input', { type: 'password', placeholder: 'Password', id: 'login-pass' });
    const submitBtn = h('button', { className: 'btn btn-primary btn-block', type: 'submit' }, 'Sign In');

    const form = h('form', {
      onSubmit: async (e) => {
        e.preventDefault();
        errorEl.classList.remove('visible');
        submitBtn.disabled = true;
        submitBtn.textContent = 'Signing in...';
        try {
          await auth.login(usernameInput.value, passwordInput.value);
          render();
          rebuildStreamNav();
          streamsCache.getAll();
          epgCache.getAll();
          logosCache.getAll();
          channelsCache.getAll();
          channelGroupsCache.getAll();
        } catch (err) {
          errorEl.textContent = err.message;
          errorEl.classList.add('visible');
          submitBtn.disabled = false;
          submitBtn.textContent = 'Sign In';
        }
      }
    },
      errorEl,
      h('div', { className: 'form-group' },
        h('label', { for: 'login-user' }, 'Username'),
        usernameInput,
      ),
      h('div', { className: 'form-group' },
        h('label', { for: 'login-pass' }, 'Password'),
        passwordInput,
      ),
      submitBtn,
    );

    app.appendChild(
      h('div', { className: 'login-page' },
        h('div', { className: 'login-card' },
          h('h1', null, 'TVProxy'),
          h('p', { className: 'subtitle' }, 'IPTV Stream Management'),
          form,
        ),
      )
    );

    usernameInput.focus();
  }

  function renderInvitePage() {
    const app = document.getElementById('app');
    app.innerHTML = '';

    const hash = window.location.hash.replace(/^#\/?/, '');
    const match = hash.match(/^invite\/(.+)/);
    const token = match ? match[1] : '';

    if (!token) {
      app.appendChild(h('div', { className: 'login-page' },
        h('div', { className: 'login-card' },
          h('h1', null, 'TVProxy'),
          h('p', { style: 'color: var(--danger)' }, 'Invalid invite link.'),
          h('button', { className: 'btn btn-primary btn-block', onClick: () => { state.currentPage = 'dashboard'; window.location.hash = ''; render(); } }, 'Go to Login'),
        ),
      ));
      return;
    }

    const errorEl = h('div', { className: 'error-msg' });
    const passwordInput = h('input', { type: 'password', placeholder: 'Choose a password', id: 'invite-pass' });
    const confirmInput = h('input', { type: 'password', placeholder: 'Confirm password', id: 'invite-confirm' });
    const submitBtn = h('button', { className: 'btn btn-primary btn-block', type: 'submit' }, 'Activate Account');

    const form = h('form', {
      onSubmit: async (e) => {
        e.preventDefault();
        errorEl.classList.remove('visible');
        if (passwordInput.value !== confirmInput.value) {
          errorEl.textContent = 'Passwords do not match';
          errorEl.classList.add('visible');
          return;
        }
        if (!passwordInput.value) {
          errorEl.textContent = 'Password is required';
          errorEl.classList.add('visible');
          return;
        }
        submitBtn.disabled = true;
        submitBtn.textContent = 'Activating...';
        try {
          await fetch('/api/auth/invite/' + encodeURIComponent(token), {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password: passwordInput.value }),
          }).then(resp => {
            if (!resp.ok) return resp.json().then(data => { throw new Error(data.error || 'Invite failed'); });
          });
          toast.success('Account activated! Please sign in.');
          state.currentPage = 'dashboard';
          window.location.hash = '';
          render();
        } catch (err) {
          errorEl.textContent = err.message;
          errorEl.classList.add('visible');
          submitBtn.disabled = false;
          submitBtn.textContent = 'Activate Account';
        }
      }
    },
      errorEl,
      h('div', { className: 'form-group' },
        h('label', { for: 'invite-pass' }, 'Password'),
        passwordInput,
      ),
      h('div', { className: 'form-group' },
        h('label', { for: 'invite-confirm' }, 'Confirm Password'),
        confirmInput,
      ),
      submitBtn,
    );

    app.appendChild(
      h('div', { className: 'login-page' },
        h('div', { className: 'login-card' },
          h('h1', null, 'TVProxy'),
          h('p', { className: 'subtitle' }, 'Activate Your Account'),
          form,
        ),
      )
    );

    passwordInput.focus();
  }

  let navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: '\u2302', tip: 'Overview of your TVProxy system status', adminOnly: true },
    { id: 'now-playing', label: 'Activity', icon: '\u25B6', tip: 'Active users and streams', adminOnly: true },
    { section: 'Content' },
    { id: 'channels', label: 'Channels', icon: '\ud83d\udcfa', tip: 'Define your custom channels and assign streams and EPG data' },
    { id: 'movies', label: 'Movies', icon: '\uD83C\uDFAC', tip: 'Browse movie library' },
    { id: 'tv-series', label: 'TV Series', icon: '\uD83D\uDCFA', tip: 'Browse TV series library' },
    { id: 'epg-guide', label: 'EPG Guide', icon: '\ud83d\udcf0', tip: 'TV programme guide grid for your channels' },
    { id: 'recordings', label: 'Recordings', icon: '\ud83d\udd34', tip: 'View active and completed recordings', iconStyle: 'font-size:0.75em' },
    { id: 'favorites', label: 'Favorites', icon: '\u2B50', tip: 'Your favorite channels and streams' },
    { section: 'Streams' },
    { section: 'Sources', adminOnly: true },
    { id: 'm3u-accounts', label: 'M3U Accounts', icon: '\u2630', tip: 'Add your SAT>IP or IPTV source M3U files', adminOnly: true },
    { id: 'satip-sources', label: 'SAT>IP Sources', icon: '\ud83d\udce1', tip: 'Scan MiniSAT>IP devices for channels', adminOnly: true },
    { id: 'epg-sources', label: 'EPG Sources', icon: '\ud83d\udcc5', tip: 'Manage XMLTV EPG data sources for programme guides', adminOnly: true },
    { section: 'Stream Management', adminOnly: true },
    { id: 'channel-groups', label: 'Channel Groups', icon: '\ud83d\udcc2', tip: 'Organise channels into groups for Jellyfin and output', adminOnly: true },
    { id: 'clients', label: 'Client Detection', icon: '\ud83d\udd0d', tip: 'Auto-detect players by HTTP headers and assign stream profiles', adminOnly: true },
    { id: 'stream-profiles', label: 'Stream Profiles', icon: '\ud83d\udd27', tip: 'Configure transcoding profiles for stream processing', adminOnly: true },
    { id: 'hdhr-devices', label: 'HDHR Devices', icon: '\ud83d\udce1', tip: 'Virtual HDHomeRun devices for Plex, Jellyfin, and Emby', adminOnly: true },
    { section: 'System', adminOnly: true },
    { id: 'settings', label: 'Settings', icon: '\u2699', tip: 'Core application settings', adminOnly: true },
    { id: 'users', label: 'Users', icon: '\ud83d\udc65', tip: 'Manage admin and user accounts', adminOnly: true },
    { id: 'logos', label: 'Logos', icon: '\ud83d\uddbc', tip: 'Saved channel logos for quick reuse', adminOnly: true },
    { id: 'wireguard', label: 'WireGuard', icon: '\ud83d\udd12', tip: 'WireGuard VPN tunnel for geo-unblocking', adminOnly: true },
  ];

  const tooltipEl = h('div', { className: 'nav-tooltip' });
  tooltipEl.style.cssText = 'position:fixed;background:var(--bg-elevated);color:var(--text-primary);border:1px solid var(--border);border-radius:var(--radius-sm);padding:6px 10px;font-size:12px;max-width:240px;pointer-events:none;opacity:0;transition:opacity 0.15s;z-index:9999;box-shadow:0 4px 12px rgba(0,0,0,0.3);';
  document.body.appendChild(tooltipEl);
  let tooltipTimer = null;

  function showTooltip(el, text) {
    tooltipTimer = setTimeout(() => {
      const rect = el.getBoundingClientRect();
      tooltipEl.textContent = text;
      tooltipEl.style.top = rect.top + 'px';
      tooltipEl.style.left = (rect.right + 8) + 'px';
      tooltipEl.style.opacity = '1';
    }, 750);
  }

  function hideTooltip() {
    clearTimeout(tooltipTimer);
    tooltipEl.style.opacity = '0';
  }

  function showSeriesDetail(show) {
    var overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:9999;display:flex;align-items:center;justify-content:center;backdrop-filter:blur(6px);';
    overlay.onclick = function(e) { if (e.target === overlay) overlay.remove(); };
    document.addEventListener('keydown', function onKey(e) { if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', onKey); } });

    var modal = document.createElement('div');
    modal.style.cssText = 'width:90%;max-width:1080px;max-height:92vh;background:#1a1d23;border-radius:16px;overflow:hidden;display:flex;flex-direction:column;box-shadow:0 24px 80px rgba(0,0,0,0.6);';

    var backdrop = document.createElement('div');
    backdrop.style.cssText = 'width:100%;height:280px;background:linear-gradient(135deg,#1a1a2e,#0f3460);position:relative;overflow:hidden;flex-shrink:0;';

    var closeBtn = document.createElement('button');
    closeBtn.textContent = '\u2715';
    closeBtn.style.cssText = 'position:absolute;top:16px;right:16px;background:rgba(0,0,0,0.6);border:none;color:#fff;font-size:18px;width:40px;height:40px;border-radius:50%;cursor:pointer;z-index:3;';
    closeBtn.onclick = function() { overlay.remove(); };
    backdrop.appendChild(closeBtn);

    backdrop.appendChild(Object.assign(document.createElement('div'), {
      style: 'position:absolute;bottom:0;left:0;right:0;height:150px;background:linear-gradient(transparent,#1a1d23);'
    }));

    var titleBlock = document.createElement('div');
    titleBlock.style.cssText = 'position:absolute;bottom:24px;left:32px;z-index:1;';
    titleBlock.innerHTML = '<div style="font-size:32px;font-weight:800;color:#fff;text-shadow:0 2px 12px rgba(0,0,0,0.7)">' + esc(show.name) + '</div>';
    var seasonCount = Object.keys(show.seasons).length;
    titleBlock.innerHTML += '<div style="color:rgba(255,255,255,0.7);font-size:14px;margin-top:4px">' + seasonCount + ' season' + (seasonCount > 1 ? 's' : '') + ' \u2022 ' + show.episodes.length + ' episodes</div>';
    backdrop.appendChild(titleBlock);
    modal.appendChild(backdrop);

    var body = document.createElement('div');
    body.style.cssText = 'padding:24px 32px;overflow-y:auto;flex:1;';

    var tmdbMeta = document.createElement('div');
    tmdbMeta.style.cssText = 'margin-bottom:20px;min-height:24px;';
    body.appendChild(tmdbMeta);

    var seasonNums = Object.keys(show.seasons).map(Number).sort(function(a, b) { return a - b; });
    if (seasonNums.length === 0 && show.episodes.length > 0) {
      show.seasons[0] = show.episodes;
      seasonNums = [0];
    }

    var tabBar = document.createElement('div');
    tabBar.style.cssText = 'display:flex;gap:8px;margin-bottom:16px;flex-wrap:wrap;';

    var epList = document.createElement('div');

    function renderSeason(num) {
      epList.innerHTML = '';
      tabBar.querySelectorAll('button').forEach(function(btn) {
        btn.style.background = btn.dataset.season == num ? '#3b82f6' : 'rgba(255,255,255,0.1)';
      });
      var eps = show.seasons[num] || [];
      eps.sort(function(a, b) { return a.episode - b.episode; });
      eps.forEach(function(ep) {
        var row = document.createElement('div');
        row.style.cssText = 'display:flex;align-items:flex-start;gap:16px;padding:12px 16px;border-radius:8px;cursor:pointer;transition:background 0.15s;';
        row.onmouseenter = function() { row.style.background = 'rgba(255,255,255,0.05)'; };
        row.onmouseleave = function() { row.style.background = ''; };

        if (ep.episode_still) {
          row.appendChild(h('img', { src: ep.episode_still, style: 'width:120px;height:68px;object-fit:cover;border-radius:6px;flex-shrink:0;' }));
        }

        var epNum = document.createElement('span');
        epNum.style.cssText = 'font-size:24px;font-weight:700;color:var(--text-muted);min-width:40px;text-align:center;flex-shrink:0;padding-top:2px;';
        epNum.textContent = ep.episode || '?';
        row.appendChild(epNum);

        var epInfo = document.createElement('div');
        epInfo.style.cssText = 'flex:1;min-width:0;';

        epInfo.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:14px;font-weight:600;color:var(--text-primary);', textContent: ep.episode_name || ('Episode ' + ep.episode) }));

        var meta = [];
        if (ep.resolution) meta.push(ep.resolution);
        if (ep.vcodec) meta.push(ep.vcodec);
        if (ep.audio) meta.push(ep.audio);
        if (ep.duration > 0) { var m = Math.floor(ep.duration / 60); meta.push(m + 'm'); }
        if (meta.length) epInfo.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:12px;color:var(--text-muted);margin-top:2px;', textContent: meta.join(' \u2022 ') }));

        if (ep.episode_overview) {
          epInfo.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:12px;color:#9ca3af;margin-top:6px;line-height:1.5;', textContent: ep.episode_overview }));
        }

        row.appendChild(epInfo);

        var playBtn = document.createElement('button');
        playBtn.style.cssText = 'background:#3b82f6;border:none;color:#fff;width:36px;height:36px;border-radius:50%;cursor:pointer;font-size:16px;flex-shrink:0;margin-top:2px;';
        playBtn.textContent = '\u25B6';
        playBtn.onclick = function(e) {
          e.stopPropagation();
          overlay.remove();
          play({ streamID: ep.id, name: show.name + ' S' + String(ep.season).padStart(2,'0') + 'E' + String(ep.episode).padStart(2,'0') });
        };
        row.appendChild(playBtn);

        row.onclick = function() {
          overlay.remove();
          play({ streamID: ep.id, name: show.name + ' S' + String(ep.season).padStart(2,'0') + 'E' + String(ep.episode).padStart(2,'0') });
        };

        epList.appendChild(row);
      });
    }

    seasonNums.forEach(function(num) {
      var btn = document.createElement('button');
      btn.style.cssText = 'background:rgba(255,255,255,0.1);border:none;color:#fff;padding:6px 16px;border-radius:6px;cursor:pointer;font-size:13px;font-weight:600;';
      btn.textContent = num === 0 ? 'All Episodes' : 'Season ' + num;
      btn.dataset.season = num;
      btn.onclick = function() { renderSeason(num); };
      tabBar.appendChild(btn);
    });

    body.appendChild(tabBar);
    body.appendChild(epList);
    modal.appendChild(body);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    if (seasonNums.length > 0) renderSeason(seasonNums[0]);

    api.get('/api/tmdb/search?query=' + encodeURIComponent(show.name) + '&type=tv').then(function(data) {
      if (!data || !data.results || !data.results.length) return;
      var match = data.results[0];
      if (!match) return;
      if (match.backdrop_path) {
        backdrop.style.backgroundImage = 'url(/api/tmdb/image?size=w1280&path=' + encodeURIComponent(match.backdrop_path) + ')';
        backdrop.style.backgroundSize = 'cover';
        backdrop.style.backgroundPosition = 'center 20%';
      }
      var pills = [];
      if (match.vote_average > 0) {
        var starColor = match.vote_average >= 7 ? '#22c55e' : match.vote_average >= 5 ? '#eab308' : '#ef4444';
        pills.push('<span style="background:' + starColor + '20;color:' + starColor + ';padding:3px 10px;border-radius:6px;font-weight:700;font-size:13px">\u2605 ' + match.vote_average.toFixed(1) + '</span>');
      }
      if (match.first_air_date) pills.push('<span style="color:#9ca3af;font-size:13px">' + match.first_air_date.substring(0, 4) + '</span>');
      if (match.genre_ids) {
        match.genre_ids.slice(0, 3).forEach(function(gid) {
          var name = TMDB_GENRES[gid];
          if (name) pills.push('<span style="background:rgba(59,130,246,0.15);color:#60a5fa;padding:3px 10px;border-radius:6px;font-size:12px">' + esc(name) + '</span>');
        });
      }
      if (pills.length) tmdbMeta.innerHTML = pills.join('');
      if (match.overview) {
        var desc = document.createElement('p');
        desc.style.cssText = 'color:#b0b8c8;font-size:14px;line-height:1.6;margin:0 0 16px 0;';
        desc.textContent = match.overview;
        body.insertBefore(desc, tabBar);
      }
    }).catch(function() {});
  }


  var TMDB_GENRES = {10759:'Action & Adventure',10762:'Kids',10763:'News',10764:'Reality',10765:'Sci-Fi & Fantasy',10766:'Soap',10767:'Talk',10768:'War & Politics',28:'Action',12:'Adventure',16:'Animation',35:'Comedy',80:'Crime',99:'Documentary',18:'Drama',10751:'Family',14:'Fantasy',36:'History',27:'Horror',10402:'Music',9648:'Mystery',10749:'Romance',878:'Science Fiction',53:'Thriller',10752:'War',37:'Western'};

  function showProgrammeModal(opts) {
    var overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.75);z-index:9999;display:flex;align-items:center;justify-content:center;backdrop-filter:blur(6px);';
    overlay.onclick = function(e) { if (e.target === overlay) overlay.remove(); };
    document.addEventListener('keydown', function onKey(e) { if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', onKey); } });

    var modal = document.createElement('div');
    modal.style.cssText = 'width:90%;max-width:1080px;max-height:92vh;background:#1a1d23;border-radius:16px;overflow:hidden;position:relative;display:flex;flex-direction:column;box-shadow:0 24px 80px rgba(0,0,0,0.6);';

    var backdrop = document.createElement('div');
    backdrop.style.cssText = 'width:100%;height:360px;background:linear-gradient(135deg,#1a1a2e 0%,#16213e 50%,#0f3460 100%);position:relative;overflow:hidden;flex-shrink:0;transition:background-image 0.5s;';

    var closeBtn = document.createElement('button');
    closeBtn.textContent = '\u2715';
    closeBtn.style.cssText = 'position:absolute;top:16px;right:16px;background:rgba(0,0,0,0.6);border:none;color:#fff;font-size:18px;width:40px;height:40px;border-radius:50%;cursor:pointer;z-index:3;transition:background 0.2s;';
    closeBtn.onmouseenter = function() { closeBtn.style.background = 'rgba(255,255,255,0.2)'; };
    closeBtn.onmouseleave = function() { closeBtn.style.background = 'rgba(0,0,0,0.6)'; };
    closeBtn.onclick = function() { overlay.remove(); };
    backdrop.appendChild(closeBtn);

    if (opts.isLive) {
      var liveBadge = document.createElement('div');
      liveBadge.style.cssText = 'position:absolute;top:16px;left:16px;background:#e53e3e;color:#fff;font-size:11px;font-weight:700;padding:4px 10px;border-radius:4px;letter-spacing:1px;z-index:3;';
      liveBadge.textContent = 'LIVE';
      backdrop.appendChild(liveBadge);
    }

    var gradOverlay = document.createElement('div');
    gradOverlay.style.cssText = 'position:absolute;bottom:0;left:0;right:0;height:200px;background:linear-gradient(transparent,#1a1d23);';
    backdrop.appendChild(gradOverlay);

    var titleBlock = document.createElement('div');
    titleBlock.style.cssText = 'position:absolute;bottom:24px;left:32px;right:32px;z-index:1;';

    var titleEl = document.createElement('div');
    titleEl.style.cssText = 'font-size:32px;font-weight:800;color:#fff;text-shadow:0 2px 12px rgba(0,0,0,0.7);letter-spacing:-0.5px;';
    titleEl.textContent = opts.title;
    titleBlock.appendChild(titleEl);

    var metaRow = document.createElement('div');
    metaRow.style.cssText = 'display:flex;align-items:center;justify-content:space-between;margin-top:8px;';

    var metaLine = document.createElement('div');
    metaLine.style.cssText = 'display:flex;align-items:center;gap:10px;font-size:14px;color:rgba(255,255,255,0.75);flex-wrap:wrap;';

    var chanSpan = document.createElement('span');
    chanSpan.style.cssText = 'display:flex;align-items:center;gap:6px;';
    if (opts.channelLogo) {
      chanSpan.innerHTML = '<img src="' + esc(opts.channelLogo) + '" style="height:20px;border-radius:3px"> ';
    }
    chanSpan.appendChild(document.createTextNode(opts.channelName));
    metaLine.appendChild(chanSpan);

    if (opts.time) {
      metaLine.appendChild(Object.assign(document.createElement('span'), { style: 'opacity:0.4', textContent: '\u2022' }));
      metaLine.appendChild(Object.assign(document.createElement('span'), { textContent: opts.time }));
    }
    metaRow.appendChild(metaLine);

    var actionIcons = document.createElement('div');
    actionIcons.style.cssText = 'display:flex;gap:8px;align-items:center;flex-shrink:0;';
    var iconBtnStyle = 'background:rgba(0,0,0,0.5);border:none;color:#fff;width:40px;height:40px;border-radius:50%;cursor:pointer;font-size:18px;display:flex;align-items:center;justify-content:center;transition:background 0.2s;';

    if (opts.isLive || (!opts.isFuture && !opts.isLive)) {
      var playIcon = document.createElement('button');
      playIcon.style.cssText = iconBtnStyle;
      playIcon.title = 'Watch Now';
      playIcon.textContent = '\u25B6';
      playIcon.onmouseenter = function() { playIcon.style.background = '#3b82f6'; };
      playIcon.onmouseleave = function() { playIcon.style.background = 'rgba(0,0,0,0.5)'; };
      playIcon.onclick = function() {
        overlay.remove();
        if (opts.vodStreamID) {
          play({ streamID: opts.vodStreamID, name: opts.title });
        } else {
          play({ channelID: opts.channelID, name: opts.channelName, tvgId: opts.tvgId });
        }
      };
      actionIcons.appendChild(playIcon);
    }

    if ((opts.isLive || opts.isFuture) && opts.stop) {
      var recIcon = document.createElement('button');
      recIcon.style.cssText = iconBtnStyle;
      recIcon.title = opts.isFuture ? 'Schedule Recording' : 'Record';
      recIcon.textContent = '\u23FA';
      recIcon.onmouseenter = function() { recIcon.style.background = '#dc2626'; };
      recIcon.onmouseleave = function() { recIcon.style.background = 'rgba(0,0,0,0.5)'; };
      recIcon.onclick = function() {
        recIcon.style.opacity = '0.5'; recIcon.disabled = true;
        var req = opts.isFuture
          ? api.post('/api/recordings/schedule', { channel_id: opts.channelID, channel_name: opts.channelName, program_title: opts.title, start_at: opts.start, stop_at: opts.stop })
          : api.post('/api/vod/record/' + opts.channelID, { program_title: opts.title, channel_name: opts.channelName, stop_at: opts.stop });
        req.then(function() { recIcon.textContent = '\u2713'; recIcon.style.background = '#16a34a'; })
          .catch(function() { recIcon.textContent = '\u2717'; recIcon.style.opacity = '1'; recIcon.disabled = false; });
      };
      actionIcons.appendChild(recIcon);
    }

    if (opts.channelID) {
      var copyIcon = document.createElement('button');
      copyIcon.style.cssText = iconBtnStyle + 'font-size:15px;';
      copyIcon.title = 'Copy stream URL';
      copyIcon.textContent = '\uD83D\uDCCB';
      copyIcon.onmouseenter = function() { copyIcon.style.background = 'rgba(255,255,255,0.2)'; };
      copyIcon.onmouseleave = function() { copyIcon.style.background = 'rgba(0,0,0,0.5)'; };
      copyIcon.onclick = function() {
        var url = window.location.origin + '/channel/' + opts.channelID;
        navigator.clipboard.writeText(url).then(function() { copyIcon.textContent = '\u2713'; setTimeout(function() { copyIcon.textContent = '\uD83D\uDCCB'; }, 1500); });
      };
      actionIcons.appendChild(copyIcon);
    }

    var refreshIcon = document.createElement('button');
    refreshIcon.style.cssText = iconBtnStyle + 'font-size:16px;';
    refreshIcon.title = 'Refresh metadata';
    refreshIcon.textContent = '\u21BB';
    refreshIcon.onmouseenter = function() { refreshIcon.style.background = 'rgba(255,255,255,0.2)'; };
    refreshIcon.onmouseleave = function() { refreshIcon.style.background = 'rgba(0,0,0,0.5)'; };
    refreshIcon.onclick = function() {
      refreshIcon.style.opacity = '0.5';
      refreshIcon.disabled = true;
      api.del('/api/tmdb/cache?query=' + encodeURIComponent(opts.title)).then(function() {
        overlay.remove();
        showProgrammeModal(opts);
      }).catch(function() {
        refreshIcon.style.opacity = '1';
        refreshIcon.disabled = false;
      });
    };
    actionIcons.appendChild(refreshIcon);

    metaRow.appendChild(actionIcons);
    titleBlock.appendChild(metaRow);
    backdrop.appendChild(titleBlock);
    modal.appendChild(backdrop);

    var body = document.createElement('div');
    body.style.cssText = 'padding:28px 32px;overflow-y:auto;flex:1;';

    var tmdbMeta = document.createElement('div');
    tmdbMeta.style.cssText = 'display:flex;align-items:center;gap:16px;margin-bottom:20px;flex-wrap:wrap;min-height:24px;';

    var localPills = [];
    if (opts.rating > 0) {
      var sc = opts.rating >= 7 ? '#22c55e' : opts.rating >= 5 ? '#eab308' : '#ef4444';
      localPills.push('<span style="background:' + sc + '20;color:' + sc + ';padding:3px 10px;border-radius:6px;font-weight:700;font-size:13px">\u2605 ' + opts.rating.toFixed(1) + '</span>');
    }
    if (opts.year) localPills.push('<span style="color:#9ca3af;font-size:13px">' + esc(opts.year) + '</span>');
    if (opts.certification) localPills.push('<span style="background:rgba(255,255,255,0.1);color:#fff;padding:3px 10px;border-radius:6px;font-size:12px;font-weight:600;border:1px solid rgba(255,255,255,0.2)">' + esc(opts.certification) + '</span>');
    if (opts.genres && opts.genres.length) {
      opts.genres.slice(0, 3).forEach(function(g) {
        localPills.push('<span style="background:rgba(59,130,246,0.15);color:#60a5fa;padding:3px 10px;border-radius:6px;font-size:12px">' + esc(g) + '</span>');
      });
    }
    if (localPills.length) tmdbMeta.innerHTML = localPills.join('');

    body.appendChild(tmdbMeta);

    var descArea = document.createElement('div');
    descArea.style.cssText = 'margin-bottom:24px;';
    if (opts.description) {
      var descEl = document.createElement('p');
      descEl.style.cssText = 'color:#b0b8c8;font-size:15px;line-height:1.7;margin:0;';
      descEl.textContent = opts.description;
      descArea.appendChild(descEl);
    }
    body.appendChild(descArea);

    var castArea = document.createElement('div');
    castArea.style.cssText = 'margin-bottom:24px;display:none;';
    body.appendChild(castArea);

    modal.appendChild(body);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    var tmdbTypeParam = opts.mediaType ? '&type=' + encodeURIComponent(opts.mediaType) : '';
    api.get('/api/tmdb/search?query=' + encodeURIComponent(opts.title) + tmdbTypeParam).then(function(data) {
      if (!data || !data.results || data.results.length === 0) return;
      var match = opts.mediaType ? data.results[0] : data.results.find(function(r) { return r.media_type === 'tv' || r.media_type === 'movie'; });
      if (!match) return;

      if (match.backdrop_path) {
        var img = new Image();
        img.onload = function() {
          backdrop.style.backgroundImage = 'url(/api/tmdb/image?size=w1280&path=' + encodeURIComponent(match.backdrop_path) + ')';
          backdrop.style.backgroundSize = 'cover';
          backdrop.style.backgroundPosition = 'center 20%';
        };
        img.src = '/api/tmdb/image?size=w1280&path=' + encodeURIComponent(match.backdrop_path);
      }

      var pills = [];
      if (match.vote_average > 0) {
        var stars = match.vote_average.toFixed(1);
        var starColor = match.vote_average >= 7 ? '#22c55e' : match.vote_average >= 5 ? '#eab308' : '#ef4444';
        pills.push('<span style="background:' + starColor + '20;color:' + starColor + ';padding:3px 10px;border-radius:6px;font-weight:700;font-size:13px">\u2605 ' + stars + '</span>');
      }
      var year = match.first_air_date ? match.first_air_date.substring(0, 4) : (match.release_date ? match.release_date.substring(0, 4) : '');
      if (year) pills.push('<span style="color:#9ca3af;font-size:13px">' + year + '</span>');
      var mediaLabel = match.media_type === 'tv' ? 'TV Series' : 'Film';
      pills.push('<span style="background:rgba(255,255,255,0.1);color:#9ca3af;padding:3px 10px;border-radius:6px;font-size:12px">' + mediaLabel + '</span>');
      if (match.genre_ids) {
        match.genre_ids.slice(0, 3).forEach(function(gid) {
          var name = TMDB_GENRES[gid];
          if (name) pills.push('<span style="background:rgba(59,130,246,0.15);color:#60a5fa;padding:3px 10px;border-radius:6px;font-size:12px">' + esc(name) + '</span>');
        });
      }
      if (match.original_language && match.original_language !== 'en') {
        pills.push('<span style="color:#9ca3af;font-size:12px">' + match.original_language.toUpperCase() + '</span>');
      }
      tmdbMeta.innerHTML = pills.join('');

      if (match.overview && !opts.description) {
        var descEl = document.createElement('p');
        descEl.style.cssText = 'color:#b0b8c8;font-size:15px;line-height:1.7;margin:0;';
        descEl.textContent = match.overview;
        descArea.appendChild(descEl);
      }

      var tmdbId = match.id;
      var tmdbType = match.media_type;
      api.get('/api/tmdb/details?type=' + tmdbType + '&id=' + tmdbId).then(function(detail) {
        if (!detail) return;

        if (detail.tagline) {
          var tagEl = document.createElement('div');
          tagEl.style.cssText = 'color:#6b7280;font-style:italic;font-size:14px;margin-bottom:12px;';
          tagEl.textContent = '"' + detail.tagline + '"';
          descArea.insertBefore(tagEl, descArea.firstChild);
        }

        if (detail.number_of_seasons) {
          var seasonsEl = document.createElement('span');
          seasonsEl.style.cssText = 'color:#9ca3af;font-size:12px';
          seasonsEl.textContent = detail.number_of_seasons + ' season' + (detail.number_of_seasons > 1 ? 's' : '');
          tmdbMeta.appendChild(seasonsEl);
        }
        if (detail.runtime) {
          var rtEl = document.createElement('span');
          rtEl.style.cssText = 'color:#9ca3af;font-size:12px';
          var hrs = Math.floor(detail.runtime / 60);
          var mins = detail.runtime % 60;
          rtEl.textContent = (hrs > 0 ? hrs + 'h ' : '') + mins + 'm';
          tmdbMeta.appendChild(rtEl);
        }

        if (detail.credits && detail.credits.cast && detail.credits.cast.length > 0) {
          castArea.style.display = 'block';
          var castTitle = document.createElement('div');
          castTitle.style.cssText = 'color:#9ca3af;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:1px;margin-bottom:12px;';
          castTitle.textContent = 'Cast';
          castArea.appendChild(castTitle);
          var castGrid = document.createElement('div');
          castGrid.style.cssText = 'display:flex;gap:16px;overflow-x:auto;padding-bottom:8px;';
          detail.credits.cast.slice(0, 8).forEach(function(person) {
            var card = document.createElement('div');
            card.style.cssText = 'flex-shrink:0;text-align:center;width:72px;';
            var imgUrl = person.profile_path ? 'https://image.tmdb.org/t/p/w185' + person.profile_path : '';
            if (imgUrl) {
              card.innerHTML = '<img src="' + imgUrl + '" style="width:64px;height:64px;border-radius:50%;object-fit:cover;margin-bottom:6px">';
            } else {
              card.innerHTML = '<div style="width:64px;height:64px;border-radius:50%;background:#374151;margin:0 auto 6px;display:flex;align-items:center;justify-content:center;color:#6b7280;font-size:24px">\u263A</div>';
            }
            card.innerHTML += '<div style="font-size:11px;color:#d1d5db;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(person.name) + '</div>' +
              '<div style="font-size:10px;color:#6b7280;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(person.character || '') + '</div>';
            castGrid.appendChild(card);
          });
          castArea.appendChild(castGrid);
        }
      }).catch(function() {});
    }).catch(function() {});
  }

  function buildEpgGuidePage() {
    const HOUR_WIDTH = 240; // px per hour
    const PX_PER_MIN = HOUR_WIDTH / 60;
    const CHANNEL_COL = 80;
    let currentHours = 6;
    let windowOffset = 0; // in hours from initial start

    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading guide...'));

      let channels, groups, guideData, scheduledRecs;
      try {
        [channels, groups, guideData, scheduledRecs] = await Promise.all([
          channelsCache.getAll(),
          channelGroupsCache.getAll(),
          api.get('/api/epg/guide?hours=' + currentHours),
          api.get('/api/recordings/schedule').catch(function() { return []; }),
        ]);
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        return;
      }
      if (!scheduledRecs) scheduledRecs = [];
      var scheduledSet = {};
      scheduledRecs.forEach(function(sr) {
        if (sr.status === 'pending' || sr.status === 'recording') {
          scheduledSet[sr.channel_id + '|' + sr.start_at] = sr.id;
        }
      });

      channels = channels.filter(function(c) { return c.is_enabled; });
      channels.sort(function(a, b) { return a.name.localeCompare(b.name); });

      var groupMap = {};
      groups.forEach(function(g) { groupMap[g.id] = g; });

      var grouped = {};
      var ungrouped = [];
      channels.forEach(function(c) {
        if (c.channel_group_id && groupMap[c.channel_group_id]) {
          var gid = c.channel_group_id;
          if (!grouped[gid]) grouped[gid] = [];
          grouped[gid].push(c);
        } else {
          ungrouped.push(c);
        }
      });

      var sortedGroupIds = Object.keys(grouped).sort(function(a, b) {
        return (groupMap[a].sort_order || 0) - (groupMap[b].sort_order || 0);
      });

      var windowStart = new Date(guideData.start).getTime();
      var windowStop = new Date(guideData.stop).getTime();
      var windowMinutes = (windowStop - windowStart) / 60000;
      var totalWidth = windowMinutes * PX_PER_MIN;
      var programs = guideData.programs || {};
      var now = Date.now();

      function formatTime(d) {
        var dt = new Date(d);
        var hh = dt.getHours();
        var mm = dt.getMinutes();
        return (hh < 10 ? '0' : '') + hh + ':' + (mm < 10 ? '0' : '') + mm;
      }

      function formatDate(ts) {
        var d = new Date(ts);
        var months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
        return months[d.getMonth()] + ' ' + d.getDate() + ', ' + formatTime(ts);
      }

      var hourMarksHtml = '';
      for (var m = 0; m < windowMinutes; m += 60) {
        hourMarksHtml += '<div class="epg-hour-mark" style="width:' + HOUR_WIDTH + 'px">' + formatTime(windowStart + m * 60000) + '</div>';
      }

      var channelCounter = 0;
      function buildChannelRow(ch) {
        channelCounter++;
        var chPrograms = ch.tvg_id ? (programs[ch.tvg_id] || []) : [];
        var programsHtml = '';

        for (var i = 0; i < chPrograms.length; i++) {
          var p = chPrograms[i];
          var pStart = new Date(p.start).getTime();
          var pStop = new Date(p.stop).getTime();
          var startMin = Math.max(0, (pStart - windowStart) / 60000);
          var endMin = Math.min(windowMinutes, (pStop - windowStart) / 60000);
          var leftPx = startMin * PX_PER_MIN;
          var widthPx = (endMin - startMin) * PX_PER_MIN - 2; // 2px gap
          if (widthPx < 2) continue;

          var isLive = now >= pStart && now < pStop;
          var isPast = now >= pStop;
          var cls = 'epg-program' + (isLive ? ' live' : '') + (isPast ? ' past' : '');
          var timeStr = formatTime(pStart) + ' - ' + formatTime(pStop);
          var tooltip = esc(p.title) + ' (' + timeStr + ')';
          if (p.description) tooltip += '&#10;' + esc(p.description.substring(0, 200));

          var schedKey = ch.id + '|' + p.start;
          var isScheduled = !!scheduledSet[schedKey];
          var recBtnCls = 'epg-record-btn' + (isScheduled ? ' scheduled' : '');
          var recBtnHtml = isPast ? '' : '<button class="' + recBtnCls + '" data-ptitle="' + esc(p.title) + '" data-pstart="' + esc(p.start) + '" data-pstop="' + esc(p.stop) + '"' + (isScheduled ? ' data-scheduled="' + esc(scheduledSet[schedKey]) + '"' : '') + '>\u23FA</button>';
          programsHtml += '<div class="' + cls + '" style="left:' + leftPx + 'px;width:' + widthPx + 'px" title="' + tooltip + '"' +
            ' data-desc="' + esc(p.description || '') + '"' +
            ' data-pstart="' + esc(p.start || '') + '"' +
            ' data-pstop="' + esc(p.stop || '') + '">' +
            recBtnHtml +
            '<div class="epg-program-title">' + esc(p.title) + '</div>' +
            '<div class="epg-program-time">' + timeStr + '</div>' +
            '</div>';
        }

        if (chPrograms.length === 0 && ch.tvg_id) {
          programsHtml = '<div class="epg-program" style="left:0;width:' + (totalWidth - 2) + 'px;opacity:0.3"><div class="epg-program-title">No EPG data</div></div>';
        } else if (!ch.tvg_id) {
          programsHtml = '<div class="epg-program" style="left:0;width:' + (totalWidth - 2) + 'px;opacity:0.3"><div class="epg-program-title">No EPG assigned</div></div>';
        }

        var logoHtml = ch.logo
          ? '<img class="epg-channel-logo" src="' + esc(ch.logo) + '" loading="lazy" alt="">'
          : '<div class="epg-channel-logo"></div>';

        return '<div class="epg-row">' +
          '<div class="epg-channel" data-chid="' + esc(String(ch.id)) + '" data-tvgid="' + esc(ch.tvg_id || '') + '" data-chname="' + esc(ch.name) + '" title="' + esc(ch.name) + '">' +
            '<span class="epg-channel-num">' + channelCounter + '</span>' +
            logoHtml +
          '</div>' +
          '<div class="epg-programs" style="width:' + totalWidth + 'px">' + programsHtml + '</div>' +
        '</div>';
      }

      channelCounter = 0;
      var rowsHtml = '';
      for (var gi = 0; gi < sortedGroupIds.length; gi++) {
        var gid = sortedGroupIds[gi];
        var grp = groupMap[gid];
        rowsHtml += '<div class="epg-group-row">' + esc(grp.name) + '</div>';
        var grpChannels = grouped[gid];
        for (var ci = 0; ci < grpChannels.length; ci++) {
          rowsHtml += buildChannelRow(grpChannels[ci]);
        }
      }
      if (ungrouped.length > 0) {
        if (sortedGroupIds.length > 0) {
          rowsHtml += '<div class="epg-group-row">Ungrouped</div>';
        }
        for (var ui = 0; ui < ungrouped.length; ui++) {
          rowsHtml += buildChannelRow(ungrouped[ui]);
        }
      }

      var nowMin = (now - windowStart) / 60000;
      var nowPx = nowMin * PX_PER_MIN;
      var nowLineHtml = (nowMin >= 0 && nowMin <= windowMinutes)
        ? '<div class="epg-now-line" style="left:' + (CHANNEL_COL + nowPx) + 'px"></div>'
        : '';

      var timeLabelEl = h('span', { className: 'epg-time-label' }, formatDate(windowStart) + ' \u2014 ' + formatDate(windowStop));

      var deltaPresets = [-24, -6, -3, -1, 1, 3, 6, 24];
      var deltaLabels = { '-24': '-1d', '-6': '-6h', '-3': '-3h', '-1': '-1h', '1': '+1h', '3': '+3h', '6': '+6h', '24': '+1d' };
      var nowBtn = h('button', { className: 'btn btn-sm btn-primary', onClick: function() { navigate(0); } }, 'Now');
      var dayLabel = h('span', { className: 'epg-day-label' }, '');
      var navEl = h('div', { className: 'epg-nav' });

      function formatOffset(hrs) {
        if (hrs === 0) return 'Now';
        var sign = hrs > 0 ? '+' : '-';
        var abs = Math.abs(hrs);
        if (abs >= 24 && abs % 24 === 0) return 'Now ' + sign + (abs / 24) + 'd';
        return 'Now ' + sign + abs + 'h';
      }

      deltaPresets.forEach(function(d) {
        if (d > 0 && navEl.children.length === 4) navEl.appendChild(nowBtn);
        navEl.appendChild(h('button', {
          className: 'btn btn-sm btn-secondary',
          onClick: navigate.bind(null, d),
        }, deltaLabels[String(d)]));
      });
      if (navEl.children.length === 8) navEl.appendChild(nowBtn);

      var guideLoading = false;

      function navigate(delta) {
        if (guideLoading) return;
        if (delta === 0) {
          windowOffset = 0;
        } else {
          windowOffset += delta;
        }
        nowBtn.textContent = formatOffset(windowOffset);
        nowBtn.className = 'btn btn-sm btn-primary';
        loadGuide();
      }

      async function loadGuide() {
        if (guideLoading) return;
        guideLoading = true;
        container.innerHTML = '';
        container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading guide...'));
        try {
          var startParam = '';
          if (windowOffset !== 0) {
            var offsetMs = windowOffset * 3600000;
            var baseStart = new Date(Date.now() + offsetMs);
            baseStart = new Date(baseStart.getTime() - (baseStart.getTime() % (30 * 60000)));
            startParam = '&start=' + baseStart.toISOString();
          }
          guideData = await api.get('/api/epg/guide?hours=' + currentHours + startParam);
          windowStart = new Date(guideData.start).getTime();
          windowStop = new Date(guideData.stop).getTime();
          windowMinutes = (windowStop - windowStart) / 60000;
          totalWidth = windowMinutes * PX_PER_MIN;
          programs = guideData.programs || {};
          now = Date.now();
          guideLoading = false;
          renderFull();
        } catch (err) {
          guideLoading = false;
          container.innerHTML = '';
          container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        }
      }

      function renderFull() {
        timeLabelEl.textContent = formatDate(windowStart) + ' \u2014 ' + formatDate(windowStop);
        var ws = new Date(windowStart);
        var days = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
        dayLabel.textContent = days[ws.getDay()] + ' ' + ws.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });

        hourMarksHtml = '';
        for (var m = 0; m < windowMinutes; m += 60) {
          hourMarksHtml += '<div class="epg-hour-mark" style="width:' + HOUR_WIDTH + 'px">' + formatTime(windowStart + m * 60000) + '</div>';
        }

        channelCounter = 0;
        rowsHtml = '';
        for (var gi = 0; gi < sortedGroupIds.length; gi++) {
          var gid = sortedGroupIds[gi];
          var grp = groupMap[gid];
          rowsHtml += '<div class="epg-group-row">' + esc(grp.name) + '</div>';
          var grpChannels = grouped[gid];
          for (var ci = 0; ci < grpChannels.length; ci++) {
            rowsHtml += buildChannelRow(grpChannels[ci]);
          }
        }
        if (ungrouped.length > 0) {
          if (sortedGroupIds.length > 0) {
            rowsHtml += '<div class="epg-group-row">Ungrouped</div>';
          }
          for (var ui = 0; ui < ungrouped.length; ui++) {
            rowsHtml += buildChannelRow(ungrouped[ui]);
          }
        }

        nowMin = (now - windowStart) / 60000;
        nowPx = nowMin * PX_PER_MIN;
        nowLineHtml = (nowMin >= 0 && nowMin <= windowMinutes)
          ? '<div class="epg-now-line" style="left:' + (CHANNEL_COL + nowPx) + 'px"></div>'
          : '';

        buildDom();
      }

      function buildDom() {
        container.innerHTML = '';

        var toolbar = h('div', { className: 'epg-toolbar' },
          navEl,
          dayLabel,
          timeLabelEl,
          h('span', { style: 'font-size:13px;color:var(--text-muted)' }, channels.length + ' channels'),
        );

        var scrollEl = document.createElement('div');
        scrollEl.className = 'epg-scroll';

        var innerHtml = '<div class="epg-header-row">' +
          '<div class="epg-corner">Channel</div>' +
          '<div class="epg-timeline">' + hourMarksHtml + '</div>' +
          '</div>' +
          '<div style="position:relative">' +
          nowLineHtml +
          rowsHtml +
          '</div>';

        scrollEl.innerHTML = innerHtml;

        scrollEl.addEventListener('click', function(e) {
          var recBtn = e.target.closest('.epg-record-btn');
          if (recBtn) {
            e.stopPropagation();
            if (recBtn.classList.contains('recording') || recBtn.classList.contains('scheduled')) return;
            var row = recBtn.closest('.epg-row');
            if (!row) return;
            var ch = row.querySelector('.epg-channel');
            if (!ch) return;
            var pStart = recBtn.dataset.pstart || '';
            var pStop = recBtn.dataset.pstop || '';
            var pStartTime = pStart ? new Date(pStart).getTime() : 0;
            var isFuture = pStartTime > Date.now();
            if (isFuture) {
              var body = { channel_id: ch.dataset.chid, channel_name: ch.dataset.chname || '', program_title: recBtn.dataset.ptitle || '', start_at: pStart, stop_at: pStop };
              recBtn.classList.add('scheduled');
              recBtn.disabled = true;
              api.post('/api/recordings/schedule', body).then(function() {
                toast.success('Recording scheduled');
              }).catch(function() {
                recBtn.classList.remove('scheduled'); recBtn.disabled = false;
              });
            } else {
              var body = { program_title: recBtn.dataset.ptitle || '', channel_name: ch.dataset.chname || '', stop_at: pStop };
              recBtn.classList.add('recording');
              recBtn.disabled = true;
              api.post('/api/vod/record/' + ch.dataset.chid, body).catch(function() {
                recBtn.classList.remove('recording'); recBtn.disabled = false;
              });
            }
            return;
          }
          var ch = e.target.closest('.epg-channel');
          if (ch) {
            play({ channelID: ch.dataset.chid, name: ch.dataset.chname, tvgId: ch.dataset.tvgid || undefined });
            return;
          }
          var prog = e.target.closest('.epg-program');
          if (prog) {
            var row = prog.closest('.epg-row');
            if (!row) return;
            ch = row.querySelector('.epg-channel');
            if (!ch) return;
            var titleEl = prog.querySelector('.epg-program-title');
            var timeEl = prog.querySelector('.epg-program-time');
            var progTitle = titleEl ? titleEl.textContent : '';
            var progTime = timeEl ? timeEl.textContent : '';
            var progDesc = prog.dataset.desc || '';
            var pStart = prog.dataset.pstart || '';
            var pStop = prog.dataset.pstop || '';
            var pStartTime = pStart ? new Date(pStart).getTime() : 0;
            var isLive = pStartTime <= Date.now() && (!pStop || new Date(pStop).getTime() > Date.now());
            var isFuture = pStartTime > Date.now();
            showProgrammeModal({
              title: progTitle,
              time: progTime,
              description: progDesc,
              channelName: ch.dataset.chname || '',
              channelID: ch.dataset.chid,
              tvgId: ch.dataset.tvgid || undefined,
              channelLogo: ch.querySelector('.epg-channel-logo') ? ch.querySelector('.epg-channel-logo').src : '',
              isLive: isLive,
              isFuture: isFuture,
              start: pStart,
              stop: pStop,
            });
          }
        });

        container.appendChild(toolbar);
        container.appendChild(scrollEl);

        if (nowMin >= 0 && nowMin <= windowMinutes) {
          var scrollTarget = nowPx - scrollEl.clientWidth / 2 + CHANNEL_COL;
          if (scrollTarget > 0) scrollEl.scrollLeft = scrollTarget;
        }
      }

      if (channels.length === 0) {
        container.innerHTML = '';
        container.appendChild(h('div', { className: 'epg-empty' }, 'No channels configured. Add channels first.'));
        return;
      }

      renderFull();
    };
  }

  const streamGroupsCache = Object.create(null); // accountId -> { groups, sortedGroups, groupDisplay, groupSearch }

  function buildStreamGroupsPage(pageId, filterFn) {
    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      if (!_favoriteIds) await loadFavorites();

      let groups, sortedGroups, groupDisplay, groupSearch;

      if (streamGroupsCache[pageId]) {
        var c = streamGroupsCache[pageId];
        groups = c.groups;
        sortedGroups = c.sortedGroups;
        groupDisplay = c.groupDisplay;
        groupSearch = c.groupSearch;
      } else {
        let allStreams;
        try {
          var cached = await streamsCache.getAll();
          allStreams = cached.filter(filterFn);
        } catch (err) {
          container.innerHTML = '';
          container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
          return;
        }

        groups = Object.create(null);
        for (let i = 0; i < allStreams.length; i++) {
          const g = allStreams[i].group || '';
          if (!groups[g]) groups[g] = [];
          groups[g].push(allStreams[i]);
        }

        sortedGroups = Object.keys(groups).sort((a, b) => {
          if (!a) return 1;
          if (!b) return -1;
          return a.localeCompare(b);
        });

        groupDisplay = new Array(sortedGroups.length);
        groupSearch = new Array(sortedGroups.length);
        for (let i = 0; i < sortedGroups.length; i++) {
          const g = sortedGroups[i];
          groupDisplay[i] = (g || '(No Group)').replace(/^(TV|Movie)\|/, '');
          groupSearch[i] = groupDisplay[i].toLowerCase();
        }

        streamGroupsCache[pageId] = { groups: groups, sortedGroups: sortedGroups, groupDisplay: groupDisplay, groupSearch: groupSearch };
      }

      let searchTerm = '';
      let searchTimer = null;
      const rendered = Object.create(null);

      const summaryEl = h('h3', null, 'Loading streams...');
      const groupsContainer = h('div', null);

      const searchInput = h('input', {
        type: 'text',
        placeholder: 'Filter streams...',
        style: 'padding: 6px 10px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-primary); font-size: 13px; width: 220px; outline: none;',
      });
      searchInput.addEventListener('input', () => {
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => {
          searchTerm = searchInput.value.toLowerCase();
          renderGroups();
        }, 300);
      });

      groupsContainer.addEventListener('toggle', (e) => {
        const details = e.target;
        if (!details.open || details.tagName !== 'DETAILS') return;
        const gIdx = details.dataset.gidx;
        if (rendered[gIdx]) return;
        rendered[gIdx] = true;
        let streams = groups[sortedGroups[gIdx]].slice();
        if (searchTerm) {
          streams = streams.filter(function(s) { return matchesSearch(s.name.toLowerCase(), searchTerm); });
        }
        streams.sort(function(a, b) {
          if (a.vod_type === 'series' && b.vod_type === 'series') {
            if ((a.vod_season || 0) !== (b.vod_season || 0)) return (a.vod_season || 0) - (b.vod_season || 0);
            return (a.vod_episode || 0) - (b.vod_episode || 0);
          }
          if ((a.vod_year || 0) !== (b.vod_year || 0)) return (a.vod_year || 0) - (b.vod_year || 0);
          return a.name.localeCompare(b.name);
        });
        const tableEl = document.createElement('table');
        tableEl.className = 'stream-group-table';
        tableEl.innerHTML = '<tbody>' + buildStreamRows(streams).join('') + '</tbody>';
        details.appendChild(tableEl);
      }, true);

      groupsContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-sid]');
        if (!btn) return;
        if (btn.dataset.fav) {
          var sid = btn.dataset.sid;
          var isFav = _favoriteIds && _favoriteIds.has(sid);
          (isFav ? api.del('/api/favorites/' + sid) : api.post('/api/favorites/' + sid)).then(function() {
            if (isFav) { _favoriteIds.delete(sid); } else { _favoriteIds.add(sid); }
            btn.textContent = isFav ? '\u2606' : '\u2B50';
            btn.style.color = isFav ? 'var(--text-muted)' : '#eab308';
            toast.success(isFav ? 'Removed from favorites' : 'Added to favorites');
          }).catch(function() {});
          return;
        }
        if (btn.dataset.qadd) {
          quickAddChannel(btn.dataset.sid, btn.dataset.sname, btn.dataset.tvgid || '', btn.dataset.slogo || '');
          return;
        }
        if (btn.dataset.radioPlay) {
          toggleInlineRadio(btn, btn.dataset.sid, btn.dataset.sname, btn.dataset.tvgid || undefined);
          return;
        }
        if (btn.dataset.radioRec) {
          toggleRadioRecord(btn, btn.dataset.sid);
          return;
        }
        play({ streamID: btn.dataset.sid, name: btn.dataset.sname, tvgId: btn.dataset.tvgid || undefined });
      });

      var activeInlineRadio = null;
      function toggleInlineRadio(btn, streamID, name, tvgId) {
        if (activeInlineRadio && activeInlineRadio.streamID === streamID) {
          if (activeInlineRadio.audio.paused) {
            activeInlineRadio.audio.play();
            btn.textContent = '\u23F9';
            btn.title = 'Stop';
          } else {
            stopInlineRadio();
          }
          return;
        }
        if (activeInlineRadio) stopInlineRadio();
        btn.textContent = '\u23F3';
        btn.title = 'Connecting...';
        var row = btn.closest('tr');
        var nameCell = row ? row.querySelectorAll('td')[1] : null;
        var origName = nameCell ? nameCell.textContent : '';
        createAudioSession('/stream/' + streamID + '/vod')
          .then(function(resp) {
            if (!resp) { btn.textContent = '\u25B6'; return; }
            var audio = createAudioElement('/vod/' + resp.session_id + '/stream');
            audio.onplaying = function() {
              btn.textContent = '\u23F9'; btn.title = 'Stop';
              updateInlineNowPlaying(tvgId, nameCell, origName);
            };
            audio.onerror = function() { btn.textContent = '\u25B6'; btn.title = 'Play'; stopInlineRadio(); };
            audio.play().catch(function() { btn.textContent = '\u25B6'; });
            activeInlineRadio = { streamID: streamID, sessionID: resp.session_id, consumerID: resp.consumer_id, audio: audio, btn: btn, nameCell: nameCell, origName: origName, tvgId: tvgId, nowInterval: null };
            if (tvgId) {
              activeInlineRadio.nowInterval = setInterval(function() { updateInlineNowPlaying(tvgId, nameCell, origName); }, 30000);
            }
          }).catch(function() { btn.textContent = '\u25B6'; });
      }
      function updateInlineNowPlaying(tvgId, nameCell, origName) {
        if (!tvgId || !nameCell) return;
        api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(function(p) {
          if (p && p.title) {
            nameCell.innerHTML = esc(origName) + ' <span style="color:var(--text-muted);font-size:12px">\u2014 ' + esc(p.title) + '</span>';
          }
        }).catch(function() {});
      }
      function stopInlineRadio() {
        if (!activeInlineRadio) return;
        activeInlineRadio.audio.pause();
        activeInlineRadio.audio.removeAttribute('src');
        activeInlineRadio.btn.textContent = '\u25B6';
        activeInlineRadio.btn.title = 'Play';
        if (activeInlineRadio.nameCell && activeInlineRadio.origName) {
          activeInlineRadio.nameCell.textContent = activeInlineRadio.origName;
        }
        if (activeInlineRadio.nowInterval) clearInterval(activeInlineRadio.nowInterval);
        releaseAudioSession(activeInlineRadio.sessionID, activeInlineRadio.consumerID);
        activeInlineRadio = null;
      }

      function toggleRadioRecord(btn, streamID) {
        if (btn.dataset.recording === '1') {
          api.del('/api/vod/record/' + btn.dataset.channelId).then(function() {
            btn.style.color = '';
            btn.dataset.recording = '0';
            btn.title = 'Record';
          }).catch(function() {});
          return;
        }
        btn.style.color = '#ffa726';
        api.post('/api/vod/record/' + streamID, { program_title: btn.dataset.sname || '', channel_name: btn.dataset.sname || '' })
          .then(function() {
            btn.style.color = '#e53935';
            btn.dataset.recording = '1';
            btn.dataset.channelId = streamID;
            btn.title = 'Stop Recording';
          })
          .catch(function() { btn.style.color = ''; });
      }

      function buildStreamRows(streams) {
        const rows = [];
        for (let j = 0; j < streams.length; j++) {
          const s = streams[j];
          const logo = s.logo
            ? '<img class="stream-group-logo" src="' + esc(s.logo) + '" loading="lazy" alt="">'
            : '';
          let tracksCell = '';
          if (s.tracks && s.tracks.length > 0) {
            const parts = s.tracks.filter(function(t) { return t.category === 'video' || t.category === 'audio'; }).map(function(t) {
              var label = t.label || t.type || '';
              if (!t.label && t.language) label += '[' + t.language.replace(/\0/g, '').trim() + ']';
              if (t.audio_type === 3) label += '[AD]';
              else if (t.audio_type === 2) label += '[HI]';
              var color = t.category === 'video' ? 'var(--accent)' : t.audio_type === 3 ? 'var(--text-muted)' : 'inherit';
              return '<span style="background:var(--bg-hover);border-radius:3px;padding:1px 4px;font-size:11px;white-space:nowrap;color:' + color + '">' + esc(label) + '</span>';
            });
            tracksCell = '<td style="color:var(--text-muted);font-size:11px">' + parts.join(' ') + '</td>';
          } else {
            tracksCell = '<td></td>';
          }
          var isRadio = s.group && s.group.toLowerCase() === 'radio';
          var actionHtml = '<div class="actions-cell" style="justify-content:flex-end;gap:4px;">';
          actionHtml += '<button class="btn btn-sm btn-icon" title="Favorite" data-fav="1" data-sid="' + s.id + '" style="font-size:14px;color:' + (_favoriteIds && _favoriteIds.has(s.id) ? '#eab308' : 'var(--text-muted)') + '">' + (_favoriteIds && _favoriteIds.has(s.id) ? '\u2B50' : '\u2606') + '</button>';
          actionHtml += '<button class="btn btn-primary btn-sm btn-icon" title="Add as Channel" style="font-size:16px" data-qadd="1" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '" data-slogo="' + esc(s.logo || '') + '">+</button>';
          if (isRadio) {
            actionHtml += '<button class="btn btn-sm btn-icon" title="Record" data-radio-rec="1" data-sid="' + s.id + '" style="font-size:12px">\u23FA</button>';
            actionHtml += '<button class="btn btn-secondary btn-sm btn-icon" title="Play" data-radio-play="1" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '">\u25B6</button>';
          } else {
            actionHtml += '<button class="btn btn-secondary btn-sm btn-icon" title="Play" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '">\u25B6</button>';
          }
          actionHtml += '</div>';
          rows.push('<tr><td>' + logo + '</td><td>' + esc(s.name) + '</td>' + tracksCell + '<td style="width:120px">' + actionHtml + '</td></tr>');
        }
        return rows;
      }

      function renderGroups() {
        const openSet = new Set();
        groupsContainer.querySelectorAll('details[open]').forEach(el => {
          openSet.add(el.dataset.gidx);
        });

        let totalVisible = 0;
        const html = [];
        const filteredGroups = {};

        for (let i = 0; i < sortedGroups.length; i++) {
          let streams = groups[sortedGroups[i]];
          if (searchTerm) {
            streams = streams.filter(function(s) { return matchesSearch(s.name.toLowerCase(), searchTerm); });
            if (streams.length === 0) continue;
          }
          filteredGroups[i] = streams;
          totalVisible += streams.length;
          const open = searchTerm || openSet.has(String(i)) ? ' open' : '';
          html.push('<details class="stream-group" data-gidx="' + i + '"' + open + '><summary>' + esc(groupDisplay[i]) + '<span class="stream-group-count">' + streams.length + '</span></summary></details>');
        }

        summaryEl.textContent = totalVisible.toLocaleString() + ' streams in ' + html.length + ' group' + (html.length !== 1 ? 's' : '');

        if (html.length === 0) {
          groupsContainer.innerHTML = '<div style="padding:40px 16px;text-align:center;color:var(--text-muted)">' +
            (searchTerm ? 'No streams match "' + esc(searchInput.value) + '"' : 'No streams found') + '</div>';
          return;
        }

        for (const key in rendered) delete rendered[key];

        groupsContainer.innerHTML = html.join('');
      }

      container.innerHTML = '';
      container.appendChild(h('div', { className: 'table-container' },
        h('div', { className: 'stream-groups-header' },
          summaryEl,
          h('div', { className: 'btn-group', style: 'align-items: center;' }, searchInput),
        ),
        groupsContainer,
      ));

      renderGroups();

      var cacheListener = DataCache.onChange(function() {
        if (state.currentPage !== pageId) {
          DataCache.offChange(cacheListener);
          return;
        }
        var cached = streamsCache._data;
        if (!cached) return;
        var allStreams = cached.filter(filterFn);
        groups = Object.create(null);
        for (var i = 0; i < allStreams.length; i++) {
          var g = allStreams[i].group || '';
          if (!groups[g]) groups[g] = [];
          groups[g].push(allStreams[i]);
        }
        sortedGroups = Object.keys(groups).sort(function(a, b) {
          if (!a) return 1;
          if (!b) return -1;
          return a.localeCompare(b);
        });
        groupDisplay = new Array(sortedGroups.length);
        groupSearch = new Array(sortedGroups.length);
        for (var i = 0; i < sortedGroups.length; i++) {
          groupDisplay[i] = (sortedGroups[i] || '(No Group)').replace(/^(TV|Movie)\|/, '');
          groupSearch[i] = groupDisplay[i].toLowerCase();
        }
        streamGroupsCache[pageId] = { groups: groups, sortedGroups: sortedGroups, groupDisplay: groupDisplay, groupSearch: groupSearch };
        renderGroups();
      });
    };
  }

  async function rebuildStreamNav() {
    const [accounts, satipSources] = await Promise.all([
      api.get('/api/m3u/accounts').catch(() => []),
      api.get('/api/satip/sources').catch(() => []),
    ]);
    navItems = navItems.filter(n => !n.id || (!n.id.startsWith('streams-') && !n.id.startsWith('satip-streams-')));
    Object.keys(pages).forEach(k => { if ((k.startsWith('streams-') || k.startsWith('satip-streams-')) && k !== 'stream-profiles') delete pages[k]; });
    const idx = navItems.findIndex(n => n.section === 'Streams');
    if (idx === -1) return;
    var allStreams = streamsCache._data || [];
    var vodAccountIds = new Set();
    accounts.forEach(function(a) {
      var accountStreams = allStreams.filter(function(s) { return s.m3u_account_id === a.id; });
      if (accountStreams.length > 0 && accountStreams.every(function(s) { return s.vod_type; })) {
        vodAccountIds.add(a.id);
      }
    });
    var liveAccounts = accounts.filter(function(a) { return !vodAccountIds.has(a.id); });
    const accountNavItems = liveAccounts.map(a => ({
      id: 'streams-' + a.id,
      label: a.name,
      icon: '\u25b6',
      tip: 'Streams from ' + a.name,
    }));
    liveAccounts.forEach(a => {
      pages['streams-' + a.id] = buildStreamGroupsPage('streams-' + a.id, function(s) { return s.m3u_account_id === a.id; });
    });
    const satipNavItems = satipSources.map(function(s) {
      var pageId = 'satip-streams-' + s.id;
      pages[pageId] = buildStreamGroupsPage(pageId, function(ss) { return ss.satip_source_id === s.id; });
      return { id: pageId, label: s.name, icon: '\ud83d\udce1', tip: 'Streams from ' + s.name };
    });

    pages['streams-movies'] = buildStreamGroupsPage('streams-movies', function(s) { return s.vod_type === 'movie'; });
    pages['streams-tvseries'] = buildStreamGroupsPage('streams-tvseries', function(s) { return s.vod_type === 'series'; });
    var vodNavItems = [
      { id: 'streams-movies', label: 'Movies', icon: '\uD83C\uDFAC', tip: 'Movie streams from all sources (deduplicated)' },
      { id: 'streams-tvseries', label: 'TV Series', icon: '\uD83D\uDCFA', tip: 'TV series streams grouped by show' },
    ];

    navItems.splice(idx + 1, 0, ...accountNavItems, ...satipNavItems, ...vodNavItems);
    if (auth.isLoggedIn()) {
      const oldSidebar = document.querySelector('.sidebar');
      if (oldSidebar) {
        oldSidebar.replaceWith(renderSidebar());
      }
    }
  }

  function renderSidebar() {
    const isAdmin = state.user && state.user.is_admin;
    const visible = navItems.filter((n, i, arr) => {
      if (n.adminOnly && !isAdmin) return false;
      if (n.section) {
        const next = arr.slice(i + 1).find(x => !x.adminOnly || isAdmin);
        if (!next || next.section) return false;
      }
      return true;
    });
    const items = visible.map(item => {
      if (item.section) {
        return h('div', { className: 'nav-section' }, item.section);
      }
      const el = h('div', {
        className: 'nav-item' + (state.currentPage === item.id ? ' active' : ''),
        onClick: () => { mobileNav.close(); navigate(item.id); },
      },
        h('span', { className: 'icon', style: item.iconStyle || '' }, item.icon),
        item.label,
      );
      if (item.tip) {
        el.addEventListener('mouseenter', () => showTooltip(el, item.tip));
        el.addEventListener('mouseleave', hideTooltip);
      }
      return el;
    });

    var caches = [streamsCache, epgCache, logosCache];
    var statusEl = h('div', { className: 'data-status' });

    function updateStatus() {
      var loading = caches.filter(function(c) { return c.state === 'loading'; });
      var ready = caches.filter(function(c) { return c.state === 'ready'; });
      if (loading.length > 0) {
        statusEl.innerHTML = '<div class="data-status-bar"><div class="data-status-bar-fill loading"></div></div>' +
          '<span class="data-status-text">Fetching ' + loading.map(function(c) { return c.label; }).join(', ') + '...</span>';
      } else if (ready.length > 0) {
        statusEl.innerHTML = '<div class="data-status-bar"><div class="data-status-bar-fill ready"></div></div>' +
          '<span class="data-status-text">Up to date</span>';
      } else {
        statusEl.innerHTML = '';
      }
    }
    if (renderSidebar._prevListener) DataCache.offChange(renderSidebar._prevListener);
    renderSidebar._prevListener = updateStatus;
    DataCache.onChange(updateStatus);
    updateStatus();

    return h('nav', { className: 'sidebar' },
      h('div', { className: 'sidebar-header' },
        h('h2', null, 'TVProxy'),
        h('span', { className: 'version' }, 'IPTV Management'),
      ),
      h('div', { className: 'sidebar-nav' }, ...items),
      h('div', { className: 'sidebar-footer' },
        statusEl,
        h('div', { className: 'user-info' },
          h('span', { className: 'user-name' }, state.user ? state.user.username : ''),
          h('button', { className: 'logout-btn', onClick: () => auth.logout() }, 'Logout'),
        ),
      ),
    );
  }

  async function renderDashboard(container) {
    container.innerHTML = '';
    container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

    try {
      const [accounts, satipSources, channels, groups, epgSources, devices, wgStatus] = await Promise.all([
        api.get('/api/m3u/accounts').catch(() => []),
        api.get('/api/satip/sources').catch(() => []),
        channelsCache.getAll().catch(() => []),
        channelGroupsCache.getAll().catch(() => []),
        api.get('/api/epg/sources').catch(() => []),
        api.get('/api/hdhr/devices').catch(() => []),
        api.get('/api/wireguard/multi/status').catch(() => null),
      ]);

      const m3uStreamCount = accounts.reduce((sum, a) => sum + (a.stream_count || 0), 0);
      const satipStreamCount = satipSources.reduce((sum, s) => sum + (s.stream_count || 0), 0);
      var wgProfiles = wgStatus && wgStatus.profiles ? wgStatus.profiles : [];
      var wgConnected = wgProfiles.filter(function(p) { return p.state === 'connected' && p.healthy; }).length;
      var wgTotal = wgProfiles.length;
      var wgLabel = wgTotal > 0 ? wgConnected + '/' + wgTotal : 'Off';

      container.innerHTML = '';

      const cards = [
        { label: 'M3U Accounts', value: accounts.length, icon: '\u2630', page: 'm3u-accounts' },
        { label: 'SAT>IP Sources', value: satipSources.length, icon: '\ud83d\udce1', page: 'satip-sources' },
        { label: 'M3U Streams', value: m3uStreamCount, icon: '\u25b6', page: accounts.length ? 'streams-' + accounts[0].id : 'dashboard' },
        { label: 'SAT>IP Streams', value: satipStreamCount, icon: '\ud83d\udce1', page: satipSources.length ? 'satip-sources' : 'dashboard' },
        { label: 'Channels', value: channels.length, icon: '\ud83d\udcfa', page: 'channels' },
        { label: 'Channel Groups', value: groups.length, icon: '\ud83d\udcc2', page: 'channels' },
        { label: 'EPG Sources', value: epgSources.length, icon: '\ud83d\udcc5', page: 'epg-sources' },
        { label: 'HDHR Devices', value: devices.length, icon: '\ud83d\udce1', page: 'hdhr-devices' },
        { label: 'WireGuard', value: wgLabel, icon: '\ud83d\udd12', page: 'wireguard' },
      ];

      const grid = h('div', { className: 'dashboard-grid' },
        ...cards.map(c =>
          h('div', { className: 'stat-card', style: 'cursor: pointer', onClick: () => navigate(c.page) },
            h('div', { className: 'stat-icon' }, c.icon),
            h('div', { className: 'stat-label' }, c.label),
            h('div', { className: 'stat-value' }, String(c.value)),
          )
        ),
      );

      const enabledDevices = devices.filter(d => d.is_enabled && d.port > 0);
      const hostname = window.location.hostname;
      let deviceUrlsSection;
      if (enabledDevices.length > 0) {
        const thead = h('tr', null,
          h('th', null, 'Device'),
          h('th', null, 'Port'),
          h('th', null, 'M3U URL'),
          h('th', null, 'EPG URL'),
          h('th', null, 'Discover URL'),
        );
        const rows = enabledDevices.map(d => {
          const base = window.location.protocol + '//' + hostname + ':' + d.port;
          return h('tr', null,
            h('td', null, d.name),
            h('td', null, String(d.port)),
            h('td', null, h('a', { href: base + '/output/m3u', target: '_blank' }, base + '/output/m3u')),
            h('td', null, h('a', { href: base + '/output/epg', target: '_blank' }, base + '/output/epg')),
            h('td', null, h('a', { href: base + '/discover.json', target: '_blank' }, base + '/discover.json')),
          );
        });
        deviceUrlsSection = h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'HDHR Device URLs')),
          h('div', { style: 'overflow-x: auto' },
            h('table', null,
              h('thead', null, thead),
              h('tbody', null, ...rows),
            ),
          ),
        );
      } else {
        deviceUrlsSection = h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'HDHR Device URLs')),
          h('div', { style: 'padding: 16px; color: var(--text-secondary)' },
            'No HDHR devices configured. Add one in ',
            h('a', { href: '#', onClick: (e) => { e.preventDefault(); navigate('hdhr-devices'); } }, 'HDHR Devices'),
            '.',
          ),
        );
      }

      const cachesInfo = [
        { cache: streamsCache, icon: '\u25b6' },
        { cache: epgCache, icon: '\ud83d\udcc5' },
        { cache: logosCache, icon: '\ud83d\uddbc' },
      ];
      const cacheRows = cachesInfo.map(function(ci) {
        var c = ci.cache;
        var stateText = c.state === 'loading' ? 'Fetching...' : c.state === 'ready' ? c.count.toLocaleString() + ' items' : 'Not loaded';
        var barClass = 'data-status-bar-fill' + (c.state === 'loading' ? ' loading' : c.state === 'ready' ? ' ready' : '');
        return h('div', { className: 'cache-status-row' },
          h('span', { className: 'cache-status-label' }, ci.icon + ' ' + c.label),
          h('div', { className: 'data-status-bar cache-bar' }, h('div', { className: barClass })),
          h('span', { className: 'cache-status-value' }, stateText),
        );
      });
      const cacheSection = h('div', { className: 'table-container', style: 'margin-top: 24px' },
        h('div', { className: 'table-header' }, h('h3', null, 'Data Cache')),
        h('div', { style: 'padding: 12px 16px; display: flex; flex-direction: column; gap: 10px' }, ...cacheRows),
      );

      function updateDashCache() {
        cachesInfo.forEach(function(ci, i) {
          var c = ci.cache;
          var row = cacheRows[i];
          if (!row) return;
          var valEl = row.querySelector('.cache-status-value');
          var barEl = row.querySelector('.data-status-bar-fill');
          if (valEl) valEl.textContent = c.state === 'loading' ? 'Fetching...' : c.state === 'ready' ? c.count.toLocaleString() + ' items' : 'Not loaded';
          if (barEl) barEl.className = 'data-status-bar-fill' + (c.state === 'loading' ? ' loading' : c.state === 'ready' ? ' ready' : '');
        });
      }
      if (renderDashboard._prevListener) DataCache.offChange(renderDashboard._prevListener);
      renderDashboard._prevListener = updateDashCache;
      DataCache.onChange(updateDashCache);

      container.appendChild(grid);
      container.appendChild(cacheSection);
      container.appendChild(deviceUrlsSection);
    } catch (err) {
      container.innerHTML = '';
      container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load dashboard: ' + err.message));
    }
  }

  function buildCrudPage(config) {
    const perPage = config.perPage || 50;

    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      let allItems;
      let searchIndex; // parallel array of pre-lowercased search strings
      let groupsData = null;
      const openGroups = new Set();
      let groupsInitialized = false;
      const groupsContainerEl = config.groupBy ? h('table') : null;
      try {
        allItems = config.cache ? await config.cache.getAll() : await api.get(config.apiPath);
        if (config.groupBy) groupsData = await config.groupBy.loadGroups();
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        return;
      }

      const searchKeys = config.searchKeys || config.columns.map(c => c.key);

      function buildSearchIndex() {
        searchIndex = new Array(allItems.length);
        for (let i = 0; i < allItems.length; i++) {
          const parts = [];
          for (let k = 0; k < searchKeys.length; k++) {
            const val = allItems[i][searchKeys[k]];
            if (val != null) parts.push(String(val));
          }
          searchIndex[i] = parts.join(' ').toLowerCase();
        }
      }
      buildSearchIndex();

      let searchTerm = '';
      let currentPage = 1;
      let searchTimer = null;
      let filteredCache = null;

      function getFiltered() {
        if (!searchTerm) return allItems;
        if (filteredCache) return filteredCache;
        const q = searchTerm.toLowerCase();
        const result = [];
        for (let i = 0; i < allItems.length; i++) {
          if (matchesSearch(searchIndex[i], q)) {
            result.push(allItems[i]);
          }
        }
        filteredCache = result;
        return result;
      }

      const countEl = h('h3', null, '');
      const tbodyEl = h('tbody', null);
      const paginationEl = h('div', {
        style: 'display: flex; align-items: center; justify-content: center; gap: 8px; padding: 16px; color: var(--text-secondary); font-size: 14px;',
      });

      const searchInput = h('input', {
        type: 'text',
        placeholder: 'Search...',
        style: 'padding: 6px 10px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-primary); font-size: 13px; width: 220px; outline: none;',
      });
      searchInput.addEventListener('input', () => {
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => {
          searchTerm = searchInput.value;
          filteredCache = null;
          currentPage = 1;
          updateTable();
        }, 300);
      });

      function buildItemRow(item) {
        const tr = document.createElement('tr');
        for (let c = 0; c < config.columns.length; c++) {
          const col = config.columns[c];
          const val = col.render ? col.render(item) : item[col.key];
          const td = document.createElement('td');
          if (col.tdStyle) td.style.cssText = col.tdStyle;
          if (val != null && typeof val === 'object' && val.nodeType) {
            td.appendChild(val);
          } else {
            td.textContent = val != null ? String(val) : '-';
          }
          tr.appendChild(td);
        }
        const actionsTd = document.createElement('td');
        actionsTd.className = 'actions-cell';
        if (config.update !== false && (typeof config.update !== 'function' || config.update(item))) {
          const editBtn = document.createElement('button');
          editBtn.className = 'btn btn-secondary btn-sm btn-icon';
          editBtn.textContent = '\u270E';
          editBtn.title = 'Edit';
          editBtn.onclick = () => openForm(item);
          actionsTd.appendChild(editBtn);
        }
        if (config.rowActions) {
          const actions = config.rowActions(item, reloadData, openForm);
          for (let a = 0; a < actions.length; a++) {
            const btn = document.createElement('button');
            if (actions[a].icon) {
              btn.className = 'btn btn-secondary btn-sm btn-icon';
              btn.textContent = actions[a].icon;
              btn.title = actions[a].label;
            } else {
              btn.className = 'btn btn-secondary btn-sm';
              btn.textContent = actions[a].label;
            }
            btn.onclick = actions[a].handler;
            actionsTd.appendChild(btn);
          }
        }
        if (config.delete !== false && (typeof config.delete !== 'function' || config.delete(item))) {
          const delBtn = document.createElement('button');
          delBtn.className = 'btn btn-danger btn-sm btn-icon btn-icon-circle';
          delBtn.textContent = '\u2715';
          delBtn.title = 'Delete';
          delBtn.onclick = () => deleteItem(item);
          actionsTd.appendChild(delBtn);
        }
        tr.appendChild(actionsTd);
        return tr;
      }

      function buildGroupedThead() {
        const thead = document.createElement('thead');
        const headRow = document.createElement('tr');
        for (let c = 0; c < config.columns.length; c++) {
          const col = config.columns[c];
          const th = h('th', null, col.label);
          if (col.thStyle) th.style.cssText = col.thStyle;
          headRow.appendChild(th);
        }
        headRow.appendChild(h('th', { style: 'width: 120px' }, 'Actions'));
        thead.appendChild(headRow);
        return thead;
      }

      function renderGrouped(filtered) {
        const gb = config.groupBy;
        const groupMap = {};
        (groupsData || []).forEach(g => { groupMap[g.id] = g; });

        const grouped = {};
        const ungrouped = [];
        const seen = new Set();
        for (let i = 0; i < filtered.length; i++) {
          const item = filtered[i];
          seen.add(item);
          const gid = item[gb.key];
          if (gid && groupMap[gid]) {
            if (!grouped[gid]) grouped[gid] = [];
            grouped[gid].push(item);
          } else {
            ungrouped.push(item);
          }
        }

        if (searchTerm) {
          const q = searchTerm.toLowerCase();
          (groupsData || []).forEach(g => {
            if (matchesSearch((g[gb.nameKey] || '').toLowerCase(), q)) {
              for (let i = 0; i < allItems.length; i++) {
                if (allItems[i][gb.key] === g.id && !seen.has(allItems[i])) {
                  if (!grouped[g.id]) grouped[g.id] = [];
                  grouped[g.id].push(allItems[i]);
                  seen.add(allItems[i]);
                }
              }
            }
          });
        }

        let totalVisible = 0;
        Object.values(grouped).forEach(items => { totalVisible += items.length; });
        totalVisible += ungrouped.length;

        countEl.textContent = searchTerm
          ? config.title + ' (' + totalVisible + ' of ' + allItems.length + ')'
          : config.title + ' (' + allItems.length + ')';

        const colSpan = config.columns.length + 1;

        const sortedGroups = (groupsData || []).slice()
          .sort((a, b) => (a[gb.sortKey] || 0) - (b[gb.sortKey] || 0));

        if (!groupsInitialized) {
          sortedGroups.forEach(g => openGroups.add(g.id));
          if (ungrouped.length > 0) openGroups.add('__ungrouped__');
          groupsInitialized = true;
        }

        groupsContainerEl.querySelectorAll('tbody').forEach(el => el.remove());
        let hasContent = false;

        function buildGroupTbody(gid, label, items) {
          const tbody = document.createElement('tbody');
          tbody.dataset.gid = gid;

          const headerTr = document.createElement('tr');
          headerTr.className = 'group-header-row';
          const headerTd = document.createElement('td');
          headerTd.colSpan = colSpan;

          const arrow = h('span', { className: 'group-header-arrow' + (openGroups.has(gid) ? ' open' : '') }, '\u25B6');
          headerTd.appendChild(arrow);
          headerTd.appendChild(document.createTextNode(label));
          headerTd.appendChild(h('span', { className: 'stream-group-count', style: 'margin-left:8px' }, String(items.length)));

          if (gid !== '__ungrouped__') {
            const group = (groupsData || []).find(g => g.id === gid);
            if (group) {
              const grpActions = document.createElement('span');
              grpActions.style.cssText = 'float:right;display:none;gap:4px;align-items:center;';
              grpActions.appendChild(h('button', { className: 'btn btn-secondary btn-sm btn-icon', title: 'Edit group', onClick: (e) => {
                e.preventDefault(); e.stopPropagation(); editGroupInline(group);
              }}, '\u270E'));
              grpActions.appendChild(h('button', { className: 'btn btn-danger btn-sm btn-icon btn-icon-circle', title: 'Delete group', onClick: (e) => {
                e.preventDefault(); e.stopPropagation(); deleteGroupFromHeader(group);
              }}, '\u2715'));
              headerTd.appendChild(grpActions);
              headerTr.addEventListener('mouseenter', () => { grpActions.style.display = 'inline-flex'; });
              headerTr.addEventListener('mouseleave', () => { grpActions.style.display = 'none'; });
            }
          }

          headerTr.appendChild(headerTd);

          const dataRows = [];
          headerTr.addEventListener('click', (e) => {
            if (e.target.closest('button')) return;
            var isOpen = openGroups.has(gid);
            if (isOpen) {
              openGroups.delete(gid);
              arrow.className = 'group-header-arrow';
            } else {
              openGroups.add(gid);
              arrow.className = 'group-header-arrow open';
            }
            for (var r = 0; r < dataRows.length; r++) {
              dataRows[r].style.display = openGroups.has(gid) ? '' : 'none';
            }
          });

          tbody.appendChild(headerTr);

          for (var i = 0; i < items.length; i++) {
            var row = buildItemRow(items[i]);
            if (!openGroups.has(gid)) row.style.display = 'none';
            dataRows.push(row);
            tbody.appendChild(row);
          }

          return tbody;
        }

        for (let gi = 0; gi < sortedGroups.length; gi++) {
          const group = sortedGroups[gi];
          const items = grouped[group.id];
          if (!items || items.length === 0) continue;
          hasContent = true;
          groupsContainerEl.appendChild(buildGroupTbody(group.id, group[gb.nameKey] || 'Unknown', items));
        }

        if (ungrouped.length > 0) {
          hasContent = true;
          groupsContainerEl.appendChild(buildGroupTbody('__ungrouped__', gb.ungroupedLabel || 'Ungrouped', ungrouped));
        }

        if (!hasContent) {
          const tbody = document.createElement('tbody');
          tbody.appendChild(h('tr', { className: 'empty-row' },
            h('td', { colspan: String(colSpan), style: 'padding:40px 16px;text-align:center;color:var(--text-muted)' },
              searchTerm ? 'No matching items' : 'No items found')
          ));
          groupsContainerEl.appendChild(tbody);
        }

        paginationEl.innerHTML = '';
      }

      function editGroupInline(group) {
        const gb = config.groupBy;
        const formEl = h('div');
        const inputs = {};
        (gb.fields || []).forEach(f => {
          if (f.type === 'checkbox') {
            const cb = h('input', { type: 'checkbox', id: 'gf-' + f.key });
            cb.checked = !!group[f.key];
            inputs[f.key] = cb;
            formEl.appendChild(h('div', { className: 'form-check', style: 'display:flex;align-items:center;gap:6px;margin:8px 0' }, cb, h('label', { for: 'gf-' + f.key, style: 'cursor:pointer;margin:0' }, f.label)));
          } else if (f.type === 'select' && f.options) {
            const sel = h('select', { id: 'gf-' + f.key }, ...f.options.map(o => {
              const opt = h('option', { value: String(o.value) }, o.label);
              if (String(group[f.key] || f.default) === String(o.value)) opt.selected = true;
              return opt;
            }));
            inputs[f.key] = sel;
            formEl.appendChild(h('div', { className: 'form-group' }, h('label', null, f.label), sel));
          } else {
            const inp = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
            inp.value = group[f.key] != null ? String(group[f.key]) : (f.default != null ? String(f.default) : '');
            inputs[f.key] = inp;
            formEl.appendChild(h('div', { className: 'form-group' }, h('label', null, f.label), inp));
          }
        });
        showModal('Edit ' + (gb.singular || 'Group'), formEl, async () => {
          const body = {};
          (gb.fields || []).forEach(f => {
            if (f.type === 'checkbox') body[f.key] = inputs[f.key].checked;
            else if (f.type === 'number') body[f.key] = Number(inputs[f.key].value);
            else body[f.key] = inputs[f.key].value;
          });
          await api.put(gb.apiPath + '/' + group.id, body);
          toast.success((gb.singular || 'Group') + ' updated');
          if (gb.onChanged) gb.onChanged();
          groupsData = await gb.loadGroups();
          filteredCache = null;
          updateTable();
        });
      }

      async function deleteGroupFromHeader(group) {
        const gb = config.groupBy;
        const ok = await confirmDialog('Delete group "' + (group[gb.nameKey] || '') + '"? Channels will become ungrouped.');
        if (!ok) return;
        try {
          await api.del(gb.apiPath + '/' + group.id);
          toast.success((gb.singular || 'Group') + ' deleted');
          if (gb.onChanged) gb.onChanged();
          groupsData = await gb.loadGroups();
          await reloadData();
        } catch (err) {
          toast.error(err.message);
        }
      }

      function manageGroupsModal() {
        const gb = config.groupBy;
        const bodyEl = h('div');

        function renderGroupList() {
          bodyEl.innerHTML = '';
          const sorted = (groupsData || []).slice().sort((a, b) => (a[gb.sortKey] || 0) - (b[gb.sortKey] || 0));

          const addBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: () => {
            const formContent = h('div');
            const cnInputs = {};
            (gb.fields || []).forEach(f => {
              const inp = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
              inp.value = f.default != null ? String(f.default) : '';
              cnInputs[f.key] = inp;
              formContent.appendChild(h('div', { className: 'form-group' }, h('label', null, f.label), inp));
            });
            showModal('Add ' + (gb.singular || 'Group'), formContent, async () => {
              const body = {};
              (gb.fields || []).forEach(f => {
                body[f.key] = f.type === 'number' ? Number(cnInputs[f.key].value) : cnInputs[f.key].value;
              });
              await api.post(gb.apiPath, body);
              toast.success((gb.singular || 'Group') + ' created');
              if (gb.onChanged) gb.onChanged();
              groupsData = await gb.loadGroups();
              renderGroupList();
              filteredCache = null;
              updateTable();
            }, 'Create');
          }}, '+ Add ' + (gb.singular || 'Group'));
          bodyEl.appendChild(h('div', { style: 'margin-bottom:12px' }, addBtn));

          if (sorted.length === 0) {
            bodyEl.appendChild(h('p', { style: 'color:var(--text-muted)' }, 'No groups defined yet.'));
            return;
          }

          sorted.forEach(group => {
            bodyEl.appendChild(h('div', { style: 'display:flex;align-items:center;gap:8px;padding:8px 0;border-bottom:1px solid var(--border)' },
              h('span', { style: 'flex:1;font-weight:500' }, group[gb.nameKey] || 'Unknown'),
              h('span', { style: 'color:var(--text-secondary);font-size:13px;min-width:60px' }, 'Order: ' + (group[gb.sortKey] || 0)),
              h('button', { className: 'btn btn-secondary btn-sm btn-icon', title: 'Edit', onClick: () => editGroupInline(group) }, '\u270E'),
              h('button', { className: 'btn btn-danger btn-sm btn-icon btn-icon-circle', title: 'Delete', onClick: async () => {
                const ok = await confirmDialog('Delete group "' + (group[gb.nameKey] || '') + '"?');
                if (!ok) return;
                try {
                  await api.del(gb.apiPath + '/' + group.id);
                  toast.success((gb.singular || 'Group') + ' deleted');
                  if (gb.onChanged) gb.onChanged();
                  groupsData = await gb.loadGroups();
                  renderGroupList();
                  filteredCache = null;
                  updateTable();
                } catch (err) {
                  toast.error(err.message);
                }
              }}, '\u2715'),
            ));
          });
        }

        renderGroupList();
        showModal('Manage ' + (gb.plural || 'Groups'), bodyEl);
      }

      function buildShell() {
        container.innerHTML = '';

        const headerActions = [];
        if (config.groupBy) {
          headerActions.push(
            h('button', { className: 'btn btn-secondary btn-sm', onClick: () => manageGroupsModal() }, 'Manage Groups')
          );
        }
        if (config.create) {
          headerActions.push(
            h('button', { className: 'btn btn-primary btn-sm btn-icon', title: 'Add New', style: 'font-size:18px', onClick: () => openForm(null) }, '+')
          );
        }
        if (config.extraActions) {
          config.extraActions.forEach(a => {
            headerActions.push(
              h('button', { className: 'btn btn-secondary btn-sm', onClick: () => a.handler(reloadData) }, a.label)
            );
          });
        }

        if (config.groupBy && groupsContainerEl) {
          groupsContainerEl.appendChild(buildGroupedThead());
          container.appendChild(h('div', { className: 'table-container' },
            h('div', { className: 'table-header' },
              countEl,
              h('div', { className: 'btn-group', style: 'align-items: center;' },
                searchInput,
                ...headerActions,
              ),
            ),
            groupsContainerEl,
          ));
        } else {
          container.appendChild(h('div', { className: 'table-container' },
            h('div', { className: 'table-header' },
              countEl,
              h('div', { className: 'btn-group', style: 'align-items: center;' },
                searchInput,
                ...headerActions,
              ),
            ),
            h('table', null,
              h('thead', null,
                h('tr', null,
                  ...config.columns.map(col => { const th = h('th', null, col.label); if (col.thStyle) th.style.cssText = col.thStyle; return th; }),
                  h('th', { style: 'width: 120px' }, 'Actions'),
                ),
              ),
              tbodyEl,
            ),
          ));
          container.appendChild(paginationEl);
        }
      }

      function updateTable() {
        const filtered = getFiltered();

        if (config.groupBy && groupsContainerEl) {
          renderGrouped(filtered);
          return;
        }

        const totalPages = Math.max(1, Math.ceil(filtered.length / perPage));
        if (currentPage > totalPages) currentPage = totalPages;
        const start = (currentPage - 1) * perPage;
        const pageItems = filtered.slice(start, start + perPage);

        const countText = searchTerm
          ? config.title + ' (' + filtered.length + ' of ' + allItems.length + ')'
          : config.title + ' (' + allItems.length + ')';
        countEl.textContent = countText;

        tbodyEl.innerHTML = '';
        if (pageItems.length === 0) {
          tbodyEl.appendChild(
            h('tr', { className: 'empty-row' },
              h('td', { colspan: String(config.columns.length + 1) }, searchTerm ? 'No matching items' : 'No items found'))
          );
        } else {
          for (let i = 0; i < pageItems.length; i++) {
            tbodyEl.appendChild(buildItemRow(pageItems[i]));
          }
        }

        paginationEl.innerHTML = '';
        if (totalPages > 1) {
          const prevBtn = h('button', { className: 'btn btn-secondary btn-sm',
            onClick: () => { currentPage--; updateTable(); } }, 'Prev');
          if (currentPage <= 1) prevBtn.disabled = true;

          const nextBtn = h('button', { className: 'btn btn-secondary btn-sm',
            onClick: () => { currentPage++; updateTable(); } }, 'Next');
          if (currentPage >= totalPages) nextBtn.disabled = true;

          paginationEl.appendChild(prevBtn);

          let startPg = Math.max(1, currentPage - 3);
          let endPg = Math.min(totalPages, startPg + 6);
          if (endPg - startPg < 6) startPg = Math.max(1, endPg - 6);

          if (startPg > 1) {
            paginationEl.appendChild(h('button', { className: 'btn btn-secondary btn-sm',
              onClick: () => { currentPage = 1; updateTable(); } }, '1'));
            if (startPg > 2) paginationEl.appendChild(h('span', { style: 'color: var(--text-muted)' }, '...'));
          }
          for (let i = startPg; i <= endPg; i++) {
            const pg = i;
            paginationEl.appendChild(h('button', {
              className: 'btn btn-sm ' + (pg === currentPage ? 'btn-primary' : 'btn-secondary'),
              onClick: () => { currentPage = pg; updateTable(); },
            }, String(pg)));
          }
          if (endPg < totalPages) {
            if (endPg < totalPages - 1) paginationEl.appendChild(h('span', { style: 'color: var(--text-muted)' }, '...'));
            paginationEl.appendChild(h('button', { className: 'btn btn-secondary btn-sm',
              onClick: () => { currentPage = totalPages; updateTable(); } }, String(totalPages)));
          }

          paginationEl.appendChild(nextBtn);
          paginationEl.appendChild(h('span', { style: 'margin-left: 12px; color: var(--text-muted); font-size: 12px;' },
            'Page ' + currentPage + ' of ' + totalPages +
            ' (' + (start + 1) + '-' + Math.min(start + perPage, filtered.length) + ')'));
        }
      }

      async function reloadData() {
        try {
          if (config.cache) { config.cache.invalidate(); allItems = await config.cache.getAll(); }
          else { allItems = await api.get(config.apiPath); }
          if (config.groupBy) groupsData = await config.groupBy.loadGroups();
          buildSearchIndex();
          filteredCache = null;
          updateTable();
        } catch (err) {
          toast.error('Failed to reload: ' + err.message);
        }
      }

      const reloadHandler = () => { if (container.isConnected) reloadData(); else document.removeEventListener('tvproxy-reload-page', reloadHandler); };
      document.addEventListener('tvproxy-reload-page', reloadHandler);

      function openForm(item, isDuplicate) {
        const isEdit = item !== null && !isDuplicate;
        const formEl = h('div');
        const fields = config.fields || [];
        const inputs = {};

        fields.forEach(field => {
          if (field.type === 'checkbox') {
            const checked = isEdit ? item[field.key] : (field.default || false);
            const cb = h('input', { type: 'checkbox', id: 'field-' + field.key });
            cb.checked = checked;
            inputs[field.key] = cb;
            formEl.appendChild(h('div', { className: 'form-check', style: 'display:flex;align-items:center;gap:6px' }, cb, h('label', { for: 'field-' + field.key, style: 'cursor:pointer;margin:0' }, field.label)));
          } else if (field.type === 'select') {
            const sel = h('select', { id: 'field-' + field.key },
              ...field.options.map(o => {
                const opt = h('option', { value: String(o.value) }, o.label);
                if (isEdit && String(item[field.key]) === String(o.value)) opt.selected = true;
                return opt;
              })
            );
            inputs[field.key] = sel;
            formEl.appendChild(h('div', { className: 'form-group' }, h('label', { for: 'field-' + field.key }, field.label), sel));
          } else if (field.type === 'textarea') {
            const ta = h('textarea', { id: 'field-' + field.key, placeholder: field.placeholder || '', rows: '4' });
            ta.value = isEdit ? (item[field.key] || '') : (field.default || '');
            inputs[field.key] = ta;
            formEl.appendChild(h('div', { className: 'form-group' }, h('label', { for: 'field-' + field.key }, field.label), ta));
          } else if (field.type === 'autocomplete') {
            const wrapper = h('div', { className: 'autocomplete-wrapper' });
            const inp = h('input', {
              type: 'text',
              id: 'field-' + field.key,
              placeholder: field.placeholder || '',
            });
            inp.value = isEdit ? (item[field.key] || '') : '';
            if (isEdit && field.displayValueKey && field.cache && item[field.key]) {
              var editVal = item[field.key];
              inp._selectedValue = editVal;
              field.cache.getAll().then(function(allItems) {
                var match = allItems.find(function(o) { return o[field.valueKey] === editVal; });
                if (match) inp.value = match[field.displayValueKey] || editVal;
              }).catch(function() {});
            }
            const dropdown = h('div', { className: 'autocomplete-dropdown' });
            dropdown.style.display = 'none';

            let acOptions = null;
            let selectedIdx = -1;

            async function ensureOptions() {
              if (field.cache) {
                await field.cache.getAll();
              } else if (acOptions === null && field.loadOptions) {
                acOptions = await field.loadOptions();
              }
              return acOptions || [];
            }

            function renderDropdown(query) {
              dropdown.innerHTML = '';
              const q = (query || '').toLowerCase();
              let matches = [];
              if (q.length >= 1) {
                if (field.cache) {
                  matches = field.cache.search(q, 50);
                } else {
                  if (!acOptions) { dropdown.style.display = 'none'; return; }
                  const startsWith = [];
                  const contains = [];
                  for (let i = 0; i < acOptions.length; i++) {
                    const opt = acOptions[i];
                    const v = (opt[field.valueKey] || '').toLowerCase();
                    const d = (opt[field.displayKey] || '').toLowerCase();
                    if (d.indexOf(q) === 0 || v.indexOf(q) === 0) {
                      startsWith.push(opt);
                    } else if (d.indexOf(q) !== -1 || v.indexOf(q) !== -1) {
                      contains.push(opt);
                    }
                  }
                  const combined = startsWith.concat(contains);
                  for (let i = 0; i < combined.length && i < 50; i++) {
                    matches.push(combined[i]);
                  }
                }
              }
              if (matches.length === 0) { dropdown.style.display = 'none'; return; }
              selectedIdx = -1;
              for (let i = 0; i < matches.length; i++) {
                const opt = matches[i];
                const row = h('div', { className: 'autocomplete-item' });
                if (opt.icon || opt.url) {
                  row.appendChild(h('img', { src: opt.icon || opt.url, className: 'autocomplete-icon' }));
                }
                const text = h('div', { className: 'autocomplete-text' });
                text.appendChild(h('div', { className: 'autocomplete-name' }, opt[field.displayKey] || ''));
                const secondary = field.secondaryKey ? opt[field.secondaryKey] : (field.valueKey !== field.displayKey ? opt[field.valueKey] : null);
                if (secondary) {
                  text.appendChild(h('div', { className: 'autocomplete-id' }, secondary || ''));
                }
                row.appendChild(text);
                row.addEventListener('click', () => {
                  inp.value = (field.displayValueKey ? opt[field.displayValueKey] : opt[field.valueKey]) || '';
                  inp._selectedValue = opt[field.valueKey] || '';
                  dropdown.style.display = 'none';
                  if (field.onSelect) field.onSelect(opt, inputs);
                });
                dropdown.appendChild(row);
              }
              dropdown.style.display = 'block';
            }

            inp.addEventListener('focus', async () => {
              await ensureOptions();
              renderDropdown(inp.value);
            });
            inp.addEventListener('input', () => { delete inp._selectedValue; renderDropdown(inp.value); });

            inp.addEventListener('keydown', (e) => {
              const items = dropdown.querySelectorAll('.autocomplete-item');
              if (!items.length) return;
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                selectedIdx = Math.min(selectedIdx + 1, items.length - 1);
                items.forEach((el, i) => el.classList.toggle('selected', i === selectedIdx));
                items[selectedIdx].scrollIntoView({ block: 'nearest' });
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                selectedIdx = Math.max(selectedIdx - 1, 0);
                items.forEach((el, i) => el.classList.toggle('selected', i === selectedIdx));
                items[selectedIdx].scrollIntoView({ block: 'nearest' });
              } else if (e.key === 'Enter' && selectedIdx >= 0) {
                e.preventDefault();
                items[selectedIdx].click();
              } else if (e.key === 'Escape') {
                dropdown.style.display = 'none';
              }
            });

            setTimeout(() => {
              document.addEventListener('click', function acClose(e) {
                if (!wrapper.contains(e.target)) {
                  dropdown.style.display = 'none';
                }
              });
            }, 0);

            wrapper.appendChild(inp);
            wrapper.appendChild(dropdown);
            inputs[field.key] = inp;
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', { for: 'field-' + field.key }, field.label),
              wrapper,
              field.help ? h('div', { className: 'help-text' }, field.help) : null,
            ));
          } else if (field.type === 'logo-picker') {
            const wrapper = h('div', { className: 'autocomplete-wrapper' });
            wrapper._selectedLogoId = null;
            wrapper.value = '';

            function renderDisplay(logo) {
              wrapper.innerHTML = '';
              if (!logo) {
                renderSearch();
                return;
              }
              wrapper._selectedLogoId = logo.id;
              wrapper.value = String(logo.id);
              const display = h('div', { className: 'logo-picker-display' });
              if (logo.url) {
                display.appendChild(h('img', { src: logo.url, style: 'width:32px;height:32px;object-fit:contain;border-radius:2px;background:var(--bg-input);' }));
              }
              const nameLink = h('a', { href: '#', style: 'flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' }, logo.name || 'Untitled');
              nameLink.addEventListener('click', (e) => { e.preventDefault(); navigate('logos'); });
              display.appendChild(nameLink);
              const changeLink = h('a', { href: '#', style: 'font-size:13px;flex-shrink:0;' }, 'Change');
              changeLink.addEventListener('click', (e) => { e.preventDefault(); renderSearch(); });
              display.appendChild(changeLink);
              const removeBtn = h('span', { style: 'cursor:pointer;color:var(--text-muted);font-size:16px;flex-shrink:0;', title: 'Remove logo' }, '\u00d7');
              removeBtn.addEventListener('click', () => {
                wrapper._selectedLogoId = null;
                wrapper.value = '';
                renderSearch();
              });
              display.appendChild(removeBtn);
              wrapper.appendChild(display);
            }

            function renderSearch() {
              wrapper.innerHTML = '';
              const inp = h('input', { type: 'text', id: 'field-' + field.key, placeholder: 'Search logos...' });
              const dropdown = h('div', { className: 'autocomplete-dropdown' });
              dropdown.style.display = 'none';
              let selectedIdx = -1;

              function renderDropdown(query) {
                dropdown.innerHTML = '';
                const q = (query || '').toLowerCase();
                if (q.length < 1) { dropdown.style.display = 'none'; return; }
                const matches = field.cache.search(q, 50);
                if (matches.length === 0) { dropdown.style.display = 'none'; return; }
                selectedIdx = -1;
                for (let i = 0; i < matches.length; i++) {
                  const opt = matches[i];
                  const row = h('div', { className: 'autocomplete-item' });
                  if (opt.url) {
                    row.appendChild(h('img', { src: opt.url, className: 'autocomplete-icon' }));
                  }
                  const text = h('div', { className: 'autocomplete-text' });
                  text.appendChild(h('div', { className: 'autocomplete-name' }, opt.name || ''));
                  row.appendChild(text);
                  row.addEventListener('click', () => { renderDisplay(opt); });
                  dropdown.appendChild(row);
                }
                dropdown.style.display = 'block';
              }

              inp.addEventListener('focus', async () => {
                await field.cache.getAll();
                renderDropdown(inp.value);
              });
              inp.addEventListener('input', () => { renderDropdown(inp.value); });
              inp.addEventListener('keydown', (e) => {
                const items = dropdown.querySelectorAll('.autocomplete-item');
                if (!items.length) return;
                if (e.key === 'ArrowDown') {
                  e.preventDefault();
                  selectedIdx = Math.min(selectedIdx + 1, items.length - 1);
                  items.forEach((el, i) => el.classList.toggle('selected', i === selectedIdx));
                  items[selectedIdx].scrollIntoView({ block: 'nearest' });
                } else if (e.key === 'ArrowUp') {
                  e.preventDefault();
                  selectedIdx = Math.max(selectedIdx - 1, 0);
                  items.forEach((el, i) => el.classList.toggle('selected', i === selectedIdx));
                  items[selectedIdx].scrollIntoView({ block: 'nearest' });
                } else if (e.key === 'Enter' && selectedIdx >= 0) {
                  e.preventDefault();
                  items[selectedIdx].click();
                } else if (e.key === 'Escape') {
                  dropdown.style.display = 'none';
                }
              });

              setTimeout(() => {
                document.addEventListener('click', function lpClose(e) {
                  if (!wrapper.contains(e.target)) {
                    dropdown.style.display = 'none';
                  }
                });
              }, 0);

              wrapper.appendChild(inp);
              wrapper.appendChild(dropdown);
            }

            wrapper._setLogo = function(logo) { renderDisplay(logo); };

            if (isEdit && item.logo_id) {
              field.cache.getAll().then(logos => {
                const found = logos.find(l => l.id === item.logo_id);
                if (found) renderDisplay(found);
                else renderSearch();
              });
            } else {
              renderSearch();
            }

            inputs[field.key] = wrapper;
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', null, field.label),
              wrapper,
              field.help ? h('div', { className: 'help-text' }, field.help) : null,
            ));
          } else if (field.type === 'async-select') {
            const sel = h('select', { id: 'field-' + field.key });
            sel.appendChild(h('option', { value: '' }, field.emptyLabel || '-- None --'));
            const currentVal = isEdit ? item[field.key] : null;

            function loadSelectOpts(autoSelectId) {
              while (sel.children.length > 1) sel.removeChild(sel.lastChild);
              if (!field.loadOptions) return;
              field.loadOptions().then(options => {
                for (const opt of (options || [])) {
                  const optEl = h('option', { value: String(opt[field.valueKey || 'id']) },
                    opt[field.displayKey || 'name']);
                  const matchVal = autoSelectId || currentVal;
                  if (matchVal != null && String(matchVal) === String(opt[field.valueKey || 'id'])) {
                    optEl.selected = true;
                  }
                  sel.appendChild(optEl);
                }
              }).catch(() => {});
            }
            loadSelectOpts();

            inputs[field.key] = sel;
            let selectRow;
            if (field.createNew) {
              sel.style.flex = '1';
              const addBtn = h('button', { className: 'btn btn-secondary btn-sm', style: 'flex-shrink:0;', onClick: (e) => {
                e.preventDefault();
                const cn = field.createNew;
                const formContent = h('div');
                const cnInputs = {};
                (cn.fields || []).forEach(f => {
                  const inp = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
                  inp.value = f.default != null ? String(f.default) : '';
                  cnInputs[f.key] = inp;
                  formContent.appendChild(h('div', { className: 'form-group' }, h('label', null, f.label), inp));
                });
                showModal(cn.label || 'Create New', formContent, async () => {
                  const body = {};
                  (cn.fields || []).forEach(f => {
                    body[f.key] = f.type === 'number' ? Number(cnInputs[f.key].value) : cnInputs[f.key].value;
                  });
                  const result = await api.post(cn.apiPath, body);
                  toast.success('Created successfully');
                  if (cn.onCreated) cn.onCreated();
                  if (config.groupBy && field.key === config.groupBy.key) {
                    groupsData = await config.groupBy.loadGroups();
                  }
                  loadSelectOpts(result.id);
                }, 'Create');
              }}, '+');
              selectRow = h('div', { style: 'display:flex;gap:6px;align-items:center;' }, sel, addBtn);
            } else {
              selectRow = sel;
            }
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', { for: 'field-' + field.key }, field.label),
              selectRow,
              field.help ? h('div', { className: 'help-text' }, field.help) : null,
            ));
          } else if (field.type === 'async-multi-select') {
            const container = h('div', { id: 'field-' + field.key, className: 'checkbox-group', style: 'display:flex;flex-direction:column;gap:6px;padding:4px 0;' });
            const currentVals = isEdit && Array.isArray(item[field.key]) ? item[field.key].map(String) : [];
            if (field.loadOptions) {
              field.loadOptions().then(options => {
                for (const opt of (options || [])) {
                  const val = String(opt[field.valueKey || 'id']);
                  const cb = h('input', { type: 'checkbox', value: val, id: 'field-' + field.key + '-' + val });
                  if (currentVals.includes(val)) cb.checked = true;
                  const lbl = h('label', { for: 'field-' + field.key + '-' + val, style: 'display:flex;align-items:center;gap:6px;cursor:pointer;' },
                    cb, opt[field.displayKey || 'name']);
                  container.appendChild(lbl);
                }
                if ((options || []).length === 0) {
                  container.appendChild(h('div', { style: 'color:var(--text-secondary);font-size:0.85em;' }, 'No options available'));
                }
              }).catch(() => {});
            }
            inputs[field.key] = container;
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', null, field.label),
              container,
              field.help ? h('div', { className: 'help-text' }, field.help) : null,
            ));
          } else {
            const inp = h('input', {
              type: field.type || 'text',
              id: 'field-' + field.key,
              placeholder: field.placeholder || '',
            });
            inp.value = isEdit ? (item[field.key] != null ? String(item[field.key]) : '') : (field.default != null ? String(field.default) : '');
            inputs[field.key] = inp;
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', { for: 'field-' + field.key }, field.label),
              inp,
              field.help ? h('div', { className: 'help-text' }, field.help) : null,
            ));
          }
        });

        const conditionalFields = [];
        fields.forEach(field => {
          if (field.showWhen && inputs[field.key]) {
            const wrapper = inputs[field.key].closest('.form-group') || inputs[field.key].parentElement;
            conditionalFields.push({ field, wrapper });
          }
        });
        if (conditionalFields.length > 0) {
          const updateVisibility = () => {
            const formValues = {};
            fields.forEach(f => {
              const el = inputs[f.key];
              if (el) formValues[f.key] = f.type === 'checkbox' ? el.checked : el.value;
            });
            conditionalFields.forEach(({ field, wrapper }) => {
              wrapper.style.display = field.showWhen(formValues) ? '' : 'none';
            });
          };
          fields.forEach(f => {
            const el = inputs[f.key];
            if (el) el.addEventListener('change', updateVisibility);
          });
          updateVisibility();
        }

        if (config.postFormSetup) config.postFormSetup(inputs, isEdit, item);

        showModal(
          isEdit ? 'Edit ' + config.singular : 'Add ' + config.singular,
          formEl,
          async () => {
            if (config.preSave) await config.preSave(inputs);
            const body = {};
            fields.forEach(field => {
              if (field.readOnly && isEdit) return;
              if (field.exclude) return;
              const el = inputs[field.key];
              if (field.type === 'checkbox') {
                body[field.key] = el.checked;
              } else if (field.type === 'number') {
                body[field.key] = el.value ? Number(el.value) : 0;
              } else if (field.type === 'async-select') {
                body[field.key] = el.value || null;
              } else if (field.type === 'async-multi-select') {
                const checked = [];
                el.querySelectorAll('input[type="checkbox"]:checked').forEach(cb => checked.push(cb.value));
                body[field.key] = checked;
              } else if (field.type === 'logo-picker') {
                body[field.key] = el._selectedLogoId || null;
              } else if (field.type === 'autocomplete' && el._selectedValue !== undefined) {
                body[field.key] = el._selectedValue;
              } else {
                body[field.key] = el.value;
              }
            });
            let result;
            if (isEdit) {
              result = await api.put(config.apiPath + '/' + item.id, body);
              toast.success(config.singular + ' updated');
            } else {
              result = await api.post(config.apiPath, body);
              toast.success(config.singular + ' created');
            }
            if (config.postSave) await config.postSave(result, inputs, isEdit, item);
            if (config.onChange) config.onChange();
            await reloadData();
          },
          isEdit ? 'Save Changes' : 'Create',
        );
      }

      async function deleteItem(item) {
        const name = item.name || item.username || item.key || item.url || 'this item';
        const ok = await confirmDialog('Delete "' + name + '"? This cannot be undone.');
        if (!ok) return;
        try {
          await api.del(config.apiPath + '/' + item.id);
          toast.success(config.singular + ' deleted');
          if (config.onChange) config.onChange();
          await reloadData();
        } catch (err) {
          toast.error(err.message);
        }
      }

      buildShell();
      updateTable();
    };
  }

  const CODEC_NAMES = {
    avc1:'H.264', h264:'H.264', hev1:'H.265', hvc1:'H.265',
    vp8:'VP8', vp9:'VP9', av01:'AV1', mp4a:'AAC', aac:'AAC',
    'ac-3':'AC-3', opus:'Opus', mp3:'MP3', flac:'FLAC'
  };
  function codecName(s) { if (!s) return '?'; return CODEC_NAMES[s.split('.')[0].toLowerCase()] || s; }

  let activePlayerCleanup = null;
  let playInProgress = false;

  function createAudioSession(vodPath) {
    return api.post(vodPath + '?profile=Browser').then(function(resp) {
      return resp && resp.session_id ? resp : null;
    });
  }

  function createAudioElement(src) {
    var audio = new Audio(src);
    audio.volume = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
    return audio;
  }

  function releaseAudioSession(sessionID, consumerID) {
    if (!sessionID) return;
    api.del('/vod/' + sessionID + (consumerID ? '?consumer_id=' + consumerID : '')).catch(function() {});
  }

  function buildFloatingRadioBar(name, onStop) {
    var bar = document.createElement('div');
    bar.style.cssText = 'position:fixed;bottom:0;left:0;right:0;background:var(--bg-card);border-top:1px solid var(--border);padding:8px 16px;display:flex;align-items:center;gap:12px;z-index:9999;';
    var nameEl = document.createElement('span');
    nameEl.style.cssText = 'color:#e0e0e0;font-size:14px;flex:1;';
    nameEl.textContent = name;
    var stopBtn = document.createElement('button');
    stopBtn.className = 'btn btn-sm';
    stopBtn.textContent = '\u23F9 Stop';
    stopBtn.onclick = onStop;
    bar.appendChild(nameEl);
    bar.appendChild(stopBtn);
    document.body.appendChild(bar);
    return { bar: bar, nameEl: nameEl };
  }

  var floatingRadio = null;
  function startFloatingRadio(id, name, tvgId, isChannel, directUrl) {
    stopFloatingRadio();
    var ui = buildFloatingRadioBar(name, stopFloatingRadio);

    if (directUrl) {
      var audio = createAudioElement(directUrl);
      audio.onplaying = function() { ui.nameEl.textContent = '\u25B6 ' + name; };
      audio.onerror = function() { stopFloatingRadio(); };
      audio.play().catch(function() {});
      floatingRadio = { audio: audio, bar: ui.bar, sessionID: null, consumerID: null };
      return;
    }

    var vodPath = isChannel ? '/channel/' + id + '/vod' : '/stream/' + id + '/vod';
    createAudioSession(vodPath)
      .then(function(resp) {
        if (!resp) { ui.bar.remove(); return; }
        var audio = createAudioElement('/vod/' + resp.session_id + '/stream');
        audio.onplaying = function() { ui.nameEl.textContent = '\u25B6 ' + name; };
        audio.onerror = function() { stopFloatingRadio(); };
        audio.play().catch(function() {});

        if (tvgId) {
          api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(function(p) {
            if (p && p.title) ui.nameEl.textContent = '\u25B6 ' + name + ' \u2014 ' + p.title;
          }).catch(function() {});
        }

        floatingRadio = { audio: audio, bar: ui.bar, sessionID: resp.session_id, consumerID: resp.consumer_id };
      }).catch(function() { ui.bar.remove(); });
  }
  function stopFloatingRadio() {
    if (!floatingRadio) return;
    floatingRadio.audio.pause();
    floatingRadio.audio.removeAttribute('src');
    floatingRadio.bar.remove();
    releaseAudioSession(floatingRadio.sessionID, floatingRadio.consumerID);
    floatingRadio = null;
  }

  function isAudioOnly(streamTracks, streamGroup) {
    if (streamGroup && streamGroup.toLowerCase() === 'radio') return true;
    if (!streamTracks || !streamTracks.length) return false;
    return !streamTracks.some(function(t) { return t.category === 'video'; });
  }

  async function play(opts) {
    if (playInProgress) return;
    playInProgress = true;
    document.body.style.cursor = 'wait';
    try {
      if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
      var streamTracks = null;
      var streamGroup = null;
      var streamID = opts.streamID;
      var channelID = opts.channelID;
      var name = opts.name || '';
      var tvgId = opts.tvgId;

      var lookupID = streamID;
      if (!lookupID && channelID && channelsCache._data) {
        var ch = channelsCache._data.find(function(c) { return c.id === channelID; });
        if (ch && ch.stream_ids && ch.stream_ids.length > 0) {
          lookupID = ch.stream_ids[0];
        }
      }
      if (lookupID && streamsCache._data) {
        var s = streamsCache._data.find(function(s) { return s.id === lookupID; });
        if (s) {
          if (s.tracks) streamTracks = s.tracks;
          if (s.group) streamGroup = s.group;
        }
      }

      if (isAudioOnly(streamTracks, streamGroup) && !opts.fileUrl) {
        var radioID = streamID || (channelID ? channelID : null);
        if (radioID) startFloatingRadio(radioID, name, tvgId, !!channelID);
        return;
      }

      if (opts.fileUrl) {
        if (isAudioOnly(streamTracks, streamGroup)) {
          startFloatingRadio(null, name, tvgId, false, opts.fileUrl);
        } else {
          openVideoModal(name, opts.fileUrl, tvgId, null, null);
        }
        return;
      }

      if (opts.recordingPath) {
        try {
          var recResp = await api.post(opts.recordingPath + '?profile=Browser');
          if (recResp.audio_only && recResp.session_id) {
            api.del('/vod/' + recResp.session_id + (recResp.consumer_id ? '?consumer_id=' + recResp.consumer_id : '')).catch(function() {});
            startFloatingRadio(null, name, tvgId, false, opts.recordingPath.replace('/play', '/stream'));
          } else if (recResp.session_id) {
            var recSession = { id: recResp.session_id, consumer_id: recResp.consumer_id, duration: recResp.duration, container: recResp.container };
            openVideoModal(name, null, tvgId, recSession, recResp.session_id);
          }
        } catch(e) {}
        return;
      }

      var defaultAudio = defaultAudioIndex(streamTracks);
      var audioParam = defaultAudio > 0 ? '&audio=' + defaultAudio : '';
      var vodPath = streamID ? '/stream/' + streamID + '/vod' : '/channel/' + channelID + '/vod';
      let session = null;
      try {
        const resp = await api.post(vodPath + '?profile=Browser' + audioParam);
        if (resp.audio_only) {
          if (resp.session_id) {
            api.del('/vod/' + resp.session_id + (resp.consumer_id ? '?consumer_id=' + resp.consumer_id : '')).catch(function() {});
          }
          var radioID = streamID || channelID;
          if (radioID) startFloatingRadio(radioID, name, tvgId, !!channelID);
          return;
        }
        if (resp.session_id) {
          session = { id: resp.session_id, consumer_id: resp.consumer_id, duration: resp.duration, container: resp.container, request_headers: resp.request_headers };
        }
      } catch(e) {
        playInProgress = false;
        document.body.style.cursor = '';
        return;
      }
      openVideoModal(name, vodPath.replace('/vod', '') + '?profile=Browser', tvgId, session, channelID || null, streamTracks, streamGroup);
    } finally {
      playInProgress = false;
      document.body.style.cursor = '';
    }
  }

  function defaultAudioIndex(tracks) {
    if (!tracks) return 0;
    var audioTracks = tracks.filter(function(t) { return t.category === 'audio'; });
    for (var i = 0; i < audioTracks.length; i++) {
      if (audioTracks[i].audio_type !== 3) return i;
    }
    return 0;
  }


  function openVideoModal(title, url, tvgId, dvr, channelID, streamTracks, streamGroup) {
    if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
    const playerCtx = new AbortController();
    let pollInterval = null;
    let progInterval = null;
    let signalInterval = null;
    var transcodeTimer = null;
    var ctrlUpdateTimer = null;
    let signalData = null;
    let satipStreamUrl = null;
    let nowProgram = null;
    let nowPlayingEl = null;
    let isRecording = false;
    let pollFailures = 0;
    let currentAudioIndex = 0;
    let currentProfile = 'Browser';
    let probeData = null;
    let isLive = !!channelID || !dvr || !dvr.duration;
    const dvrTracker = dvr ? createDVRTracker(isLive, isLive ? 0 : dvr.duration) : null;
    let retryCount = 0;
    const MAX_RETRIES = 3;
    let retryTimeout = null;
    let statsInterval = null;
    const audioOnly = isAudioOnly(streamTracks, streamGroup);


    function cleanup() {
      activePlayerCleanup = null;
      playerCtx.abort();
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (pollInterval) { clearInterval(pollInterval); pollInterval = null; }
      if (progInterval) { clearInterval(progInterval); progInterval = null; }
      if (recordElapsedTimer) { clearInterval(recordElapsedTimer); recordElapsedTimer = null; }
      if (transcodeTimer) { clearInterval(transcodeTimer); transcodeTimer = null; }
      if (ctrlUpdateTimer) { clearInterval(ctrlUpdateTimer); ctrlUpdateTimer = null; }
      if (signalInterval) { clearInterval(signalInterval); signalInterval = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      if (dashPlayer) {
        dashPlayer.destroy();
        dashPlayer = null;
      } else if (videoEl) {
        videoEl.pause();
        videoEl.removeAttribute('src');
        videoEl.load();
      }
      if (dvr && !isRecording) {
        api.del('/vod/' + dvr.id + (dvr.consumer_id ? '?consumer_id=' + dvr.consumer_id : '')).catch(function() {});
      }
      overlay.remove();
      if (channelID && state.currentPage === 'channels') {
        document.dispatchEvent(new CustomEvent('tvproxy-reload-page'));
      }
    }
    activePlayerCleanup = cleanup;

    function fmtTime(secs) {
      var h = Math.floor(secs / 3600);
      var m = Math.floor((secs % 3600) / 60);
      var s = Math.floor(secs % 60);
      return h > 0 ? h + ':' + String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0')
                    : m + ':' + String(s).padStart(2, '0');
    }

    var overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.85);z-index:10000;display:flex;align-items:' + (window.innerWidth <= 768 ? 'flex-start' : 'center') + ';justify-content:center;' + (window.innerWidth <= 768 ? 'padding-top:env(safe-area-inset-top);' : '');
    var modal = document.createElement('div');
    modal.style.cssText = 'background:#000;border-radius:12px;max-width:900px;width:' + (window.innerWidth <= 768 ? '100%' : '90%') + ';position:relative;overflow:hidden;' + (window.innerWidth <= 768 ? 'border-radius:0;margin:0;' : '');

    var playerWrap = document.createElement('div');
    playerWrap.setAttribute('data-shaka-player-container', '');
    playerWrap.style.cssText = 'position:relative;border-radius:12px;overflow:hidden;' + (audioOnly ? '' : 'aspect-ratio:16/9;') + 'background:#000;';

    var videoEl = document.createElement('video');
    videoEl.setAttribute('playsinline', '');
    videoEl.setAttribute('data-shaka-player', '');
    videoEl.style.cssText = 'width:100%;height:100%;display:block;';
    playerWrap.appendChild(videoEl);

    var recordBtn = document.createElement('button');
    recordBtn.title = 'Record';
    recordBtn.textContent = '\u23FA';
    var recordStartTime = null;
    var recordElapsedTimer = null;
    function startRecordingUI() {
      isRecording = true;
      recordStartTime = Date.now();
      recordBtn.style.color = '#e53935';
      recordBtn.title = 'Stop Recording';
      updateStatusText();
      if (recordElapsedTimer) clearInterval(recordElapsedTimer);
      recordElapsedTimer = setInterval(updateStatusText, 1000);
    }
    function stopRecordingUI() {
      isRecording = false;
      recordStartTime = null;
      if (recordElapsedTimer) { clearInterval(recordElapsedTimer); recordElapsedTimer = null; }
      recordBtn.style.color = '';
      recordBtn.title = 'Record';
      updateStatusText();
    }
    recordBtn.onclick = function() {
      if (!dvr) return;
      recordBtn.disabled = true;
      var recordChannelID = channelID || (dvr ? dvr.id : null);
      if (!recordChannelID) { recordBtn.disabled = false; return; }
      if (isRecording) {
        api.del('/api/vod/record/' + recordChannelID).then(function() { stopRecordingUI(); }).catch(function() {}).finally(function() { recordBtn.disabled = false; });
      } else {
        startRecordingUI();
        api.post('/api/vod/record/' + recordChannelID, { program_title: title, channel_name: title }).catch(function() { stopRecordingUI(); }).finally(function() { recordBtn.disabled = false; });
      }
    };

    var statsBtn = document.createElement('button');
    statsBtn.textContent = '\u2139';
    statsBtn.title = 'Stats';

    var audioBtn = document.createElement('button');
    audioBtn.textContent = '\uD83C\uDF99';
    audioBtn.title = 'Audio Track';
    audioBtn.style.display = 'none';
    var audioMenu = document.createElement('div');
    audioMenu.style.cssText = 'display:none;position:absolute;top:44px;right:80px;background:rgba(0,0,0,0.9);backdrop-filter:blur(8px);border-radius:8px;padding:4px 0;z-index:30;min-width:180px;pointer-events:auto;';
    audioBtn.onclick = function(e) {
      e.stopPropagation();
      audioMenu.style.display = audioMenu.style.display === 'none' ? 'block' : 'none';
    };

    function buildAudioMenu(tracks) {
      audioMenu.innerHTML = '';
      if (!tracks || tracks.length < 2) { audioBtn.style.display = 'none'; return; }
      audioBtn.style.display = '';
      tracks.forEach(function(track, idx) {
        var label = track.codec || '?';
        if (track.language) label += ' [' + track.language + ']';
        if (track.channels) label += ' ' + track.channels + 'ch';
        if (track.audio_type === 3) label += ' (AD)';
        var item = document.createElement('div');
        item.style.cssText = 'padding:6px 14px;color:#fff;font-size:13px;cursor:pointer;white-space:nowrap;' + (idx === currentAudioIndex ? 'background:rgba(255,255,255,0.15);' : '');
        item.textContent = (idx === currentAudioIndex ? '\u2713 ' : '   ') + label;
        item.onclick = function() {
          if (idx === currentAudioIndex) { audioMenu.style.display = 'none'; return; }
          currentAudioIndex = idx;
          audioMenu.style.display = 'none';
          switchAudioTrack(idx);
        };
        audioMenu.appendChild(item);
      });
    }

    function switchAudioTrack(audioIndex) {
      if (!dvr || !channelID) return;
      if (isRecording) {
        toast.warn('Cannot switch audio while recording');
        buildAudioMenu(probeData ? probeData.audio_tracks : []);
        return;
      }
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Switching audio...';
      (async function() {
        try {
          await api.del('/vod/' + dvr.id + (dvr.consumer_id ? '?consumer_id=' + dvr.consumer_id : '')).catch(function() {});
          var audioParam = audioIndex > 0 ? '&audio=' + audioIndex : '';
          var vodPath = '/channel/' + channelID + '/vod';
          var resp = await api.post(vodPath + '?profile=' + encodeURIComponent(currentProfile) + audioParam);
          if (resp.session_id) {
            dvr = { id: resp.session_id, consumer_id: resp.consumer_id, duration: resp.duration, container: resp.container };
            if (dvrTracker) dvrTracker.reset();
            streamSrc = '/vod/' + dvr.id + '/dash/manifest.mpd';
          }
        } catch(e) {
          toast.error('Audio switch failed');
        }
        restartPlayback();
      })();
    }

    var closeBtn = document.createElement('button');
    closeBtn.textContent = '\u2715';
    closeBtn.title = 'Close';
    closeBtn.onclick = cleanup;

    var titleEl = document.createElement('span');
    titleEl.textContent = title;

    var floatBar = document.createElement('div');
    floatBar.style.cssText = 'position:absolute;top:0;left:0;right:0;display:flex;align-items:center;gap:8px;padding:8px 12px;background:linear-gradient(rgba(0,0,0,0.7),transparent);opacity:0;transition:opacity 0.2s;z-index:20;pointer-events:none;';
    var favBtn = channelID ? favoriteButton(channelID) : null;
    var barBtns = favBtn ? [titleEl, favBtn, recordBtn, audioBtn, statsBtn, closeBtn] : [titleEl, recordBtn, audioBtn, statsBtn, closeBtn];
    titleEl.style.cssText = 'flex:1;color:#fff;font-size:14px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-shadow:0 1px 2px rgba(0,0,0,0.5);';
    var btnStyle = 'background:rgba(255,255,255,0.15);backdrop-filter:blur(8px);border:none;color:#fff;width:32px;height:32px;border-radius:50%;font-size:14px;cursor:pointer;pointer-events:auto;transition:background 0.15s;';
    recordBtn.style.cssText = btnStyle;
    audioBtn.style.cssText = btnStyle;
    statsBtn.style.cssText = btnStyle;
    closeBtn.style.cssText = btnStyle;
    barBtns.forEach(function(b) { floatBar.appendChild(b); });
    playerWrap.appendChild(floatBar);

    playerWrap.addEventListener('mouseenter', function() { floatBar.style.opacity = '1'; });
    playerWrap.addEventListener('mouseleave', function() { floatBar.style.opacity = '0'; });
    videoEl.addEventListener('click', function() {
      if (videoEl.paused) videoEl.play(); else videoEl.pause();
    });
    playerWrap.addEventListener('click', function(e) {
      if (e.target !== audioBtn && !audioMenu.contains(e.target)) audioMenu.style.display = 'none';
    });

    var statsOverlay = document.createElement('div');
    statsOverlay.style.cssText = 'display:none;position:absolute;top:8px;left:8px;background:rgba(0,0,0,0.8);color:#fff;padding:10px 12px;border-radius:6px;font-size:11px;font-family:monospace;line-height:1.6;z-index:100;max-height:80%;overflow-y:auto;pointer-events:none;';
    statsBtn.onclick = function() { statsOverlay.style.display = statsOverlay.style.display === 'none' ? 'block' : 'none'; };
    playerWrap.appendChild(statsOverlay);
    playerWrap.appendChild(audioMenu);

    var ctrlBar = document.createElement('div');
    ctrlBar.style.cssText = 'position:absolute;bottom:0;left:0;right:0;background:linear-gradient(transparent,rgba(0,0,0,0.8));padding:0 12px 8px;z-index:20;opacity:0;transition:opacity 0.2s;';
    playerWrap.addEventListener('mouseenter', function() { ctrlBar.style.opacity = '1'; });
    playerWrap.addEventListener('mouseleave', function() { ctrlBar.style.opacity = '0'; });

    var seekRow = document.createElement('div');
    seekRow.style.cssText = 'position:relative;height:12px;cursor:pointer;margin-bottom:4px;display:flex;align-items:center;';
    var seekTrack = document.createElement('div');
    seekTrack.style.cssText = 'position:absolute;left:0;right:0;height:4px;background:rgba(255,255,255,0.2);border-radius:2px;transition:height 0.1s;';
    var seekTranscoded = document.createElement('div');
    seekTranscoded.style.cssText = 'position:absolute;left:0;height:100%;background:rgba(255,255,255,0.3);border-radius:2px;width:0%;';
    var seekBuffered = document.createElement('div');
    seekBuffered.style.cssText = 'position:absolute;left:0;height:100%;background:rgba(255,255,255,0.5);border-radius:2px;width:0%;';
    var seekPlayed = document.createElement('div');
    seekPlayed.style.cssText = 'position:absolute;left:0;height:100%;background:#4fc3f7;border-radius:2px;width:0%;';
    var seekThumb = document.createElement('div');
    seekThumb.style.cssText = 'position:absolute;width:12px;height:12px;background:#fff;border-radius:50%;top:50%;transform:translate(-50%,-50%);left:0%;opacity:0;transition:opacity 0.15s;z-index:1;';
    seekTrack.appendChild(seekTranscoded);
    seekTrack.appendChild(seekBuffered);
    seekTrack.appendChild(seekPlayed);
    seekRow.appendChild(seekTrack);
    seekRow.appendChild(seekThumb);
    seekRow.addEventListener('mouseenter', function() { seekTrack.style.height = '6px'; seekThumb.style.opacity = '1'; });
    seekRow.addEventListener('mouseleave', function() { seekTrack.style.height = '4px'; seekThumb.style.opacity = '0'; });
    seekRow.addEventListener('click', function(e) {
      var rect = seekTrack.getBoundingClientRect();
      var pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      if (dashPlayer) {
        var win = dashPlayer.getDvrWindow();
        var knownDur = dvr.duration || epgDuration || 0;
        var seekOff = (dvr && dvr._seekOffset) || 0;
        var effectiveDur = knownDur > 0 ? knownDur : (seekOff + win.end);
        var absTarget = pct * effectiveDur;
        var relTarget = absTarget - seekOff;
        if (relTarget > win.end && dvr && dvr.duration > 0 && !seekRow._seeking) {
          seekRow._seeking = true;
          statusEl.style.color = '#ffa726';
          statusEl.textContent = 'Seeking...';
          dvr._seekOffset = absTarget;
          fetch('/vod/' + dvr.id + '/seek?position=' + absTarget.toFixed(1), { method: 'POST' }).then(function() {
            restartPlayback();
          }).catch(function() {
            statusEl.textContent = 'Seek failed';
          }).finally(function() {
            seekRow._seeking = false;
          });
          return;
        }
        var dashTarget = Math.max(win.start, Math.min(win.end - 1, relTarget));
        console.log('SEEK: pct=' + pct.toFixed(3), 'absTarget=' + absTarget.toFixed(1), 'relTarget=' + relTarget.toFixed(1), 'dashTarget=' + dashTarget.toFixed(1), 'seekOff=' + seekOff.toFixed(1));
        dashPlayer.seek(dashTarget);
      } else {
        var dur = videoEl.duration;
        if (dur && isFinite(dur)) videoEl.currentTime = pct * dur;
      }
    });

    var ctrlBtns = document.createElement('div');
    ctrlBtns.style.cssText = 'display:flex;align-items:center;gap:8px;';

    var playBtn = document.createElement('button');
    playBtn.style.cssText = 'background:none;border:none;color:#fff;font-size:18px;cursor:pointer;padding:4px;';
    playBtn.textContent = '\u25B6';
    playBtn.onclick = function() {
      if (videoEl.paused) { videoEl.play(); playBtn.textContent = '\u23F8'; }
      else { videoEl.pause(); playBtn.textContent = '\u25B6'; }
    };
    videoEl.addEventListener('play', function() { playBtn.textContent = '\u23F8'; });
    videoEl.addEventListener('pause', function() { playBtn.textContent = '\u25B6'; });

    var timeDisplay = document.createElement('span');
    timeDisplay.style.cssText = 'color:#fff;font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap;min-width:90px;';
    timeDisplay.textContent = '0:00 / 0:00';

    var spacer = document.createElement('div');
    spacer.style.cssText = 'flex:1;';

    var volBtn = document.createElement('button');
    volBtn.style.cssText = 'background:none;border:none;color:#fff;font-size:16px;cursor:pointer;padding:4px;';
    volBtn.textContent = '\uD83D\uDD0A';
    volBtn.onclick = function() {
      videoEl.muted = !videoEl.muted;
      volBtn.textContent = videoEl.muted ? '\uD83D\uDD07' : '\uD83D\uDD0A';
    };

    var fsBtn = document.createElement('button');
    fsBtn.style.cssText = 'background:none;border:none;color:#fff;font-size:16px;cursor:pointer;padding:4px;';
    fsBtn.textContent = '\u26F6';
    fsBtn.onclick = function() {
      if (document.fullscreenElement) document.exitFullscreen();
      else playerWrap.requestFullscreen().catch(function() {});
    };

    ctrlBtns.appendChild(playBtn);
    ctrlBtns.appendChild(timeDisplay);
    ctrlBtns.appendChild(spacer);
    ctrlBtns.appendChild(volBtn);
    ctrlBtns.appendChild(fsBtn);

    ctrlBar.appendChild(seekRow);
    ctrlBar.appendChild(ctrlBtns);
    playerWrap.appendChild(ctrlBar);

    function fmtCtrlTime(s) {
      if (!isFinite(s) || s < 0) s = 0;
      var h = Math.floor(s / 3600);
      var m = Math.floor((s % 3600) / 60);
      var sec = Math.floor(s % 60);
      return h > 0 ? h + ':' + String(m).padStart(2, '0') + ':' + String(sec).padStart(2, '0')
                    : m + ':' + String(sec).padStart(2, '0');
    }

    var ctrlUpdateTimer = setInterval(function() {
      if (playerCtx.signal.aborted) { clearInterval(ctrlUpdateTimer); return; }
      var cur = videoEl.currentTime || 0;
      var dur = videoEl.duration;
      var knownDur = dvr.duration || epgDuration || (isFinite(dur) ? dur : 0);

      if (dashPlayer) {
        var win = dashPlayer.getDvrWindow();
        var seekOff = (dvr && dvr._seekOffset) || 0;
        var effectiveDur = knownDur > 0 ? knownDur : (seekOff + win.end);
        if (win.size > 0 && effectiveDur > 0) {
          var absCur = seekOff + cur;
          var absTranscoded = seekOff + win.end;
          seekPlayed.style.width = ((absCur / effectiveDur) * 100) + '%';
          seekThumb.style.left = ((absCur / effectiveDur) * 100) + '%';
          seekTranscoded.style.width = ((absTranscoded / effectiveDur) * 100) + '%';
          var bufEnd = 0;
          if (videoEl.buffered.length > 0) bufEnd = seekOff + videoEl.buffered.end(videoEl.buffered.length - 1);
          seekBuffered.style.width = ((bufEnd / effectiveDur) * 100) + '%';
          timeDisplay.textContent = fmtCtrlTime(absCur) + ' / ' + fmtCtrlTime(effectiveDur);
        } else {
          var absCur = seekOff + cur;
          timeDisplay.textContent = fmtCtrlTime(absCur);
        }
      } else {
        if (isFinite(dur) && dur > 0) {
          seekPlayed.style.width = ((cur / dur) * 100) + '%';
          seekThumb.style.left = ((cur / dur) * 100) + '%';
          timeDisplay.textContent = fmtCtrlTime(cur) + ' / ' + fmtCtrlTime(dur);
        } else {
          timeDisplay.textContent = fmtCtrlTime(cur);
        }
      }
    }, 250);

    modal.appendChild(playerWrap);

    var statusEl = document.createElement('span');
    statusEl.style.cssText = 'background:rgba(255,255,255,0.15);backdrop-filter:blur(8px);color:#fff;font-size:11px;padding:4px 10px;border-radius:16px;pointer-events:none;white-space:nowrap;';
    statusEl.textContent = 'Idle';
    floatBar.insertBefore(statusEl, closeBtn);

    overlay.appendChild(modal);
    overlay.onclick = function(e) {
      audioMenu.style.display = 'none';
    };
    document.body.appendChild(overlay);

    var streamSrc = dvr ? '/vod/' + dvr.id + '/dash/manifest.mpd' : url;
    var epgDuration = 0;

    var savedVol = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
    videoEl.volume = savedVol;
    videoEl.addEventListener('volumechange', function() { localStorage.setItem('tvproxy_volume', String(videoEl.volume)); });

    if (streamTracks && streamTracks.length > 0) {
      var videoTrack = streamTracks.find(function(t) { return t.category === 'video'; });
      var audioTracks = streamTracks.filter(function(t) { return t.category === 'audio'; });
      probeData = {
        video: videoTrack ? { codec: videoTrack.codec || '', fps: '', bit_rate: '', field_order: '', pix_fmt: '', color_space: '', profile: '' } : null,
        audio_tracks: audioTracks.map(function(t) { return { codec: t.codec || '?', language: t.language || '', channels: t.channels || 0, audio_type: t.audio_type || 0 }; }),
        duration: 0,
        profile: currentProfile
      };
      buildAudioMenu(probeData.audio_tracks);
    }

    var dashPlayer = null;

    function waitForStream() {
      if (!dvr) return Promise.resolve();
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Connecting...';
      return new Promise(function(resolve, reject) {
        var attempts = 0;
        function poll() {
          if (playerCtx.signal.aborted) { reject(new Error('cancelled')); return; }
          fetch('/vod/' + dvr.id + '/status').then(function(r) { return r.json(); }).then(function(st) {
            var bufThreshold = (dvr && dvr.duration > 0) ? 6 : 12;
            if (st.buffered > bufThreshold) { if (st.duration > 0 && !dvr.duration) dvr.duration = st.duration; resolve(); return; }
            if (st.error) { reject(new Error(st.error)); return; }
            attempts++;
            if (attempts >= 60) { reject(new Error('stream timeout')); return; }
            setTimeout(poll, 500);
          }).catch(function() { attempts++; if (attempts >= 60) { reject(new Error('poll failed')); return; } setTimeout(poll, 500); });
        }
        poll();
      });
    }

    if (typeof dashjs !== 'undefined') {
      var epgReady = tvgId ? api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(function(program) {
        if (program && program.start && program.stop) {
          nowProgram = program;
          var remaining = (new Date(program.stop).getTime() - Date.now()) / 1000;
          epgDuration = remaining > 0 ? remaining : 0;
        }
      }).catch(function() {}) : Promise.resolve();

      Promise.all([waitForStream(), epgReady]).then(function() {
        statusEl.style.color = '#ffa726';
        statusEl.textContent = 'Buffering...';
        if (epgDuration > 0) {
          streamSrc += (streamSrc.indexOf('?') >= 0 ? '&' : '?') + 'epg_duration=' + epgDuration;
        }
        return fetch(streamSrc).then(function(r) { return r.text(); }).then(function(mpd) {
          console.log('DASH manifest preflight (' + mpd.length + ' bytes):', mpd.substring(0, 500));
        }).catch(function(e) { console.warn('Manifest preflight failed:', e); });
      }).then(function() {
        var isVOD = (dvr && dvr.duration > 0) || epgDuration > 0;
        dashPlayer = dashjs.MediaPlayer().create();
        window._dashDebug = dashPlayer;
        dashPlayer.updateSettings({
          streaming: {
            timeShiftBuffer: {
              calcFromSegmentTimeline: true,
              fallbackToSegmentTimeline: true
            },
            delay: {
              liveDelay: 4,
              useSuggestedPresentationDelay: false
            },
            liveCatchup: {
              enabled: !isVOD
            },
            buffer: {
              stallThreshold: 0.5,
              bufferTimeDefault: 12
            },
            retryAttempts: {
              MPD: 10,
              MediaSegment: 10,
              InitializationSegment: 10
            },
            retryIntervals: {
              MPD: 2000,
              MediaSegment: 1000,
              InitializationSegment: 1000
            },
            manifestRequestTimeout: 60000,
            fragmentRequestTimeout: 30000
          }
        });
        dashPlayer.on(dashjs.MediaPlayer.events.ERROR, function(e) {
          console.error('DASH ERROR:', e);
          if (e.error && e.error.code === 32) {
            fetch(streamSrc).then(function(r) { return r.text(); }).then(function(mpd) { console.error('MANIFEST AT ERROR:', mpd); });
          }
          statusEl.style.color = '#ff6b6b';
          statusEl.textContent = 'Errored';
          statusEl.style.cursor = 'pointer';
          statusEl.style.pointerEvents = 'auto';
          statusEl.title = e.error ? e.error.message : String(e);
          statusEl.onclick = function() { navigator.clipboard.writeText(JSON.stringify(e.error || e, null, 2)).then(function() { statusEl.textContent = 'Copied!'; setTimeout(function() { statusEl.textContent = 'Errored'; }, 1500); }); };
        });
        dashPlayer.on(dashjs.MediaPlayer.events.STREAM_INITIALIZED, function() {
          var dvrWin = dashPlayer.getDvrWindow();
          var knownDuration = dvr.duration || epgDuration || 0;
          console.log('DASH INIT. dvrWindow:', dvrWin, 'duration:', dashPlayer.duration(), 'known:', knownDuration);
          if (isVOD) {
            dashPlayer.seek(0);
          }
          videoEl.play().catch(function() {});
        });
        dashPlayer.extend('RequestModifier', function() {
          return {
            modifyRequestURL: function(url) {
              if (url.indexOf('manifest.mpd') >= 0 && epgDuration > 0) {
                var remaining = nowProgram && nowProgram.stop ? Math.max(0, (new Date(nowProgram.stop).getTime() - Date.now()) / 1000) : epgDuration;
                url = url.replace(/epg_duration=[^&]*/, 'epg_duration=' + remaining);
                if (url.indexOf('epg_duration') < 0) {
                  url += (url.indexOf('?') >= 0 ? '&' : '?') + 'epg_duration=' + remaining;
                }
              }
              return url;
            },
            modifyRequestHeader: function(xhr) { return xhr; }
          };
        }, true);
        dashPlayer.initialize(videoEl, streamSrc, true);
      }).catch(function(e) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Errored';
        statusEl.title = e.message || String(e);
        statusEl.onclick = function() { navigator.clipboard.writeText(e.message || String(e)).then(function() { statusEl.textContent = 'Copied!'; setTimeout(function() { statusEl.textContent = 'Errored'; }, 1500); }); };
      });
    } else {
      videoEl.src = streamSrc;
      videoEl.play().catch(function() {});
    }


    videoEl.addEventListener('playing', function() {
      retryCount = 0;
      statusEl.style.color = '#4caf50';
      updateStatusText();
      if (channelID) api.del('/api/channels/' + channelID + '/fail').catch(function() {});
    });
    videoEl.addEventListener('waiting', function() {
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Buffering';
    });
    videoEl.addEventListener('error', function() {
      if (channelID) api.post('/api/channels/' + channelID + '/fail').catch(function() {});
      handleRetry();
    });

    function handleRetry() {
      if (playerCtx.signal.aborted) return;
      if (retryCount >= MAX_RETRIES) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Errored ';
        var retryLink = document.createElement('a');
        retryLink.textContent = 'Retry';
        retryLink.href = '#';
        retryLink.style.cssText = 'color:#4fc3f7;cursor:pointer;text-decoration:underline;';
        retryLink.onclick = function(e) { e.preventDefault(); retryCount = 0; restartPlayback(); };
        statusEl.appendChild(retryLink);
        return;
      }
      retryCount++;
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Retrying... (' + retryCount + '/' + MAX_RETRIES + ')';
      if (dvr && channelID) {
        retryTimeout = setTimeout(async function() {
          try {
            await api.del('/vod/' + dvr.id + (dvr.consumer_id ? '?consumer_id=' + dvr.consumer_id : '')).catch(function() {});
            var audioParam = currentAudioIndex > 0 ? '&audio=' + currentAudioIndex : '';
            var resp = await fetch('/channel/' + channelID + '/vod?profile=' + encodeURIComponent(currentProfile) + audioParam, { method: 'POST' }).then(function(r) { return r.json(); });
            if (resp.session_id) {
              dvr = { id: resp.session_id, consumer_id: resp.consumer_id, duration: resp.duration, container: resp.container };
              if (dvrTracker) dvrTracker.reset();
              streamSrc = '/vod/' + dvr.id + '/dash/manifest.mpd';
            }
          } catch(e) {}
          restartPlayback();
        }, 2000);
      } else {
        retryTimeout = setTimeout(restartPlayback, 2000);
      }
    }

    function restartPlayback() {
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (dashPlayer && dvr) {
        statusEl.style.color = '#ffa726';
        statusEl.textContent = 'Reconnecting...';
        waitForStream().then(function() {
          return fetch(streamSrc).then(function() {}).catch(function() {});
        }).then(function() {
          dashPlayer.attachSource(streamSrc);
          videoEl.play().catch(function() {});
        }).catch(function() {
          statusEl.style.color = '#ff6b6b';
          statusEl.textContent = 'Reconnect failed';
        });
      } else if (dashPlayer) {
        dashPlayer.attachSource(streamSrc);
        videoEl.play().catch(function() {});
      } else if (videoEl) {
        videoEl.src = streamSrc;
        videoEl.play().catch(function() {});
      }
    }

    function formatTime(d) {
      return new Date(d).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }

    function buildStatusSuffix() {
      if (!nowProgram) return '';
      var suffix = ' \u2014 ' + nowProgram.title;
      if (nowProgram.start && nowProgram.stop) {
        suffix += ' (' + formatTime(nowProgram.start) + ' - ' + formatTime(nowProgram.stop) + ')';
      }
      return suffix;
    }

    function updateStatusText() {
      var state = isRecording ? 'Recording' : 'Streaming';
      if (isRecording && recordStartTime) {
        var elapsed = Math.floor((Date.now() - recordStartTime) / 1000);
        var m = Math.floor(elapsed / 60);
        var s = elapsed % 60;
        state += ' ' + m + ':' + String(s).padStart(2, '0');
      }
      statusEl.textContent = state + buildStatusSuffix();
    }

    function fetchNowPlaying() {
      if (!tvgId || playerCtx.signal.aborted) return;
      api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(function(program) {
        if (program && program.title) {
          nowProgram = program;
          updateStatusText();
          if (nowPlayingEl) {
            var info = program.title;
            if (program.start && program.stop) {
              info += ' \u2022 ' + formatTime(program.start) + ' - ' + formatTime(program.stop);
            }
            nowPlayingEl.textContent = info;
          }
          if (program.start && program.stop && !dvr.duration) {
            var progDur = (new Date(program.stop).getTime() - Date.now()) / 1000;
            if (progDur > 0) {
              epgDuration = progDur;
              if (dashPlayer) {
                dashPlayer.updateSettings({
                  streaming: {
                    timeShiftBuffer: { calcFromSegmentTimeline: true }
                  }
                });
              }
            }
          }
        }
      }).catch(function() {});
    }


    fetchNowPlaying();
    if (tvgId) {
      progInterval = setInterval(fetchNowPlaying, 60000);
    }

    if (dvr) {
      pollInterval = setInterval(async function() {
        if (playerCtx.signal.aborted) { clearInterval(pollInterval); return; }
        try {
          var resp = await fetch('/vod/' + dvr.id + '/status', { signal: playerCtx.signal });
          if (resp.status === 404) {
            clearInterval(pollInterval);
            statusEl.style.color = '#ff6b6b';
            statusEl.textContent = 'Session ended';
            return;
          }
          if (!resp.ok) { pollFailures++; if (pollFailures >= 3) clearInterval(pollInterval); return; }
          pollFailures = 0;
          var st = await resp.json();
          if (st.error && !st.recording) {
            statusEl.style.color = '#ff6b6b';
            statusEl.textContent = 'Errored';
            statusEl.style.cursor = 'pointer';
            statusEl.style.pointerEvents = 'auto';
            statusEl.title = st.error;
            statusEl.onclick = function() { navigator.clipboard.writeText(st.error).then(function() { statusEl.textContent = 'Copied!'; setTimeout(function() { statusEl.textContent = 'Errored'; }, 1500); }); };
            clearInterval(pollInterval);
            return;
          }
          if (dvrTracker) dvrTracker.updateBuffered(st.buffered);
          if (st.duration > 0 && dvr && !dvr.duration) {
            dvr.duration = st.duration;
          }
          if (isLive && st.duration > 0 && !channelID) {
            isLive = false;
            if (dvrTracker) dvrTracker.setDuration(st.duration);
          }
          if (st.profile && st.profile !== currentProfile) {
            currentProfile = st.profile;
          }
          if (st.recording && !isRecording) {
            startRecordingUI();
          } else if (!st.recording && isRecording) {
            stopRecordingUI();
          }
          if (st.video || st.audio_tracks) {
            probeData = { video: st.video || null, audio_tracks: st.audio_tracks || [], duration: st.duration, profile: st.profile || '' };
            buildAudioMenu(probeData.audio_tracks);
          }
          if (st.output_video_codec) outputCodecs.video = st.output_video_codec;
          if (st.output_audio_codec) outputCodecs.audio = st.output_audio_codec;
          if (st.output_container) outputCodecs.container = st.output_container;
          if (st.output_hwaccel) outputCodecs.hwaccel = st.output_hwaccel;
          if (st.seek_offset != null) dvr._seekOffset = st.seek_offset;
          if (st.stream_url && st.stream_url.startsWith('rtsp://') && !signalInterval) {
            satipStreamUrl = st.stream_url;
            var pollSignal = async function() {
              if (playerCtx.signal.aborted || !satipStreamUrl) return;
              try { signalData = await api.get('/api/satip/signal?url=' + encodeURIComponent(satipStreamUrl)); } catch(e) {}
            };
            pollSignal();
            signalInterval = setInterval(pollSignal, 5000);
          }
        } catch(e) {
          if (e.name === 'AbortError' || playerCtx.signal.aborted) { clearInterval(pollInterval); return; }
          pollFailures++;
          if (pollFailures >= 3) clearInterval(pollInterval);
        }
      }, 2000);
    }

    var outputCodecs = { video: '', audio: '', container: '', hwaccel: '' };
    var lastBufferedTime = 0;
    var lastBufferedTs = 0;
    var transcodeSpeed = 0;

    function updateStats() {
      if (playerCtx.signal.aborted || statsOverlay.style.display === 'none') return;
      var vi = probeData && probeData.video ? probeData.video : null;
      var at = probeData && probeData.audio_tracks ? probeData.audio_tracks : [];
      var activeAudio = at.length > 0 ? at[currentAudioIndex] || at[0] : null;
      var res = videoEl && videoEl.videoWidth ? videoEl.videoWidth + 'x' + videoEl.videoHeight : null;
      var buf = videoEl && videoEl.buffered.length > 0 ? (videoEl.buffered.end(0) - videoEl.currentTime).toFixed(1) + 's' : '0s';

      if (dvrTracker && dvrTracker.buffered > 0) {
        var now = Date.now();
        if (lastBufferedTs > 0) {
          var elapsed = (now - lastBufferedTs) / 1000;
          if (elapsed > 0.5) {
            var mediaDelta = dvrTracker.buffered - lastBufferedTime;
            transcodeSpeed = mediaDelta / elapsed;
            lastBufferedTime = dvrTracker.buffered;
            lastBufferedTs = now;
          }
        } else {
          lastBufferedTime = dvrTracker.buffered;
          lastBufferedTs = Date.now();
        }
      }

      var left = [];
      if (vi || activeAudio) {
        var inputParts = [];
        if (vi) {
          var vIn = esc(vi.codec) + (vi.profile ? ' (' + esc(vi.profile) + ')' : '');
          if (vi.pix_fmt) vIn += ' ' + esc(vi.pix_fmt);
          if (vi.color_space && vi.color_space !== 'unknown') vIn += ' ' + esc(vi.color_space);
          if (vi.field_order && vi.field_order !== 'unknown' && vi.field_order !== 'progressive') vIn += ' ' + esc(vi.field_order);
          inputParts.push(vIn);
        }
        if (activeAudio) {
          var aIn = esc(activeAudio.codec || '?');
          if (activeAudio.language) aIn += ' [' + esc(activeAudio.language) + ']';
          if (activeAudio.channels) aIn += ' ' + activeAudio.channels + 'ch';
          inputParts.push(aIn);
        }
        left.push('In: ' + inputParts.join(' | '));
      }
      if (outputCodecs.video || outputCodecs.audio) {
        var outParts = [];
        if (outputCodecs.video) outParts.push(esc(outputCodecs.video));
        if (outputCodecs.audio) outParts.push(esc(outputCodecs.audio));
        if (outputCodecs.container) outParts.push(esc(outputCodecs.container));
        if (outputCodecs.hwaccel && outputCodecs.hwaccel !== 'none') outParts.push(esc(outputCodecs.hwaccel.toUpperCase()));
        left.push('Out: ' + outParts.join(' | '));
      }
      if (res) left.push(res);
      if (vi) {
        if (vi.fps) left.push(esc(vi.fps) + ' fps');
        if (vi.bit_rate) left.push((parseInt(vi.bit_rate) / 1000).toFixed(0) + ' kbps');
      }
      var playbackParts = ['buf ' + esc(buf)];
      if (transcodeSpeed > 0) playbackParts.push(transcodeSpeed.toFixed(1) + 'x');
      var quality = videoEl && videoEl.getVideoPlaybackQuality ? videoEl.getVideoPlaybackQuality() : null;
      if (quality) {
        var dropColor = quality.droppedVideoFrames > 50 ? '#ff6b6b' : quality.droppedVideoFrames > 0 ? '#ffb300' : '#4caf50';
        playbackParts.push('<span style="color:' + dropColor + '">' + quality.droppedVideoFrames + ' dropped</span>');
      }
      left.push(playbackParts.join(' | '));
      if (probeData && probeData.profile) left.push(esc(probeData.profile));

      var right = [];
      if (signalData) {
        var lockColor = signalData.lock ? '#4caf50' : '#ff6b6b';
        var lvl = signalData.level_pct;
        var qlt = signalData.quality_pct;
        var lvlColor = lvl > 60 ? '#4caf50' : lvl > 30 ? '#ffb300' : '#ff6b6b';
        var qltColor = qlt > 60 ? '#4caf50' : qlt > 30 ? '#ffb300' : '#ff6b6b';
        function sigBar(pct, color) {
          return '<span style="display:inline-block;width:50px;height:5px;background:#333;vertical-align:middle;border-radius:2px">'
            + '<span style="display:block;width:' + pct + '%;height:100%;background:' + color + ';border-radius:2px"></span></span>';
        }
        var tunerLabel = signalData.fe_id ? 'FE' + signalData.fe_id + ' ' : '';
        right.push(esc(tunerLabel) + '<span style="color:' + lockColor + '">' + (signalData.lock ? 'Locked' : 'No Lock') + '</span>');
        right.push('Lvl ' + sigBar(lvl, lvlColor) + ' <span style="color:' + lvlColor + '">' + lvl + '%</span>');
        right.push('Qlt ' + sigBar(qlt, qltColor) + ' <span style="color:' + qltColor + '">' + qlt + '%</span>');
        if (signalData.ber != null) right.push('BER ' + signalData.ber);
        if (signalData.freq_mhz) right.push(signalData.freq_mhz + ' MHz ' + esc((signalData.msys || '').toUpperCase()));
        if (signalData.bitrate_kbps) right.push((signalData.bitrate_kbps / 1000).toFixed(1) + ' Mbps');
      }

      var html = '<div style="display:flex;gap:24px;">';
      html += '<div>' + left.join('<br>') + '</div>';
      if (right.length) html += '<div>' + right.join('<br>') + '</div>';
      html += '</div>';
      statsOverlay.innerHTML = html;
    }

    statsInterval = setInterval(updateStats, 2000);

  }
  async function findOrCreateLogoByUrl(url, name, inputs) {
    const logos = await logosCache.getAll();
    const found = logos.find(l => l.url === url);
    if (found) {
      inputs.logo_id._setLogo(found);
      return;
    }
    try {
      const newLogo = await api.post('/api/logos', { name: name || 'Logo', url: url });
      logosCache.invalidate();
      inputs.logo_id._setLogo(newLogo);
    } catch {
    }
  }

  async function quickAddChannel(streamId, streamName, tvgId, logoUrl) {
    const nameInp = h('input', { type: 'text', value: streamName });
    const groupSelect = h('select');
    groupSelect.appendChild(h('option', { value: '' }, '-- No Group --'));

    try {
      const groups = await channelGroupsCache.getAll();
      groups.forEach(g => {
        groupSelect.appendChild(h('option', { value: g.id }, g.name));
      });
    } catch {}

    const epgData = await epgCache.getAll().catch(() => []);

    let matchedEpg = null;
    if (tvgId) {
      matchedEpg = epgData.find(e => e.channel_id === tvgId) || null;
    }
    if (!matchedEpg) {
      const nameLc = streamName.toLowerCase();
      matchedEpg = epgData.find(e => (e.name || '').toLowerCase() === nameLc) || null;
    }
    if (!matchedEpg) {
      const stripSuffix = s => s.toLowerCase().replace(/[\s_-]*(hd|sd|fhd|uhd|\+1)$/i, '').trim();
      const norm = stripSuffix(streamName);
      const isHD = /[\s_-]hd$/i.test(streamName);
      let fallback = null;
      for (const e of epgData) {
        if (stripSuffix(e.name || '') === norm) {
          const eHD = /[\s_-]hd$/i.test(e.name || '');
          if (eHD === isHD) { matchedEpg = e; break; }
          if (!fallback) fallback = e;
        }
      }
      if (!matchedEpg) matchedEpg = fallback;
    }

    let selectedEpg = matchedEpg;
    const acWrapper = h('div', { className: 'autocomplete-wrapper' });
    const tvgInp = h('input', {
      type: 'text',
      placeholder: 'Search EPG channels...',
      value: matchedEpg ? (matchedEpg._display_name || matchedEpg.name) : (tvgId || ''),
    });
    tvgInp._selectedValue = matchedEpg ? matchedEpg.channel_id : (tvgId || '');
    const acDropdown = h('div', { className: 'autocomplete-dropdown' });
    acDropdown.style.display = 'none';

    function renderAcDropdown(query) {
      acDropdown.innerHTML = '';
      const matches = epgCache.search(query, 50);
      if (!matches.length) { acDropdown.style.display = 'none'; return; }
      let selIdx = -1;
      matches.forEach((opt, i) => {
        const row = h('div', { className: 'autocomplete-item' });
        if (opt.icon) row.appendChild(h('img', { src: opt.icon, className: 'autocomplete-icon' }));
        const text = h('div', { className: 'autocomplete-text' });
        text.appendChild(h('div', { className: 'autocomplete-name' }, opt._display_name || opt.name || ''));
        text.appendChild(h('div', { className: 'autocomplete-id' }, opt.channel_id || ''));
        row.appendChild(text);
        row.addEventListener('click', () => {
          selectedEpg = opt;
          tvgInp.value = opt._display_name || opt.name || '';
          tvgInp._selectedValue = opt.channel_id || '';
          acDropdown.style.display = 'none';
        });
        acDropdown.appendChild(row);
      });
      acDropdown.style.display = 'block';
    }

    tvgInp.addEventListener('input', () => {
      selectedEpg = null;
      tvgInp._selectedValue = '';
      renderAcDropdown(tvgInp.value);
    });
    tvgInp.addEventListener('focus', () => { if (tvgInp.value) renderAcDropdown(tvgInp.value); });
    tvgInp.addEventListener('keydown', e => {
      const items = acDropdown.querySelectorAll('.autocomplete-item');
      if (!items.length) return;
      let selIdx = Array.from(items).findIndex(el => el.classList.contains('selected'));
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        selIdx = Math.min(selIdx + 1, items.length - 1);
        items.forEach((el, i) => el.classList.toggle('selected', i === selIdx));
        items[selIdx].scrollIntoView({ block: 'nearest' });
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        selIdx = Math.max(selIdx - 1, 0);
        items.forEach((el, i) => el.classList.toggle('selected', i === selIdx));
        items[selIdx].scrollIntoView({ block: 'nearest' });
      } else if (e.key === 'Enter' && selIdx >= 0) {
        e.preventDefault(); items[selIdx].click();
      } else if (e.key === 'Escape') {
        acDropdown.style.display = 'none';
      }
    });
    setTimeout(() => {
      document.addEventListener('click', function acClose(e) {
        if (!acWrapper.contains(e.target)) acDropdown.style.display = 'none';
      });
    }, 0);

    const statusEl = matchedEpg
      ? h('small', { style: 'color:var(--success)' }, 'Auto-matched: ' + (matchedEpg._display_name || matchedEpg.name))
      : h('small', { style: 'color:var(--text-muted)' }, tvgId ? 'No EPG match — using stream tvg-id' : 'No EPG match found');

    acWrapper.appendChild(tvgInp);
    acWrapper.appendChild(acDropdown);

    const bodyEl = h('div', null,
      h('div', { className: 'form-group' }, h('label', null, 'Channel Name'), nameInp),
      h('div', { className: 'form-group' }, h('label', null, 'EPG Channel ID'), acWrapper, statusEl),
      h('div', { className: 'form-group' }, h('label', null, 'Channel Group'), groupSelect),
    );

    showModal('Quick Add Channel', bodyEl, async () => {
      if (!nameInp.value.trim()) throw new Error('Channel name is required');

      const resolvedTvgId = tvgInp._selectedValue || tvgInp.value.trim();
      const channelData = {
        name: nameInp.value.trim(),
        tvg_id: resolvedTvgId,
        channel_group_id: groupSelect.value || null,
        is_enabled: true,
      };

      const icon = logoUrl || (selectedEpg && selectedEpg.icon) || '';
      if (icon) channelData.logo = icon;

      const channel = await api.post('/api/channels', channelData);
      await api.post('/api/channels/' + channel.id + '/streams', { stream_ids: [streamId] });
      channelsCache.invalidate();
      toast.success('Channel "' + channel.name + '" created');
    }, 'Add Channel');
  }

  const pages = {
    dashboard: renderDashboard,

    'm3u-accounts': buildCrudPage({
      title: 'M3U Accounts',
      singular: 'M3U Account',
      apiPath: '/api/m3u/accounts',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name', render: item => {
          const wrap = h('span', null, item.name);
          if (item.last_error) wrap.appendChild(h('div', { style: 'color:var(--danger);font-size:0.85em;margin-top:2px' }, item.last_error));
          return wrap;
        }},
        { key: 'type', label: 'Type', render: item => h('span', { className: 'badge badge-info' }, item.type || 'm3u') },
        { key: 'url', label: 'URL', render: item => {
          const url = item.url || '';
          return url.length > 50 ? url.substring(0, 50) + '...' : url;
        }},
        { key: 'max_streams', label: 'Max Streams' },
        { key: 'stream_count', label: 'Streams' },
      ],
      fields: [
        { key: 'name', label: 'Name', placeholder: 'My IPTV Provider' },
        { key: 'type', label: 'Type', type: 'select', options: [
          { value: 'm3u', label: 'M3U URL' },
          { value: 'xtream', label: 'Xtream Codes' },
        ]},
        { key: 'url', label: 'URL', placeholder: 'http://provider.com/get.php?username=...', help: 'M3U URL or Xtream Codes server URL' },
        { key: 'username', label: 'Username (Xtream)', placeholder: 'Optional for Xtream' },
        { key: 'password', label: 'Password (Xtream)', type: 'password', placeholder: 'Optional for Xtream' },
        { key: 'max_streams', label: 'Max Concurrent Streams', type: 'number', default: 1 },
      ],
      rowActions: (item, reload) => [
        {
          label: 'Refresh',
          icon: '\u21BB',
          handler: async () => {
            try {
              await api.post('/api/m3u/accounts/' + item.id + '/refresh');
              streamsCache.invalidate();
              for (var k in streamGroupsCache) delete streamGroupsCache[k];
              rebuildStreamNav();
              toast.success('Refresh started for ' + item.name);
              var pollCount = 0;
              var pollTimer = setInterval(async () => {
                try {
                  var status = await api.get('/api/m3u/accounts/' + item.id + '/status');
                  if (status.state === 'done' || status.state === 'error') {
                    clearInterval(pollTimer);
                    reload();
                    if (status.state === 'done') toast.success(item.name + ': ' + status.message);
                    else toast.error(item.name + ': ' + status.message);
                  }
                } catch (e) {}
                if (++pollCount > 60) clearInterval(pollTimer);
              }, 2000);
            } catch (err) {
              toast.error(err.message);
            }
          },
        },
      ],
    }),

    'satip-sources': buildCrudPage({
      title: 'SAT>IP Sources',
      singular: 'SAT>IP Source',
      apiPath: '/api/satip/sources',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name', render: item => {
          const wrap = h('span', null, item.name);
          if (item.last_error) wrap.appendChild(h('div', { style: 'color:var(--danger);font-size:0.85em;margin-top:2px' }, item.last_error));
          return wrap;
        }},
        { key: 'host', label: 'Host' },
        { key: 'transmitter_file', label: 'Transmitter', render: item => item.transmitter_file ? item.transmitter_file.split('/').pop() : '—' },
        { key: 'is_enabled', label: 'Enabled', render: item => item.is_enabled ? '\u2714' : '\u2718' },
        { key: 'stream_count', label: 'Streams' },
        { key: 'last_scanned', label: 'Last Scanned', render: item => item.last_scanned ? new Date(item.last_scanned).toLocaleString() : 'Never' },
      ],
      fields: [
        { key: 'name', label: 'Name', placeholder: 'Home SAT>IP Server' },
        { key: 'host', label: 'Host / IP Address', placeholder: '192.168.1.100' },
        { key: 'http_port', label: 'HTTP Port', type: 'number', default: 8875, help: 'SAT>IP HTTP port (default 8875)' },
        { key: 'is_enabled', label: 'Enabled', type: 'checkbox', default: true },
        {
          key: '_delivery_system',
          label: 'Delivery System',
          type: 'select',
          exclude: true,
          options: [
            { value: '', label: '— select —' },
            { value: 'dvb-t', label: 'DVB-T / DVB-T2 (Terrestrial)' },
            { value: 'dvb-s', label: 'DVB-S / DVB-S2 (Satellite)' },
            { value: 'dvb-c', label: 'DVB-C (Cable)' },
          ],
        },
        {
          key: 'transmitter_file',
          label: 'Transmitter',
          type: 'select',
          options: [{ value: '', label: '— select delivery system first —' }],
        },
      ],
      postFormSetup: (inputs, isEdit, item) => {
        var sysSel = inputs['_delivery_system'];
        var txSel = inputs['transmitter_file'];
        async function loadTransmitters(system, selectValue) {
          txSel.innerHTML = '';
          if (!system) {
            txSel.appendChild(h('option', { value: '' }, '— select delivery system first —'));
            return;
          }
          txSel.appendChild(h('option', { value: '' }, 'Loading...'));
          try {
            var list = await api.get('/api/satip/transmitters?system=' + system);
            txSel.innerHTML = '';
            txSel.appendChild(h('option', { value: '' }, '— select transmitter —'));
            list.forEach(function(t) {
              var opt = h('option', { value: t.file }, t.name);
              if (selectValue && t.file === selectValue) opt.selected = true;
              txSel.appendChild(opt);
            });
          } catch(e) {
            txSel.innerHTML = '';
            txSel.appendChild(h('option', { value: '' }, '— failed to load —'));
          }
        }
        if (isEdit && item.transmitter_file) {
          var prefix = item.transmitter_file.split('/')[0];
          sysSel.value = prefix;
          loadTransmitters(prefix, item.transmitter_file);
        }
        sysSel.addEventListener('change', function() {
          loadTransmitters(sysSel.value, null);
        });
      },
      rowActions: (item, reload) => [
        {
          label: 'Scan',
          icon: '\u21BB',
          handler: async (e) => {
            const scanBtn = e.currentTarget;
            const cell = scanBtn.parentElement;
            try {
              await api.post('/api/satip/sources/' + item.id + '/scan');
              scanBtn.disabled = true;
              scanBtn.style.display = 'none';
              const progressWrap = document.createElement('div');
              progressWrap.style.cssText = 'display:flex;flex-direction:column;gap:2px;min-width:80px;';
              const progressBar = document.createElement('div');
              progressBar.style.cssText = 'height:6px;background:var(--bg-hover);border-radius:3px;overflow:hidden;';
              const progressFill = document.createElement('div');
              progressFill.style.cssText = 'height:100%;width:0%;background:var(--accent);border-radius:3px;transition:width 0.3s;';
              progressBar.appendChild(progressFill);
              const progressLabel = document.createElement('div');
              progressLabel.style.cssText = 'font-size:10px;color:var(--text-muted);text-align:center;';
              progressLabel.textContent = 'Starting...';
              progressWrap.appendChild(progressBar);
              progressWrap.appendChild(progressLabel);
              cell.appendChild(progressWrap);
              var pollCount = 0;
              var pollTimer = setInterval(async () => {
                try {
                  var status = await api.get('/api/satip/sources/' + item.id + '/status');
                  if (status.total > 0) {
                    var pct = Math.round((status.progress / status.total) * 100);
                    progressFill.style.width = pct + '%';
                    progressLabel.textContent = status.progress + '/' + status.total + ' (' + pct + '%)';
                  } else {
                    progressLabel.textContent = status.message || 'Scanning...';
                  }
                  if (status.state === 'done' || status.state === 'error') {
                    clearInterval(pollTimer);
                    progressWrap.remove();
                    scanBtn.disabled = false;
                    scanBtn.style.display = '';
                    reload();
                    if (status.state === 'done') toast.success(item.name + ': ' + status.message);
                    else toast.error(item.name + ': ' + status.message);
                  }
                } catch (e) {}
                if (++pollCount > 120) clearInterval(pollTimer);
              }, 1000);
            } catch (err) {
              toast.error(err.message);
            }
          },
        },
        {
          label: 'Clear',
          icon: '\u{1F5D1}',
          handler: async () => {
            if (!confirm('Delete all streams for ' + item.name + '?')) return;
            try {
              await api.post('/api/satip/sources/' + item.id + '/clear');
              toast.success('Streams cleared for ' + item.name);
              reload();
            } catch (err) {
              toast.error(err.message);
            }
          },
        },
      ],
    }),

    channels: buildCrudPage({
      title: 'Channels',
      singular: 'Channel',
      apiPath: '/api/channels',
      cache: channelsCache,
      create: true,
      update: true,
      onChange: () => channelsCache.invalidate(),
      groupBy: {
        key: 'channel_group_id',
        loadGroups: () => channelGroupsCache.getAll(),
        nameKey: 'name',
        sortKey: 'sort_order',
        ungroupedLabel: 'Ungrouped',
        apiPath: '/api/channel-groups',
        singular: 'Channel Group',
        plural: 'Channel Groups',
        onChanged: () => channelGroupsCache.invalidate(),
        fields: [
          { key: 'name', label: 'Group Name', placeholder: 'Entertainment' },
          { key: 'sort_order', label: 'Sort Order', type: 'number', default: 0 },
          { key: 'jellyfin_enabled', label: 'Show in Jellyfin', type: 'checkbox', default: false },
          { key: 'jellyfin_type', label: 'Jellyfin Style', type: 'select', default: 'folders', options: [
            { value: 'folders', label: 'Channels (Grid)' },
            { value: 'livetv', label: 'Live TV (Guide)' },
            { value: 'movies', label: 'Movies' },
            { value: 'tvshows', label: 'TV Shows' },
            { value: 'music', label: 'Music' },
            { value: 'musicvideos', label: 'Music Videos' },
            { value: 'playlists', label: 'Playlists' },
            { value: 'homevideos', label: 'Home Videos' },
          ]},
        ],
      },
      columns: [
        { key: '_fav', label: '\u2B50', thStyle: 'width:36px;text-align:center', tdStyle: 'text-align:center', render: item => favoriteButton(item.id) },
        { key: 'logo', label: '', thStyle: 'width:110px;padding-right:0;text-align:center', tdStyle: 'padding-right:0;text-align:center', render: item => {
          var openModal = function() {
            var p = item._now_program;
            var pStart = p && p.start ? new Date(p.start).getTime() : 0;
            var pStop = p && p.stop ? new Date(p.stop).getTime() : 0;
            var nowMs = Date.now();
            showProgrammeModal({
              title: (p && p.title) || item._now_playing || item.name,
              time: p ? (new Date(p.start).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'}) + ' - ' + new Date(p.stop).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'})) : '',
              description: (p && p.description) || '',
              channelName: item.name,
              channelID: item.id,
              tvgId: item.tvg_id,
              channelLogo: item.logo || '',
              isLive: pStart > 0 && pStart <= nowMs && pStop > nowMs,
              isFuture: pStart > nowMs,
              start: (p && p.start) || '',
              stop: (p && p.stop) || '',
            });
          };
          if (item.logo) {
            var img = h('img', { src: item.logo, style: 'max-width:100px;max-height:40px;object-fit:contain;border-radius:2px;vertical-align:middle;cursor:pointer;' });
            img.onclick = function(e) {
              e.stopPropagation();
              play({ channelID: item.id, name: item.name, tvgId: item.tvg_id || undefined });
            };
            return img;
          }
          var icon = h('span', { style: 'cursor:pointer;font-size:24px;' }, '\u25B6');
          icon.onclick = function(e) {
            e.stopPropagation();
            play({ channelID: item.id, name: item.name, tvgId: item.tvg_id || undefined });
          };
          return icon;
        }},
        { key: 'name', label: 'Channel', render: item => {
          var wrap = h('div', { style: 'display:flex;flex-direction:column;gap:2px;cursor:pointer', title: item.name, onClick: function(e) {
            e.stopPropagation();
            var p = item._now_program;
            var pStart = p && p.start ? new Date(p.start).getTime() : 0;
            var pStop = p && p.stop ? new Date(p.stop).getTime() : 0;
            var nowMs = Date.now();
            showProgrammeModal({
              title: (p && p.title) || item._now_playing || item.name,
              time: p ? (new Date(p.start).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'}) + ' - ' + new Date(p.stop).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'})) : '',
              description: (p && p.description) || '',
              channelName: item.name,
              channelID: item.id,
              tvgId: item.tvg_id,
              channelLogo: item.logo || '',
              isLive: pStart > 0 && pStart <= nowMs && pStop > nowMs,
              isFuture: pStart > nowMs,
              start: (p && p.start) || '',
              stop: (p && p.stop) || '',
            });
          }});
          var nameEl = h('span', { style: 'font-weight:600' }, item.name);
          if (item.fail_count > 0) nameEl.appendChild(h('span', { className: 'play-fail-badge' }, '! ' + item.fail_count));
          if (!item.is_enabled) nameEl.appendChild(h('span', { className: 'badge badge-danger', style: 'margin-left:6px;font-size:10px' }, 'Off'));
          wrap.appendChild(nameEl);
          if (item._now_playing) {
            wrap.appendChild(h('span', { style: 'font-size:12px;color:var(--text-muted)' }, item._now_playing));
          }
          return wrap;
        }},
        { key: '_now_playing', label: 'Activity', tdStyle: 'font-weight:normal;color:var(--text-secondary);font-size:13px;display:none', thStyle: 'display:none', render: item => {
          if (!item._now_playing) return h('span', { style: 'color:var(--text-muted)' }, '-');
          var span = h('span', { style: 'cursor:pointer;', onClick: function(e) {
            e.stopPropagation();
            if (!item._now_program) return;
            var p = item._now_program;
            var pStart = p.start ? new Date(p.start).getTime() : 0;
            var pStop = p.stop ? new Date(p.stop).getTime() : 0;
            var nowMs = Date.now();
            showProgrammeModal({
              title: p.title || item._now_playing,
              time: (p.start ? new Date(p.start).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'}) : '') + (p.stop ? ' - ' + new Date(p.stop).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'}) : ''),
              description: p.description || '',
              channelName: item.name,
              channelID: item.id,
              tvgId: item.tvg_id,
              channelLogo: item.logo || '',
              isLive: pStart <= nowMs && pStop > nowMs,
              isFuture: pStart > nowMs,
              start: p.start || '',
              stop: p.stop || '',
            });
          }}, item._now_playing);
          return span;
        }},
      ],
      delete: false,
      fields: [
        { key: 'name', label: 'Channel Name', placeholder: 'BBC One' },
        {
          key: 'tvg_id', label: 'EPG Channel ID', type: 'autocomplete',
          placeholder: 'Search EPG channels...',
          help: 'Type to search EPG channels. Auto-matches when you enter a channel name above.',
          cache: epgCache,
          valueKey: 'channel_id',
          displayValueKey: '_display_name',
          displayKey: '_display_name',
          secondaryKey: 'channel_id',
          onSelect: (epg, inputs) => {
            if (epg.icon && inputs.logo_id && !inputs.logo_id._selectedLogoId) {
              findOrCreateLogoByUrl(epg.icon, epg.name, inputs);
            }
          },
        },
        {
          key: 'logo_id', label: 'Logo', type: 'logo-picker',
          cache: logosCache,
        },
        {
          key: 'channel_group_id', label: 'Channel Group', type: 'async-select',
          emptyLabel: '-- No Group --',
          loadOptions: () => channelGroupsCache.getAll(),
          valueKey: 'id', displayKey: 'name',
          help: 'Organize channels into groups (e.g., Sports, Entertainment)',
          createNew: {
            label: 'New Channel Group',
            fields: [
              { key: 'name', label: 'Group Name', placeholder: 'Entertainment' },
              { key: 'sort_order', label: 'Sort Order', type: 'number', default: 0 },
            ],
            apiPath: '/api/channel-groups',
            onCreated: () => channelGroupsCache.invalidate(),
          },
        },
        {
          key: 'stream_profile_id', label: 'Stream Profile Override', type: 'async-select',
          emptyLabel: '-- Auto (Client Detection) --',
          loadOptions: () => streamProfilesCache.getAll(),
          valueKey: 'id', displayKey: 'name',
          help: 'Override the stream profile for this channel. Leave empty to use automatic client detection.',
        },
        {
          key: '_stream', label: 'Stream', type: 'autocomplete',
          placeholder: 'Search streams...',
          help: 'Search and select a stream source for this channel.',
          cache: streamsCache,
          valueKey: '_display_name',
          displayKey: '_display_name',
          secondaryKey: 'group',
          exclude: true,
          onSelect: (stream, inputs) => {
            inputs._stream._selectedStreamId = stream.id;
          },
        },
        { key: 'is_enabled', label: 'Enabled', type: 'checkbox', default: true },
      ],
      postFormSetup: (inputs, isEdit, item) => {
        if (isEdit && inputs._stream && item.id) {
          Promise.all([
            api.get('/api/channels/' + item.id + '/streams'),
            api.get('/api/m3u/accounts').catch(() => []),
            api.get('/api/satip/sources').catch(() => []),
          ]).then(([streams, accounts, satipSources]) => {
            if (streams && streams.length > 0) {
              const nameMap = {};
              accounts.forEach(a => { nameMap[a.id] = a.name; });
              const satipMap = {};
              satipSources.forEach(s => { satipMap[s.id] = s.name; });
              const s = streams[0];
              const prefix = s.satip_source_id ? (satipMap[s.satip_source_id] || 'SAT>IP') : (nameMap[s.m3u_account_id] || 'Unknown');
              inputs._stream.value = prefix + '/' + s.name;
              inputs._stream._selectedStreamId = s.id;
            }
          }).catch(() => {});
        }
        if (inputs.name) {
          inputs.name.addEventListener('blur', async () => {
            const nameVal = inputs.name.value.trim();
            if (!nameVal) return;
            if (inputs.tvg_id && inputs.tvg_id.value) return;

            const epgData = await epgCache.getAll();
            if (!epgData.length) return;

            const normalized = nameVal.toLowerCase().replace(/\s*(hd|sd|fhd|uhd|\+1|_hd|_sd)\s*$/i, '').trim();

            let bestMatch = null;
            let bestScore = 0;
            for (let i = 0; i < epgData.length; i++) {
              const epgName = (epgData[i].name || '').toLowerCase();
              const epgNorm = epgName.replace(/\s*(hd|sd|fhd|uhd|\+1|_hd|_sd)\s*$/i, '').trim();
              let score = 0;
              if (epgNorm === normalized) score = 100;
              else if (epgName === nameVal.toLowerCase()) score = 95;
              else if (epgNorm.startsWith(normalized) && epgNorm.length - normalized.length < 5) score = 75;
              else if (normalized.startsWith(epgNorm) && normalized.length - epgNorm.length < 5) score = 70;
              if (score > bestScore) {
                bestScore = score;
                bestMatch = epgData[i];
              }
            }

            if (bestMatch && bestScore >= 70) {
              inputs.tvg_id.value = bestMatch._display_name || bestMatch.name;
              inputs.tvg_id._selectedValue = bestMatch.channel_id;
              if (bestMatch.icon && inputs.logo_id && !inputs.logo_id._selectedLogoId) {
                findOrCreateLogoByUrl(bestMatch.icon, bestMatch.name, inputs);
              }
              toast.info('Auto-matched EPG: ' + (bestMatch._display_name || bestMatch.name) + ' (' + bestMatch.channel_id + ')');
            }
          });
        }
      },
      postSave: async (result, inputs, isEdit, original) => {
        const streamInput = inputs._stream;
        if (streamInput && streamInput._selectedStreamId) {
          const channelId = isEdit ? original.id : result.id;
          try {
            await api.post('/api/channels/' + channelId + '/streams', {
              stream_ids: [streamInput._selectedStreamId],
            });
          } catch (err) {
            toast.error('Channel saved but stream assignment failed: ' + err.message);
          }
        }
      },
    }),

    'epg-guide': buildEpgGuidePage(),

    'favorites': async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading favorites...'));

      try {
        var favs = await api.get('/api/favorites');
        var channels = await api.get('/api/channels');
        container.innerHTML = '';

        var channelMap = {};
        channels.forEach(function(ch) { channelMap[ch.id] = ch; });

        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:24px' });
        header.appendChild(h('h2', { style: 'margin:0' }, 'Favorites'));
        container.appendChild(header);

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:16px;' });

        var favIds = (favs || []).map(function(f) { return typeof f === 'string' ? f : (f.channel_id || f.id || f); });

        favIds.forEach(function(fid) {
          var ch = channelMap[fid];
          var card = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:12px;padding:20px;transition:transform 0.15s;' });
          card.onmouseenter = function() { card.style.transform = 'scale(1.01)'; };
          card.onmouseleave = function() { card.style.transform = ''; };

          var top = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:12px' });
          var left = h('div', { style: 'display:flex;align-items:center;gap:12px' });

          if (ch && ch.logo) {
            left.appendChild(h('img', { src: ch.logo, style: 'width:40px;height:40px;object-fit:contain;border-radius:6px;background:var(--bg-elevated)', onerror: function() { this.style.display = 'none'; } }));
          }

          left.appendChild(h('div', { style: 'font-size:18px;font-weight:700;color:var(--text-primary)' }, ch ? ch.name : ('Unknown (' + fid + ')')));
          top.appendChild(left);

          var delBtn = h('button', { className: 'btn btn-danger btn-sm', title: 'Remove from favorites' }, '\u2715');
          delBtn.onclick = async function() {
            await api.del('/api/favorites/' + fid);
            toast.success('Removed from favorites');
            pages['favorites'](container);
          };
          top.appendChild(delBtn);
          card.appendChild(top);

          if (ch) {
            var info = h('div', { style: 'display:flex;gap:12px' });
            if (ch.channel_group_name) info.appendChild(h('span', { style: 'font-size:13px;color:var(--text-muted)' }, ch.channel_group_name));
            card.appendChild(info);
          }

          grid.appendChild(card);
        });

        if (favIds.length === 0) {
          grid.appendChild(h('div', { style: 'grid-column:1/-1;text-align:center;padding:48px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\u2B50'),
            h('p', { style: 'font-size:1.1em' }, 'No favorites yet')
          ));
        }

        container.appendChild(grid);
      } catch(err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color:var(--danger)' }, 'Failed to load: ' + err.message));
      }
    },

    'channel-groups': async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading channel groups...'));

      try {
        var groups = await api.get('/api/channel-groups');
        var channels = await api.get('/api/channels');
        container.innerHTML = '';

        var chCountMap = {};
        channels.forEach(function(ch) {
          var gid = ch.channel_group_id || 'none';
          chCountMap[gid] = (chCountMap[gid] || 0) + 1;
        });

        var jellyfinStyles = {
          folders: 'Channels (Grid)', movies: 'Movies', tvshows: 'TV Shows'
        };

        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:24px' });
        header.appendChild(h('h2', { style: 'margin:0' }, 'Channel Groups'));
        var addBtn = h('button', { className: 'btn btn-primary btn-sm', style: 'display:flex;align-items:center;gap:6px' }, '+ New Group');
        addBtn.onclick = function() { editGroup(null); };
        header.appendChild(addBtn);
        container.appendChild(header);

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:16px;' });

        function editGroup(group) {
          var isEdit = group !== null;
          var formEl = h('div');

          var nameLabel = h('label', null, 'Group Name');
          var nameInput = h('input', { type: 'text', placeholder: 'Entertainment', value: isEdit ? group.name : '' });
          formEl.appendChild(h('div', { className: 'form-group' }, nameLabel, nameInput));

          var imageUrlLabel = h('label', null, 'Image URL');
          var imageUrlInput = h('input', { type: 'text', placeholder: 'https://example.com/poster.jpg', value: isEdit ? (group.image_url || '') : '' });
          formEl.appendChild(h('div', { className: 'form-group' }, imageUrlLabel, imageUrlInput));

          var enabledCheck = h('input', { type: 'checkbox', id: 'grp-enabled' });
          enabledCheck.checked = isEdit ? !!group.is_enabled : true;
          formEl.appendChild(h('div', { className: 'form-check', style: 'display:flex;align-items:center;gap:6px;margin:8px 0' }, enabledCheck, h('label', { for: 'grp-enabled', style: 'cursor:pointer;margin:0' }, 'Enabled')));

          var orderLabel = h('label', null, 'Sort Order');
          var orderInput = h('input', { type: 'number', value: isEdit ? String(group.sort_order || 0) : '0' });
          formEl.appendChild(h('div', { className: 'form-group' }, orderLabel, orderInput));

          formEl.appendChild(h('div', { style: 'margin:16px 0 8px;padding:8px 0;border-top:1px solid var(--border);font-weight:600;font-size:14px;color:var(--accent)' }, 'Jellyfin Integration'));

          var jfCheck = h('input', { type: 'checkbox', id: 'jf-enabled' });
          jfCheck.checked = isEdit ? !!group.jellyfin_enabled : false;
          formEl.appendChild(h('div', { className: 'form-check', style: 'display:flex;align-items:center;gap:6px;margin:8px 0' }, jfCheck, h('label', { for: 'jf-enabled', style: 'cursor:pointer;margin:0' }, 'Show in Jellyfin')));

          var styleLabel = h('label', null, 'Presentation Style');
          var styleSelect = h('select', null, ...Object.entries(jellyfinStyles).map(function(entry) {
            var opt = h('option', { value: entry[0] }, entry[1]);
            if (isEdit && group.jellyfin_type === entry[0]) opt.selected = true;
            if (!isEdit && entry[0] === 'folders') opt.selected = true;
            return opt;
          }));
          formEl.appendChild(h('div', { className: 'form-group' }, styleLabel, styleSelect));

          showModal((isEdit ? 'Edit' : 'New') + ' Channel Group', formEl, async function() {
            var body = {
              name: nameInput.value,
              image_url: imageUrlInput.value,
              is_enabled: enabledCheck.checked,
              sort_order: Number(orderInput.value),
              jellyfin_enabled: jfCheck.checked,
              jellyfin_type: styleSelect.value,
            };
            if (isEdit) {
              await api.put('/api/channel-groups/' + group.id, body);
              toast.success('Group updated');
            } else {
              await api.post('/api/channel-groups', body);
              toast.success('Group created');
            }
            channelGroupsCache.invalidate();
            pages['channel-groups'](container);
          });
        }

        function renderGroup(group) {
          var count = chCountMap[group.id] || 0;
          var card = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:12px;padding:20px;transition:transform 0.15s;' + (group.is_enabled ? '' : 'opacity:0.5;') });
          card.onmouseenter = function() { card.style.transform = 'scale(1.01)'; };
          card.onmouseleave = function() { card.style.transform = ''; };

          var top = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:12px' });
          top.appendChild(h('div', { style: 'font-size:18px;font-weight:700;color:var(--text-primary)' }, group.name));
          var actions = h('div', { style: 'display:flex;gap:6px' });
          var editBtn = h('button', { className: 'btn btn-secondary btn-sm', title: 'Edit' }, '\u270E');
          editBtn.onclick = function() { editGroup(group); };
          actions.appendChild(editBtn);
          var delBtn = h('button', { className: 'btn btn-danger btn-sm', title: 'Delete' }, '\u2715');
          delBtn.onclick = async function() {
            if (await confirmDialog('Delete group "' + group.name + '"?')) {
              await api.del('/api/channel-groups/' + group.id);
              toast.success('Group deleted');
              channelGroupsCache.invalidate();
              pages['channel-groups'](container);
            }
          };
          actions.appendChild(delBtn);
          top.appendChild(actions);
          card.appendChild(top);

          var stats = h('div', { style: 'display:flex;gap:12px;margin-bottom:12px' });
          stats.appendChild(h('span', { style: 'font-size:13px;color:var(--text-muted)' }, count + ' channel' + (count !== 1 ? 's' : '')));
          if (group.sort_order > 0) stats.appendChild(h('span', { style: 'font-size:13px;color:var(--text-muted)' }, 'Order: ' + group.sort_order));
          card.appendChild(stats);

          if (group.jellyfin_enabled) {
            var jfBadge = h('div', { style: 'display:flex;align-items:center;gap:8px;padding:8px 12px;background:rgba(59,130,246,0.1);border:1px solid rgba(59,130,246,0.2);border-radius:8px;' });
            jfBadge.appendChild(h('span', { style: 'font-size:12px;font-weight:600;color:#3b82f6' }, 'Jellyfin'));
            jfBadge.appendChild(h('span', { style: 'font-size:12px;color:var(--text-muted)' }, jellyfinStyles[group.jellyfin_type] || 'Channels'));
            card.appendChild(jfBadge);
          }

          grid.appendChild(card);
        }

        groups.sort(function(a, b) { return (a.sort_order || 0) - (b.sort_order || 0) || a.name.localeCompare(b.name); });
        groups.forEach(renderGroup);

        if (groups.length === 0) {
          grid.appendChild(h('div', { style: 'grid-column:1/-1;text-align:center;padding:48px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\ud83d\udcc2'),
            h('p', { style: 'font-size:1.1em' }, 'No channel groups yet. Create one to organise your channels.')
          ));
        }

        container.appendChild(grid);
      } catch(err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color:var(--danger)' }, 'Failed to load: ' + err.message));
      }
    },

    'epg-sources': function(container) {
      const isAdmin = state.user && state.user.is_admin;
      return buildCrudPage({
        title: 'EPG Sources',
        singular: 'EPG Source',
        apiPath: '/api/epg/sources',
        create: isAdmin,
        update: isAdmin,
        delete: isAdmin,
        columns: [
          { key: 'name', label: 'Name', render: item => {
            const wrap = h('span', null, item.name);
            if (item.last_error) wrap.appendChild(h('div', { style: 'color:var(--danger);font-size:0.85em;margin-top:2px' }, item.last_error));
            return wrap;
          }},
          { key: 'url', label: 'URL', render: item => {
            const url = item.url || '';
            return url.length > 50 ? url.substring(0, 50) + '...' : url;
          }},
        ],
        fields: [
          { key: 'name', label: 'Source Name', placeholder: 'TV Guide' },
          { key: 'url', label: 'XMLTV URL', placeholder: 'http://epg-provider.com/guide.xml' },
        ],
        rowActions: isAdmin ? (item, reload) => [
          {
            label: 'Refresh',
            icon: '\u21BB',
            handler: async () => {
              try {
                await api.post('/api/epg/sources/' + item.id + '/refresh');
                epgCache.invalidate();
                channelsCache.invalidate();
                toast.success('EPG refresh started for ' + item.name);
                var pollCount = 0;
                var pollTimer = setInterval(async () => {
                  try {
                    var status = await api.get('/api/epg/sources/' + item.id + '/status');
                    if (status.state === 'done' || status.state === 'error') {
                      clearInterval(pollTimer);
                      reload();
                      if (status.state === 'done') toast.success(item.name + ': ' + status.message);
                      else toast.error(item.name + ': ' + status.message);
                    }
                  } catch (e) {}
                  if (++pollCount > 60) clearInterval(pollTimer);
                }, 2000);
              } catch (err) {
                toast.error(err.message);
              }
            },
          },
        ] : undefined,
      })(container);
    },

    'stream-profiles': buildCrudPage({
      title: 'Stream Profiles',
      singular: 'Stream Profile',
      apiPath: '/api/stream-profiles',
      create: true,
      update: item => !item.is_system,
      delete: item => !item.is_system && !item.is_client,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'stream_mode', label: 'Mode', render: item => ({direct:'Direct',proxy:'Proxy',ffmpeg:'FFmpeg'})[item.stream_mode] || item.stream_mode },
        { key: 'hwaccel', label: 'HW Accel', render: item => ({'default':'Global Default',none:'None (Software)',qsv:'Intel QSV',nvenc:'NVIDIA NVENC',vaapi:'VAAPI (AMD/Intel)',videotoolbox:'VideoToolbox (macOS)'})[item.hwaccel] || item.hwaccel },
        { key: 'video_codec', label: 'Codec', render: item => ({'default':'Global Default',copy:'Copy',h264:'H.264',h265:'H.265',av1:'AV1'})[item.video_codec] || item.video_codec },
        { key: 'container', label: 'Container', render: item => ({mpegts:'MPEG-TS',matroska:'Matroska',mp4:'MP4',webm:'WebM'})[item.container] || item.container },
        { key: 'delivery', label: 'Delivery', render: item => ({stream:'Stream (FFmpeg)',dash:'DASH (Shaka)'})[item.delivery] || item.delivery || 'stream' },
        { key: 'audio_codec', label: 'Audio', render: item => ({'default':'Auto',copy:'Copy',aac:'AAC',opus:'Opus'})[item.audio_codec] || item.audio_codec || 'Auto' },
        { key: 'is_default', label: 'Default', render: item => {
          const badges = [];
          if (item.is_system) badges.push(h('span', { className: 'badge badge-info', style: 'margin-right:4px' }, 'System'));
          if (item.is_client) badges.push(h('span', { className: 'badge badge-warning', style: 'margin-right:4px' }, 'Client'));
          if (item.is_default) badges.push(h('span', { className: 'badge badge-success' }, 'Default'));
          if (badges.length === 0) return '';
          const container = document.createElement('span');
          badges.forEach(b => container.appendChild(b));
          return container;
        }},
      ],
      fields: [
        { key: 'name', label: 'Profile Name', placeholder: 'My Stream Profile' },
        { key: 'stream_mode', label: 'Stream Mode', type: 'select', options: [
          { value: 'direct', label: 'Direct (source URL)' },
          { value: 'proxy', label: 'Proxy (HTTP relay)' },
          { value: 'ffmpeg', label: 'FFmpeg (transcode/copy)' },
        ], default: 'ffmpeg', help: 'How the stream reaches the client.' },
        { key: 'hwaccel', label: 'Hardware Acceleration', type: 'select', options: [
          { value: 'default', label: 'Global Default' },
          { value: 'none', label: 'None (Software)' },
          { value: 'qsv', label: 'Intel QSV (Arc/iGPU)' },
          { value: 'nvenc', label: 'NVIDIA NVENC' },
          { value: 'vaapi', label: 'VAAPI (AMD/Intel)' },
          { value: 'videotoolbox', label: 'VideoToolbox (macOS only)' },
        ], default: 'default', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'video_codec', label: 'Video Codec', type: 'select', options: [
          { value: 'default', label: 'Global Default' },
          { value: 'copy', label: 'Copy (No Transcode)' },
          { value: 'h264', label: 'H.264 / AVC' },
          { value: 'h265', label: 'H.265 / HEVC' },
          { value: 'av1', label: 'AV1' },
        ], default: 'default', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'container', label: 'Container', type: 'select', options: [
          { value: 'mpegts', label: 'MPEG-TS (HDHR/Plex)' },
          { value: 'matroska', label: 'Matroska (VLC)' },
          { value: 'mp4', label: 'MP4' },
          { value: 'webm', label: 'WebM' },
        ], showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'delivery', label: 'Delivery', type: 'select', options: [
          { value: 'stream', label: 'Stream (FFmpeg)' },
          { value: 'dash', label: 'DASH (Shaka)' },
        ], default: 'stream', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'audio_codec', label: 'Audio Codec', type: 'select', options: [
          { value: 'default', label: 'Default (auto)' },
          { value: 'copy', label: 'Copy (passthrough)' },
          { value: 'aac', label: 'AAC' },
          { value: 'opus', label: 'Opus' },
        ], default: 'default', help: 'Default: Opus for DASH/WebM, Copy for passthrough, AAC otherwise.', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'deinterlace', label: 'Deinterlace', type: 'checkbox', default: false, help: 'Apply yadif deinterlace filter (only when transcoding, not copy).', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' && (form.video_codec || 'default') !== 'copy' },
        { key: 'fps_mode', label: 'FPS Mode', type: 'select', options: [
          { value: 'auto', label: 'Auto (variable)' },
          { value: 'cfr', label: 'CFR (constant frame rate)' },
        ], default: 'auto', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' && (form.video_codec || 'default') !== 'copy' },
        { key: 'auto_detect', label: 'Auto-Detect', type: 'checkbox', default: false, help: 'Probe the stream on first play and automatically choose codec/filter settings. Probe result is cached and reused. Works for both M3U and SAT>IP streams.', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'use_custom_args', label: 'Use Custom Args', type: 'checkbox', default: false, help: 'When checked, the FFmpeg Args field below is used as the complete command (dropdowns are ignored).', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' && !form.auto_detect },
        { key: 'custom_args', label: 'FFmpeg Args', type: 'textarea', placeholder: '-b:v 4M -maxrate 5M', help: 'Extra flags appended to the composed command. When "Use Custom Args" is checked, this is the full command.', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' && !form.auto_detect },
      ],
    }),

    'hdhr-devices': buildCrudPage({
      title: 'HDHomeRun Devices',
      singular: 'HDHR Device',
      apiPath: '/api/hdhr/devices',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'device_id', label: 'Device ID' },
        { key: 'port', label: 'Port' },
        { key: 'tuner_count', label: 'Tuners' },
        { key: 'is_enabled', label: 'Status', render: item =>
          h('span', { className: 'badge ' + (item.is_enabled ? 'badge-success' : 'badge-danger') }, item.is_enabled ? 'Enabled' : 'Disabled')
        },
      ],
      fields: [
        { key: 'name', label: 'Device Name', placeholder: 'TVProxy HDHR' },
        { key: 'device_id', label: 'Device ID', placeholder: '12345678', help: '8-character hex device ID' },
        { key: 'tuner_count', label: 'Tuner Count', type: 'number', default: 2 },
        { key: 'port', label: 'Port', type: 'number', help: 'Auto-assigned starting at 47601. Each device needs a unique port for Plex.' },
        {
          key: 'channel_group_ids', label: 'Channel Groups', type: 'async-multi-select',
          loadOptions: () => channelGroupsCache.getAll(),
          valueKey: 'id', displayKey: 'name',
          help: 'Only serve channels in these groups. Leave all unchecked for all channels.',
        },
        { key: 'is_enabled', label: 'Enabled', type: 'checkbox', default: true },
      ],
    }),

    logos: buildCrudPage({
      title: 'Logos',
      singular: 'Logo',
      apiPath: '/api/logos',
      cache: logosCache,
      create: true,
      update: true,
      onChange: () => logosCache.invalidate(),
      columns: [
        { key: 'url', label: 'Logo', render: item => {
          const url = item.url || '';
          return url ? h('img', { src: url, alt: item.name || '', style: 'max-height:32px;max-width:64px;object-fit:contain;' }) : '';
        }},
        { key: 'name', label: 'Name' },
        { key: 'url', label: 'URL', render: item => {
          const url = item.url || '';
          return url.length > 60 ? url.substring(0, 60) + '...' : url;
        }},
      ],
      fields: [
        { key: 'name', label: 'Logo Name', placeholder: 'BBC Logo' },
        { key: 'url', label: 'Image URL', placeholder: 'https://...' },
      ],
    }),

    clients: async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      let clients = [];
      let profiles = [];
      try {
        [clients, profiles] = await Promise.all([api.get('/api/clients'), api.get('/api/stream-profiles')]);
        clients = Array.isArray(clients) ? clients : [];
        profiles = Array.isArray(profiles) ? profiles : [];
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        return;
      }

      const profileMap = {};
      profiles.forEach(p => { profileMap[p.id] = p.name; });

      function rulesLabel(rules) {
        if (!rules || rules.length === 0) return 'No rules';
        return rules.map(r => {
          if (r.match_type === 'exists') return r.header_name + ' exists';
          return r.header_name + ' ' + r.match_type + ' "' + r.match_value + '"';
        }).join(' AND ');
      }

      function renderList() {
        container.innerHTML = '';
        const rows = clients.map(c => {
          return h('tr', null,
            h('td', null, c.name),
            h('td', null, String(c.priority)),
            h('td', null, String(c.listen_port || 8080)),
            h('td', null, rulesLabel(c.match_rules)),
            h('td', null, profileMap[c.stream_profile_id] || '(unknown)'),
            h('td', null,
              h('span', { className: 'badge ' + (c.is_enabled ? 'badge-success' : 'badge-danger') }, c.is_enabled ? 'Enabled' : 'Disabled')
            ),
            h('td', null,
              h('button', { className: 'btn btn-sm', onClick: () => renderForm(c) }, 'Edit'),
              ' ',
              h('button', { className: 'btn btn-sm btn-danger', onClick: async () => {
                if (!confirm('Delete client "' + c.name + '"?')) return;
                try {
                  await api.del('/api/clients/' + c.id);
                  clients = clients.filter(x => x.id !== c.id);
                  toast.success('Client deleted');
                  renderList();
                } catch (err) { toast.error(err.message); }
              }}, 'Delete'),
            ),
          );
        });

        const table = h('table', { className: 'data-table' },
          h('thead', null, h('tr', null,
            h('th', null, 'Name'), h('th', null, 'Priority'), h('th', null, 'Port'), h('th', null, 'Match Rules'),
            h('th', null, 'Stream Profile'), h('th', null, 'Status'), h('th', null, 'Actions'),
          )),
          h('tbody', null, ...rows),
        );

        container.appendChild(h('div', { className: 'table-container' },
          h('div', { className: 'table-header' },
            h('h3', null, 'Client Detection'),
            h('button', { className: 'btn btn-primary', onClick: () => renderForm(null) }, '+ New Client'),
          ),
          clients.length > 0 ? table : h('p', { style: 'padding: 16px; color: var(--text-muted)' }, 'No clients configured.'),
        ));
      }

      function renderForm(existing) {
        container.innerHTML = '';
        const isEdit = !!existing;
        const rules = existing ? [...existing.match_rules] : [{ header_name: '', match_type: 'contains', match_value: '' }];

        const nameInp = h('input', { type: 'text', placeholder: 'Plex', value: existing ? existing.name : '' });
        const priorityInp = h('input', { type: 'number', value: existing ? String(existing.priority) : '50' });
        const portSelect = h('select', null,
          h('option', { value: '8080' }, '8080 (Main)'),
          h('option', { value: '8096' }, '8096 (Jellyfin)'),
        );
        portSelect.value = existing && existing.listen_port ? String(existing.listen_port) : '8080';
        const enabledChk = h('input', { type: 'checkbox' });
        enabledChk.checked = existing ? existing.is_enabled : true;

        const rulesContainer = h('div');

        function renderRuleRows() {
          rulesContainer.innerHTML = '';
          rules.forEach((rule, idx) => {
            const headerInp = h('input', { type: 'text', placeholder: 'User-Agent', value: rule.header_name || '', style: 'flex:1;min-width:120px' });
            headerInp.addEventListener('input', () => { rules[idx].header_name = headerInp.value; });

            const typeSelect = h('select', { style: 'width:120px' });
            ['contains', 'equals', 'prefix', 'exists'].forEach(mt => {
              const opt = h('option', { value: mt }, mt);
              if (rule.match_type === mt) opt.selected = true;
              typeSelect.appendChild(opt);
            });

            const valueInp = h('input', { type: 'text', placeholder: 'Match value', value: rule.match_value || '', style: 'flex:1;min-width:120px' });
            if (rule.match_type === 'exists') valueInp.style.display = 'none';
            valueInp.addEventListener('input', () => { rules[idx].match_value = valueInp.value; });

            typeSelect.addEventListener('change', () => {
              rules[idx].match_type = typeSelect.value;
              valueInp.style.display = typeSelect.value === 'exists' ? 'none' : '';
            });

            const removeBtn = h('button', { className: 'btn btn-sm btn-danger', onClick: () => {
              rules.splice(idx, 1);
              if (rules.length === 0) rules.push({ header_name: '', match_type: 'contains', match_value: '' });
              renderRuleRows();
            }}, '\u2715');

            rulesContainer.appendChild(h('div', { style: 'display:flex;gap:8px;align-items:center;margin-bottom:8px' },
              headerInp, typeSelect, valueInp, removeBtn,
            ));
          });
        }
        renderRuleRows();

        const addRuleBtn = h('button', { className: 'btn btn-sm', onClick: () => {
          rules.push({ header_name: '', match_type: 'contains', match_value: '' });
          renderRuleRows();
        }}, '+ Add Rule');

        const saveBtn = h('button', { className: 'btn btn-primary', onClick: async () => {
          saveBtn.disabled = true;
          const matchRules = rules.map(r => ({
            header_name: r.header_name,
            match_type: r.match_type,
            match_value: r.match_type === 'exists' ? '' : r.match_value,
          }));

          try {
            if (isEdit) {
              const updated = await api.put('/api/clients/' + existing.id, {
                name: nameInp.value,
                priority: parseInt(priorityInp.value, 10) || 0,
                listen_port: parseInt(portSelect.value, 10) || 8080,
                stream_profile_id: existing.stream_profile_id,
                is_enabled: enabledChk.checked,
                match_rules: matchRules,
              });
              const idx = clients.findIndex(c => c.id === existing.id);
              if (idx >= 0) clients[idx] = updated;
              toast.success('Client updated');
            } else {
              const created = await api.post('/api/clients', {
                name: nameInp.value,
                priority: parseInt(priorityInp.value, 10) || 0,
                listen_port: parseInt(portSelect.value, 10) || 8080,
                is_enabled: enabledChk.checked,
                match_rules: matchRules,
              });
              clients.push(created);
              try { profiles = await api.get('/api/stream-profiles'); profiles = Array.isArray(profiles) ? profiles : []; profiles.forEach(p => { profileMap[p.id] = p.name; }); } catch(e) {}
              toast.success('Client created');
            }
            renderList();
          } catch (err) {
            toast.error(err.message);
            saveBtn.disabled = false;
          }
        }}, isEdit ? 'Save Changes' : 'Create Client');

        const cancelBtn = h('button', { className: 'btn', onClick: renderList }, 'Cancel');

        const formContent = h('div', { style: 'padding: 16px; max-width: 700px' },
          h('div', { className: 'form-group' }, h('label', null, 'Client Name'), nameInp),
          h('div', { className: 'form-group' }, h('label', null, 'Priority'), priorityInp,
            h('small', { style: 'color: var(--text-muted); display: block' }, 'Lower number = higher priority. Clients are checked in order.')),
          h('div', { className: 'form-group' }, h('label', null, 'Incoming Port'), portSelect,
            h('small', { style: 'color: var(--text-muted); display: block' }, 'Which port this client connects through. Jellyfin clients use 8096.')),
          h('div', { className: 'form-group' }, h('label', null, 'Match Rules (all must match)'), rulesContainer, addRuleBtn),
        );

        if (isEdit) {
          const profileName = profileMap[existing.stream_profile_id] || '(unknown)';
          formContent.appendChild(h('div', { className: 'form-group' }, h('label', null, 'Stream Profile'),
            h('input', { type: 'text', value: profileName, disabled: true, style: 'opacity: 0.7' }),
            h('small', { style: 'color: var(--text-muted); display: block' }, 'Auto-created on client creation. Edit the profile via Stream Profiles to change encoding settings.')));
        }

        formContent.appendChild(h('div', { className: 'form-check', style: 'display:flex;align-items:center;gap:6px' }, enabledChk, h('label', { style: 'cursor:pointer;margin:0' }, 'Enabled')));
        formContent.appendChild(h('div', { style: 'display: flex; gap: 8px; margin-top: 16px' }, saveBtn, cancelBtn));

        container.appendChild(h('div', { className: 'table-container' },
          h('div', { className: 'table-header' }, h('h3', null, isEdit ? 'Edit Client: ' + existing.name : 'New Client')),
          formContent,
        ));
      }

      renderList();
    },

    users: buildCrudPage({
      title: 'Users',
      singular: 'User',
      apiPath: '/api/users',
      create: true,
      update: true,
      columns: [
        { key: 'username', label: 'Username' },
        { key: 'is_admin', label: 'Role', render: item =>
          h('span', { className: 'badge ' + (item.is_admin ? 'badge-warning' : 'badge-info') }, item.is_admin ? 'Admin' : 'User')
        },
        { key: 'invite_token', label: 'Status', render: item => {
          if (item.invite_token) return h('span', { className: 'badge badge-warning' }, 'Invited');
          return h('span', { className: 'badge badge-success' }, 'Active');
        }},
      ],
      fields: [
        { key: 'username', label: 'Username', placeholder: 'john' },
        { key: 'password', label: 'Password', type: 'password', placeholder: 'Enter password' },
        { key: 'is_admin', label: 'Administrator', type: 'checkbox', default: false },
      ],
      extraActions: [
        {
          label: 'Invite User',
          handler: () => {
            const usernameInp = h('input', { type: 'text', placeholder: 'username' });
            const resultEl = h('div');
            showModal('Invite User', h('div', null,
              h('div', { className: 'form-group' }, h('label', null, 'Username'), usernameInp),
              resultEl,
            ), async () => {
              if (!usernameInp.value.trim()) throw new Error('Username is required');
              const user = await api.post('/api/users/invite', { username: usernameInp.value.trim() });
              const inviteUrl = window.location.origin + '/#/invite/' + user.invite_token;
              resultEl.innerHTML = '';
              resultEl.appendChild(h('div', { style: 'margin-top:12px;padding:12px;background:var(--bg-input);border:1px solid var(--border);border-radius:var(--radius-sm);word-break:break-all;font-size:13px' },
                h('label', { style: 'display:block;margin-bottom:4px;color:var(--text-secondary)' }, 'Share this invite link:'),
                h('code', null, inviteUrl),
              ));
              try { await navigator.clipboard.writeText(inviteUrl); toast.success('Invite link copied to clipboard'); } catch {}
            }, 'Create Invite');
          },
        },
      ],
    }),

    movies: async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading movies...'));

      try {
        var items = await api.get('/api/vod/library?type=movie');
        container.innerHTML = '';

        if (items.length === 0) {
          container.appendChild(h('div', { style: 'text-align:center;padding:48px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\uD83C\uDFAC'),
            h('p', { style: 'font-size:1.1em' }, 'No movies found. Add a tvproxy-streams M3U source to import your movie library.')
          ));
          return;
        }

        var collections = {};
        var standalone = [];
        items.forEach(function(item) {
          if (item.collection) {
            if (!collections[item.collection]) collections[item.collection] = { name: item.collection, movies: [] };
            collections[item.collection].movies.push(item);
          } else {
            standalone.push(item);
          }
        });

        var displayItems = [];
        standalone.forEach(function(item) { displayItems.push({ type: 'movie', item: item }); });
        Object.values(collections).forEach(function(col) {
          col.movies.sort(function(a, b) { return (a.year || '9999').localeCompare(b.year || '9999') || a.name.localeCompare(b.name); });
          displayItems.push({ type: 'collection', collection: col });
        });
        displayItems.sort(function(a, b) {
          var nameA = a.type === 'movie' ? a.item.name : a.collection.name;
          var nameB = b.type === 'movie' ? b.item.name : b.collection.name;
          return nameA.localeCompare(nameB);
        });

        var titleCount = standalone.length + Object.keys(collections).length;
        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:24px;position:sticky;top:0;z-index:10;background:var(--bg-main);padding:12px 0;' });
        header.appendChild(h('h2', { style: 'margin:0' }, 'Movies'));
        var headerRight = h('div', { style: 'display:flex;align-items:center;gap:16px' });
        var syncSpan = h('span', { id: 'tmdb-sync-movies', style: 'display:none;font-size:0.85em;color:var(--accent)' });
        headerRight.appendChild(syncSpan);
        headerRight.appendChild(h('span', { style: 'color:var(--text-muted);font-size:0.95em' }, items.length + ' titles'));
        header.appendChild(headerRight);
        container.appendChild(header);

        var syncPoll = setInterval(function() {
          if (!document.getElementById('tmdb-sync-movies')) { clearInterval(syncPoll); return; }
          api.get('/api/tmdb/sync').then(function(s) {
            if (s.syncing && s.total > 0) {
              var pct = Math.round(s.completed / s.total * 100);
              syncSpan.textContent = 'Syncing artwork ' + pct + '% (' + s.completed + '/' + s.total + ')';
              syncSpan.style.display = '';
            } else {
              syncSpan.style.display = 'none';
              if (!s.syncing) clearInterval(syncPoll);
            }
          }).catch(function() {});
        }, 2000);

        var kidsCerts = { 'U': 1, 'PG': 1, 'G': 1, 'PG-13': 1, '12': 1, '12A': 1, 'TV-Y': 1, 'TV-Y7': 1, 'TV-G': 1, 'TV-PG': 1 };
        var decades = {};
        displayItems.forEach(function(di) {
          var yr = di.type === 'movie' ? di.item.year : null;
          if (yr && yr.length === 4) {
            var dec = yr.substring(0, 3) + '0s';
            decades[dec] = true;
          }
          if (di.type === 'collection') {
            di.collection.movies.forEach(function(m) {
              if (m.year && m.year.length === 4) decades[m.year.substring(0, 3) + '0s'] = true;
            });
          }
        });
        var decadeList = Object.keys(decades).sort();

        var activeFilters = {};
        var filterBar = h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px;position:sticky;top:52px;z-index:9;background:var(--bg-main);padding:8px 0;' });

        var pillBase = 'padding:5px 14px;border-radius:20px;cursor:pointer;font-size:12px;font-weight:500;transition:all 0.15s;';
        var pillGroups = {
          age:        { off: pillBase + 'border:1px solid rgba(168,85,247,0.3);background:rgba(168,85,247,0.08);color:#a855f7;', on: pillBase + 'border:1px solid #a855f7;background:#a855f7;color:#fff;' },
          collection: { off: pillBase + 'border:1px solid rgba(234,179,8,0.3);background:rgba(234,179,8,0.08);color:#eab308;', on: pillBase + 'border:1px solid #eab308;background:#eab308;color:#000;' },
          decade:     { off: pillBase + 'border:1px solid rgba(34,197,94,0.3);background:rgba(34,197,94,0.08);color:#22c55e;', on: pillBase + 'border:1px solid #22c55e;background:#22c55e;color:#fff;' },
          genre:      { off: pillBase + 'border:1px solid rgba(59,130,246,0.3);background:rgba(59,130,246,0.08);color:#3b82f6;', on: pillBase + 'border:1px solid #3b82f6;background:#3b82f6;color:#fff;' },
        };

        function makePill(label, key, parent, group) {
          var styles = pillGroups[group] || pillGroups.genre;
          var btn = h('button', { style: styles.off }, label);
          btn.onclick = function() {
            if (activeFilters[key]) { delete activeFilters[key]; btn.style.cssText = styles.off; }
            else { activeFilters[key] = true; btn.style.cssText = styles.on; }
            renderGrid();
          };
          (parent || filterBar).appendChild(btn);
          return btn;
        }

        makePill('Kids', 'kids', null, 'age');
        makePill('15+', 'adult', null, 'age');
        makePill('Collections', 'collections', null, 'collection');
        filterBar.appendChild(h('span', { style: 'width:1px;height:20px;background:var(--border);align-self:center;' }));
        decadeList.forEach(function(dec) { makePill(dec, 'decade_' + dec, null, 'decade'); });

        container.appendChild(filterBar);

        var genreCounts = {};
        items.forEach(function(item) {
          (item.genres || []).forEach(function(g) { genreCounts[g] = (genreCounts[g] || 0) + 1; });
        });
        var genreNames = Object.keys(genreCounts).sort(function(a, b) { return genreCounts[b] - genreCounts[a]; });

        if (genreNames.length > 0) {
          var genreBar = h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px;position:sticky;top:92px;z-index:8;background:var(--bg-main);padding:8px 0;' });
          genreNames.forEach(function(g) { makePill(g, 'genre_' + g, genreBar, 'genre'); });
          container.appendChild(genreBar);
        }

        var countSpan = headerRight.querySelector('span:last-child');

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:20px;' });

        function matchesFilters(di) {
          var hasAge = activeFilters.kids || activeFilters.adult;
          var hasDecade = Object.keys(activeFilters).some(function(k) { return k.startsWith('decade_'); });
          var hasColl = activeFilters.collections;

          if (hasColl && di.type !== 'collection') return false;

          var itemYear = di.type === 'movie' ? di.item.year : null;
          var itemCert = di.type === 'movie' ? di.item.certification : null;
          var collMovies = di.type === 'collection' ? di.collection.movies : null;

          if (hasAge) {
            if (di.type === 'movie' && itemCert) {
              var isKid = kidsCerts[itemCert];
              if (activeFilters.kids && !activeFilters.adult && !isKid) return false;
              if (activeFilters.adult && !activeFilters.kids && isKid) return false;
            }
            if (di.type === 'collection' && collMovies) {
              var rated = collMovies.filter(function(m) { return m.certification; });
              if (rated.length > 0) {
                var anyMatch = rated.some(function(m) {
                  var isK = kidsCerts[m.certification];
                  if (activeFilters.kids && activeFilters.adult) return true;
                  return activeFilters.kids ? isK : !isK;
                });
                if (!anyMatch) return false;
              }
            }
          }

          if (hasDecade) {
            var activeDecades = {};
            Object.keys(activeFilters).forEach(function(k) { if (k.startsWith('decade_')) activeDecades[k.replace('decade_', '')] = true; });
            if (di.type === 'movie') {
              if (!itemYear || !activeDecades[itemYear.substring(0, 3) + '0s']) return false;
            }
            if (di.type === 'collection' && collMovies) {
              var anyDecade = collMovies.some(function(m) { return m.year && activeDecades[m.year.substring(0, 3) + '0s']; });
              if (!anyDecade) return false;
            }
          }

          var activeGenres = [];
          Object.keys(activeFilters).forEach(function(k) { if (k.startsWith('genre_')) activeGenres.push(k.replace('genre_', '')); });
          if (activeGenres.length > 0) {
            var itemGenres = di.type === 'movie' ? (di.item.genres || []) : [];
            if (di.type === 'collection' && collMovies) {
              itemGenres = [];
              collMovies.forEach(function(m) { (m.genres || []).forEach(function(g) { if (itemGenres.indexOf(g) === -1) itemGenres.push(g); }); });
            }
            var allMatch = activeGenres.every(function(g) { return itemGenres.indexOf(g) >= 0; });
            if (!allMatch) return false;
          }

          return true;
        }

        function renderGrid() {
          grid.innerHTML = '';
          var filtered = displayItems.filter(matchesFilters);
          var count = 0;
          filtered.forEach(function(di) {
            if (di.type === 'movie') { renderMovieCard(di.item, grid); count++; }
            else { renderCollectionCard(di.collection, grid); count++; }
          });
          countSpan.textContent = count + ' / ' + displayItems.length + ' titles';
          setupGridKeyJump(grid);
        }

        function renderMovieCard(item, parent) {
          var card = h('div', { style: 'cursor:pointer;border-radius:12px;overflow:hidden;background:var(--bg-card);border:1px solid var(--border);transition:transform 0.2s,box-shadow 0.2s;' });
          card.dataset.sortName = item.name;
          card.onmouseenter = function() { card.style.transform = 'scale(1.03)'; card.style.boxShadow = '0 8px 30px rgba(0,0,0,0.3)'; };
          card.onmouseleave = function() { card.style.transform = ''; card.style.boxShadow = ''; };

          var posterWrap = h('div', { style: 'width:100%;aspect-ratio:2/3;background:linear-gradient(135deg,#1a1a2e,#16213e);display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden;' });
          if (item.poster_url) {
            posterWrap.appendChild(h('img', { src: item.poster_url, style: 'width:100%;height:100%;object-fit:cover;' }));
          } else {
            posterWrap.appendChild(h('div', { style: 'padding:12px;text-align:center;color:#fff;font-size:14px;font-weight:600;text-shadow:0 1px 4px rgba(0,0,0,0.5);' }, item.name));
          }
          card.appendChild(posterWrap);

          var info = h('div', { style: 'padding:10px 12px;' });
          info.appendChild(h('div', { style: 'font-size:13px;font-weight:600;color:var(--text-primary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;' }, item.name));

          var badges = [];
          if (item.year) badges.push(item.year);
          if (item.certification) badges.push(item.certification);
          if (item.rating > 0) badges.push('\u2605 ' + item.rating.toFixed(1));
          if (item.resolution) badges.push(item.resolution);
          if (item.audio) badges.push(item.audio);
          if (item.duration > 0) {
            var hrs = Math.floor(item.duration / 3600);
            var mins = Math.floor((item.duration % 3600) / 60);
            badges.push(hrs > 0 ? hrs + 'h ' + mins + 'm' : mins + 'm');
          }
          if (badges.length) {
            var badgeRow = h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap;margin-top:4px;' });
            badges.forEach(function(b) { badgeRow.appendChild(h('span', { style: 'font-size:10px;padding:2px 6px;border-radius:4px;background:rgba(255,255,255,0.08);color:var(--text-muted);' }, b)); });
            info.appendChild(badgeRow);
          }
          if (item.genres && item.genres.length) {
            var genreRow = h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap;margin-top:3px;' });
            item.genres.slice(0, 2).forEach(function(g) { genreRow.appendChild(h('span', { style: 'font-size:9px;padding:1px 5px;border-radius:3px;background:rgba(59,130,246,0.15);color:#60a5fa;' }, g)); });
            info.appendChild(genreRow);
          }
          card.appendChild(info);

          card.onclick = function() {
            showProgrammeModal({
              title: item.name, mediaType: 'movie',
              time: badges.filter(function(b) { return b.includes('h') || b.includes('m'); }).join(''),
              description: item.overview || '',
              year: item.year, certification: item.certification,
              rating: item.rating, genres: item.genres,
              channelName: '', channelID: null, tvgId: null,
              isLive: false, isFuture: false, vodStreamURL: item.url, vodStreamID: item.id,
            });
          };
          parent.appendChild(card);
        }

        function showCollectionModal(col) {
          var overlay = document.createElement('div');
          overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:9999;display:flex;align-items:center;justify-content:center;backdrop-filter:blur(6px);';
          overlay.onclick = function(e) { if (e.target === overlay) overlay.remove(); };
          document.addEventListener('keydown', function onKey(e) { if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', onKey); } });

          var modal = document.createElement('div');
          modal.style.cssText = 'width:90%;max-width:1080px;max-height:92vh;background:#1a1d23;border-radius:16px;overflow:hidden;display:flex;flex-direction:column;box-shadow:0 24px 80px rgba(0,0,0,0.6);';

          var backdrop = document.createElement('div');
          backdrop.style.cssText = 'width:100%;height:280px;background:linear-gradient(135deg,#1a1a2e,#0f3460);position:relative;overflow:hidden;flex-shrink:0;';
          backdrop.appendChild(Object.assign(document.createElement('div'), { style: 'position:absolute;bottom:0;left:0;right:0;height:150px;background:linear-gradient(transparent,#1a1d23);' }));

          var closeBtn = document.createElement('button');
          closeBtn.textContent = '\u2715';
          closeBtn.style.cssText = 'position:absolute;top:16px;right:16px;background:rgba(0,0,0,0.6);border:none;color:#fff;font-size:18px;width:40px;height:40px;border-radius:50%;cursor:pointer;z-index:3;';
          closeBtn.onclick = function() { overlay.remove(); };
          backdrop.appendChild(closeBtn);

          var titleBlock = document.createElement('div');
          titleBlock.style.cssText = 'position:absolute;bottom:24px;left:32px;z-index:1;';
          titleBlock.innerHTML = '<div style="font-size:32px;font-weight:800;color:#fff;text-shadow:0 2px 12px rgba(0,0,0,0.7)">' + esc(col.name) + '</div>';
          titleBlock.innerHTML += '<div style="color:rgba(255,255,255,0.7);font-size:14px;margin-top:4px">' + col.movies.length + ' film' + (col.movies.length > 1 ? 's' : '') + '</div>';
          backdrop.appendChild(titleBlock);
          modal.appendChild(backdrop);

          var body = document.createElement('div');
          body.style.cssText = 'padding:24px 32px;overflow-y:auto;flex:1;';

          col.movies.forEach(function(movie) {
            var row = document.createElement('div');
            row.style.cssText = 'display:flex;align-items:flex-start;gap:16px;padding:12px 16px;border-radius:8px;cursor:pointer;transition:background 0.15s;';
            row.onmouseenter = function() { row.style.background = 'rgba(255,255,255,0.05)'; };
            row.onmouseleave = function() { row.style.background = ''; };

            if (movie.poster_url) {
              row.appendChild(h('img', { src: movie.poster_url, style: 'width:60px;height:90px;object-fit:cover;border-radius:6px;flex-shrink:0;' }));
            }

            var info = document.createElement('div');
            info.style.cssText = 'flex:1;min-width:0;';
            info.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:14px;font-weight:600;color:var(--text-primary);', textContent: movie.name }));

            var meta = [];
            if (movie.year) meta.push(movie.year);
            if (movie.certification) meta.push(movie.certification);
            if (movie.rating > 0) meta.push('\u2605 ' + movie.rating.toFixed(1));
            if (movie.resolution) meta.push(movie.resolution);
            if (movie.audio) meta.push(movie.audio);
            if (movie.duration > 0) { var m = Math.floor(movie.duration / 60); meta.push(Math.floor(m/60) + 'h ' + (m%60) + 'm'); }
            if (meta.length) info.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:12px;color:var(--text-muted);margin-top:2px;', textContent: meta.join(' \u2022 ') }));

            if (movie.genres && movie.genres.length) {
              var gRow = document.createElement('div');
              gRow.style.cssText = 'display:flex;gap:4px;flex-wrap:wrap;margin-top:3px;';
              movie.genres.slice(0, 3).forEach(function(g) { gRow.appendChild(h('span', { style: 'font-size:10px;padding:1px 5px;border-radius:3px;background:rgba(59,130,246,0.15);color:#60a5fa;' }, g)); });
              info.appendChild(gRow);
            }

            if (movie.overview) {
              info.appendChild(Object.assign(document.createElement('div'), { style: 'font-size:11px;color:#9ca3af;margin-top:4px;line-height:1.4;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;', textContent: movie.overview }));
            }

            row.appendChild(info);

            var playBtn = document.createElement('button');
            playBtn.style.cssText = 'background:#3b82f6;border:none;color:#fff;width:36px;height:36px;border-radius:50%;cursor:pointer;font-size:16px;flex-shrink:0;margin-top:2px;';
            playBtn.textContent = '\u25B6';
            playBtn.onclick = function(e) { e.stopPropagation(); overlay.remove(); play({ streamID: movie.id, name: movie.name }); };
            row.appendChild(playBtn);

            row.onclick = function() { overlay.remove(); play({ streamID: movie.id, name: movie.name }); };
            body.appendChild(row);
          });

          modal.appendChild(body);
          overlay.appendChild(modal);
          document.body.appendChild(overlay);

          var colBackdrop = col.movies.find(function(m) { return m.collection_backdrop; });
          if (colBackdrop) {
            backdrop.style.backgroundImage = 'url(' + colBackdrop.collection_backdrop + ')';
            backdrop.style.backgroundSize = 'cover';
            backdrop.style.backgroundPosition = 'center 20%';
          }
        }

        function renderCollectionCard(col, parent) {
          var card = h('div', { style: 'cursor:pointer;border-radius:12px;overflow:hidden;background:var(--bg-card);border:1px solid var(--border);transition:transform 0.2s,box-shadow 0.2s;' });
          card.dataset.sortName = col.name;
          card.onmouseenter = function() { card.style.transform = 'scale(1.03)'; card.style.boxShadow = '0 8px 30px rgba(0,0,0,0.3)'; };
          card.onmouseleave = function() { card.style.transform = ''; card.style.boxShadow = ''; };

          var posterWrap = h('div', { style: 'width:100%;aspect-ratio:2/3;background:linear-gradient(135deg,#1a1a2e,#16213e);display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden;' });
          var colPoster = col.movies.find(function(m) { return m.collection_poster; });
          var posterSrc = colPoster ? colPoster.collection_poster : (col.movies.find(function(m) { return m.poster_url; }) || {}).poster_url;
          if (posterSrc) {
            posterWrap.appendChild(h('img', { src: posterSrc, style: 'width:100%;height:100%;object-fit:cover;' }));
          } else {
            posterWrap.appendChild(h('div', { style: 'padding:12px;text-align:center;color:#fff;font-size:14px;font-weight:600;text-shadow:0 1px 4px rgba(0,0,0,0.5);' }, col.name));
          }
          posterWrap.appendChild(h('div', { style: 'position:absolute;bottom:8px;right:8px;background:rgba(0,0,0,0.7);color:#fff;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;' }, col.movies.length + ' films'));
          card.appendChild(posterWrap);

          card.appendChild(h('div', { style: 'padding:10px 12px;' },
            h('div', { style: 'font-size:13px;font-weight:600;color:var(--text-primary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;' }, col.name)));

          card.onclick = function() { showCollectionModal(col); };
          parent.appendChild(card);
        }

        container.appendChild(grid);
        renderGrid();
      } catch(err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color:var(--danger)' }, 'Failed to load: ' + err.message));
      }
    },

    'tv-series': async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading TV series...'));

      try {
        var items = await api.get('/api/vod/library?type=series');
        container.innerHTML = '';

        if (items.length === 0) {
          container.appendChild(h('div', { style: 'text-align:center;padding:48px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\uD83D\uDCFA'),
            h('p', { style: 'font-size:1.1em' }, 'No TV series found. Add a tvproxy-streams M3U source to import your TV library.')
          ));
          return;
        }

        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:24px;position:sticky;top:0;z-index:10;background:var(--bg-main);padding:12px 0;' });
        header.appendChild(h('h2', { style: 'margin:0' }, 'TV Series'));
        var headerRight2 = h('div', { style: 'display:flex;align-items:center;gap:16px' });
        var syncSpan2 = h('span', { id: 'tmdb-sync-tv', style: 'display:none;font-size:0.85em;color:var(--accent)' });
        headerRight2.appendChild(syncSpan2);
        header.appendChild(headerRight2);
        container.appendChild(header);

        var seriesMap = {};
        items.forEach(function(item) {
          var key = item.series || item.name;
          if (!seriesMap[key]) seriesMap[key] = { name: key, seasons: {}, episodes: [] };
          seriesMap[key].episodes.push(item);
          if (item.season > 0) {
            if (!seriesMap[key].seasons[item.season]) seriesMap[key].seasons[item.season] = [];
            seriesMap[key].seasons[item.season].push(item);
          }
        });

        var seriesList = Object.values(seriesMap).sort(function(a, b) { return a.name.localeCompare(b.name); });
        headerRight2.appendChild(h('span', { style: 'color:var(--text-muted);font-size:0.95em' }, seriesList.length + ' series'));
        header.appendChild(headerRight2);

        var syncPoll2 = setInterval(function() {
          if (!document.getElementById('tmdb-sync-tv')) { clearInterval(syncPoll2); return; }
          api.get('/api/tmdb/sync').then(function(s) {
            if (s.syncing && s.total > 0) {
              var pct = Math.round(s.completed / s.total * 100);
              syncSpan2.textContent = 'Syncing artwork ' + pct + '% (' + s.completed + '/' + s.total + ')';
              syncSpan2.style.display = '';
            } else {
              syncSpan2.style.display = 'none';
              if (!s.syncing) clearInterval(syncPoll2);
            }
          }).catch(function() {});
        }, 2000);

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:20px;' });

        seriesList.forEach(function(show) {
          var card = h('div', { style: 'cursor:pointer;border-radius:12px;overflow:hidden;background:var(--bg-card);border:1px solid var(--border);transition:transform 0.2s,box-shadow 0.2s;' });
          card.dataset.sortName = show.name;
          card.onmouseenter = function() { card.style.transform = 'scale(1.03)'; card.style.boxShadow = '0 8px 30px rgba(0,0,0,0.3)'; };
          card.onmouseleave = function() { card.style.transform = ''; card.style.boxShadow = ''; };

          var posterWrap = h('div', { style: 'width:100%;aspect-ratio:2/3;background:linear-gradient(135deg,#1a1a2e,#0f3460);display:flex;align-items:center;justify-content:center;position:relative;' });
          var showPoster = '';
          show.episodes.some(function(ep) { if (ep.poster_url) { showPoster = ep.poster_url; return true; } return false; });
          if (showPoster) {
            posterWrap.appendChild(h('img', { src: showPoster, style: 'width:100%;height:100%;object-fit:cover;' }));
          } else {
            posterWrap.appendChild(h('div', { style: 'padding:12px;text-align:center;color:#fff;font-size:14px;font-weight:600;' }, show.name));
          }
          card.appendChild(posterWrap);

          var info = h('div', { style: 'padding:10px 12px;' });
          info.appendChild(h('div', { style: 'font-size:13px;font-weight:600;color:var(--text-primary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;' }, show.name));
          var seasonCount = Object.keys(show.seasons).length;
          var epCount = show.episodes.length;
          info.appendChild(h('div', { style: 'font-size:11px;color:var(--text-muted);margin-top:2px;' },
            (seasonCount > 0 ? seasonCount + ' season' + (seasonCount > 1 ? 's' : '') + ' \u2022 ' : '') + epCount + ' episode' + (epCount > 1 ? 's' : '')));
          card.appendChild(info);

          card.onclick = function() {
            showSeriesDetail(show);
          };

          grid.appendChild(card);
        });

        container.appendChild(grid);
        setupGridKeyJump(grid);
      } catch(err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color:var(--danger)' }, 'Failed to load: ' + err.message));
      }
    },

    recordings: async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading recordings...'));

      var pollTimer = null;

      function fmtSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
        return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
      }

      async function renderCompleted(completedDiv) {
        try {
          var recordings = await api.get('/api/recordings/completed');
          completedDiv.innerHTML = '';
          if (!recordings || recordings.length === 0) {
            completedDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No completed recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Title</th><th>Channel</th><th>Duration</th><th>Size</th><th>Date</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var dateStr = fmtLocalDateTime(rec.mod_time);
            var title = (rec.meta && rec.meta.program_title) || rec.filename;
            var channelName = (rec.meta && rec.meta.channel_name) || '';
            var encodedStreamID = encodeURIComponent(rec.stream_id);
            var encodedName = encodeURIComponent(rec.filename);
            var basePath = '/api/recordings/completed/' + encodedStreamID + '/' + encodedName;
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var playBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: function() {
              play({ streamID: rec.stream_id, name: title, recordingPath: basePath + '/play' });
            }}, '\u25B6 Play');
            var deleteBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm('Delete ' + title + '?')) return;
              await api.del(basePath);
              renderCompleted(completedDiv);
            }}, 'Delete');
            var dlBtn = h('button', { className: 'btn btn-sm', onClick: function() {
              var a = document.createElement('a');
              a.href = basePath + '/stream?token=' + encodeURIComponent(state.accessToken || '');
              a.download = rec.filename;
              a.click();
            }}, '\u2B07 Download');
            actions.appendChild(playBtn);
            actions.appendChild(dlBtn);
            actions.appendChild(deleteBtn);
            var durStr = rec.duration > 0 ? (Math.floor(rec.duration / 60) + ':' + String(Math.floor(rec.duration % 60)).padStart(2, '0')) : '-';
            var tr = h('tr', null,
              h('td', null, title),
              h('td', null, channelName),
              h('td', null, durStr),
              h('td', null, fmtSize(rec.size)),
              h('td', { title: fmtUTC(rec.mod_time) }, dateStr),
              actions
            );
            tbody.appendChild(tr);
          });
          table.appendChild(tbody);
          completedDiv.appendChild(table);
        } catch(err) {
          completedDiv.innerHTML = '';
          completedDiv.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        }
      }

      async function renderScheduled(scheduledDiv) {
        try {
          var allRecordings = await api.get('/api/recordings/schedule');
          var recordings = (allRecordings || []).filter(function(r) { return r.status === 'pending' || r.status === 'recording'; });
          scheduledDiv.innerHTML = '';
          if (recordings.length === 0) {
            scheduledDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No upcoming or active recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Channel</th><th>Program</th><th>Start</th><th>Stop</th><th>Status</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var isActive = rec.status === 'recording';
            var startStr = fmtLocalDateTime(rec.start_at);
            var stopStr = fmtLocalDateTime(rec.stop_at);
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var deleteBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm((isActive ? 'Stop' : 'Delete') + ' recording "' + (rec.program_title || '') + '"?')) return;
              await api.del('/api/recordings/schedule/' + rec.id);
              renderScheduled(scheduledDiv);
            }}, isActive ? 'Stop' : 'Delete');
            actions.appendChild(deleteBtn);
            var statusCell = isActive ? h('td', { style: 'color:var(--danger);font-weight:600' }, '\u23FA Recording') : h('td', null, 'Scheduled');
            var tr = h('tr', null,
              h('td', null, rec.channel_name),
              h('td', null, rec.program_title),
              h('td', { title: fmtUTC(rec.start_at) }, startStr),
              h('td', { title: fmtUTC(rec.stop_at) }, stopStr),
              statusCell,
              actions
            );
            tbody.appendChild(tr);
          });
          table.appendChild(tbody);
          scheduledDiv.appendChild(table);
        } catch(err) {
          scheduledDiv.innerHTML = '';
          scheduledDiv.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        }
      }

      async function renderActive(activeDiv) {
        try {
          var activity = await api.get('/api/activity');
          var recordings = (activity || []).filter(function(v) { return v.type === 'recording'; });
          activeDiv.innerHTML = '';
          if (recordings.length === 0) {
            activeDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No active recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Channel</th><th>Profile</th><th>Duration</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var secs = Math.floor((Date.now() - new Date(rec.started_at).getTime()) / 1000);
            if (secs < 0) secs = 0;
            var m = Math.floor(secs / 60);
            var s = secs % 60;
            var durStr = m + ':' + String(s).padStart(2, '0');
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var playBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: function() {
              play({ channelID: rec.channel_id, name: rec.channel_name || rec.stream_name || '?' });
            }}, '\u25B6 Play');
            var stopBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              stopBtn.disabled = true;
              stopBtn.textContent = 'Stopping...';
              try {
                await api.del('/api/vod/record/' + rec.channel_id);
                renderActive(activeDiv);
              } catch(e) {
                stopBtn.textContent = 'Failed';
                setTimeout(function() { stopBtn.textContent = 'Stop'; stopBtn.disabled = false; }, 2000);
              }
            }}, 'Stop');
            actions.appendChild(playBtn);
            actions.appendChild(stopBtn);
            var tr = h('tr', null,
              h('td', null, rec.channel_name || rec.stream_name || '?'),
              h('td', null, rec.profile_name || '-'),
              h('td', null, durStr),
              actions
            );
            tbody.appendChild(tr);
          });
          table.appendChild(tbody);
          activeDiv.appendChild(table);
        } catch(err) {
          activeDiv.innerHTML = '';
          activeDiv.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        }
      }

      container.innerHTML = '';

      var activeSection = h('div', { className: 'table-container' },
        h('div', { className: 'table-header' }, h('h3', null, 'Active Recordings'))
      );
      var activeDiv = h('div');
      activeSection.appendChild(activeDiv);

      var scheduledSection = h('div', { className: 'table-container', style: 'margin-top: 16px;' },
        h('div', { className: 'table-header' }, h('h3', null, 'Scheduled Recordings'))
      );
      var scheduledDiv = h('div');
      scheduledSection.appendChild(scheduledDiv);

      var completedSection = h('div', { className: 'table-container', style: 'margin-top: 16px;' },
        h('div', { className: 'table-header' }, h('h3', null, 'Completed Recordings'))
      );
      var completedDiv = h('div');
      completedSection.appendChild(completedDiv);

      container.appendChild(activeSection);
      container.appendChild(scheduledSection);
      container.appendChild(completedSection);

      renderActive(activeDiv);
      renderScheduled(scheduledDiv);
      renderCompleted(completedDiv);

      pollTimer = setInterval(function() { renderActive(activeDiv); renderScheduled(scheduledDiv); }, 5000);

      var observer = new MutationObserver(function() {
        if (!document.body.contains(container)) {
          if (pollTimer) clearInterval(pollTimer);
          observer.disconnect();
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
    },

    settings: async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      try {
        const settings = await api.get('/api/settings');
        container.innerHTML = '';

        const vodProfileEnabled = (Array.isArray(settings) ? settings : []).some(s => s.key === 'vod_profile_selector' && s.value === 'true');
        const vodProfileToggle = h('input', { type: 'checkbox', id: 'setting-vod-profile-selector' });
        vodProfileToggle.checked = vodProfileEnabled;
        vodProfileToggle.onchange = async function() {
          vodProfileToggle.disabled = true;
          try {
            await api.put('/api/settings', { vod_profile_selector: vodProfileToggle.checked ? 'true' : 'false' });
            toast.success('Setting saved');
          } catch (err) {
            toast.error(err.message);
            vodProfileToggle.checked = !vodProfileToggle.checked;
          }
          vodProfileToggle.disabled = false;
        };

        container.appendChild(h('div', { className: 'table-container' },
          h('div', { className: 'table-header' }, h('h3', null, 'Player Settings')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:10px' },
              vodProfileToggle,
              h('label', { for: 'setting-vod-profile-selector', style: 'cursor:pointer;margin:0' }, 'Show profile selector in VOD player'),
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'When enabled, the VOD player shows a dropdown to switch between stream profiles for testing transcoding. When disabled, Browser profile is always used.'),
          ),
        ));

        const currentHWAccel = ((Array.isArray(settings) ? settings : []).find(s => s.key === 'default_hwaccel') || {}).value || 'none';
        const hwaccelSelect = h('select', { id: 'setting-default-hwaccel', style: 'padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg-card); color: var(--text-primary); font-size: 14px' },
          h('option', { value: 'none' }, 'Software (none)'),
          h('option', { value: 'qsv' }, 'Intel QSV'),
          h('option', { value: 'nvenc' }, 'NVIDIA NVENC'),
          h('option', { value: 'vaapi' }, 'VA-API'),
          h('option', { value: 'videotoolbox' }, 'VideoToolbox'),
        );
        hwaccelSelect.value = currentHWAccel;
        hwaccelSelect.onchange = async function() {
          hwaccelSelect.disabled = true;
          try {
            await api.put('/api/settings', { default_hwaccel: hwaccelSelect.value });
            toast.success('Hardware acceleration updated');
          } catch (err) {
            toast.error(err.message);
            hwaccelSelect.value = currentHWAccel;
          }
          hwaccelSelect.disabled = false;
        };

        const currentVideoCodec = ((Array.isArray(settings) ? settings : []).find(s => s.key === 'default_video_codec') || {}).value || 'copy';
        const videoCodecSelect = h('select', { id: 'setting-default-video-codec', style: 'padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg-card); color: var(--text-primary); font-size: 14px' },
          h('option', { value: 'copy' }, 'Passthrough (copy)'),
          h('option', { value: 'h264' }, 'H.264'),
          h('option', { value: 'h265' }, 'H.265 / HEVC'),
          h('option', { value: 'av1' }, 'AV1'),
        );
        videoCodecSelect.value = currentVideoCodec;
        videoCodecSelect.onchange = async function() {
          videoCodecSelect.disabled = true;
          try {
            await api.put('/api/settings', { default_video_codec: videoCodecSelect.value });
            toast.success('Video codec updated');
          } catch (err) {
            toast.error(err.message);
            videoCodecSelect.value = currentVideoCodec;
          }
          videoCodecSelect.disabled = false;
        };

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'Encoding Defaults')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:10px;margin-bottom:12px' },
              h('label', { for: 'setting-default-hwaccel', style: 'margin:0;min-width:160px' }, 'Hardware acceleration:'),
              hwaccelSelect,
            ),
            h('div', { style: 'display:flex;align-items:center;gap:10px' },
              h('label', { for: 'setting-default-video-codec', style: 'margin:0;min-width:160px' }, 'Video codec:'),
              videoCodecSelect,
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'System-wide encoding defaults. Any stream profile set to "Global Default" uses these values. Set hardware acceleration to match your GPU. Video codec controls the output format — copy passes through the source codec without re-encoding.'),
          ),
        ));

        const dlnaEnabled = (Array.isArray(settings) ? settings : []).some(s => s.key === 'dlna_enabled' && s.value === 'true');
        const dlnaToggle = h('input', { type: 'checkbox', id: 'setting-dlna-enabled' });
        dlnaToggle.checked = dlnaEnabled;
        dlnaToggle.onchange = async function() {
          dlnaToggle.disabled = true;
          try {
            await api.put('/api/settings', { dlna_enabled: dlnaToggle.checked ? 'true' : 'false' });
            toast.success('Setting saved');
          } catch (err) {
            toast.error(err.message);
            dlnaToggle.checked = !dlnaToggle.checked;
          }
          dlnaToggle.disabled = false;
        };

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'DLNA Server')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:10px' },
              dlnaToggle,
              h('label', { for: 'setting-dlna-enabled', style: 'cursor:pointer;margin:0' }, 'Enable DLNA MediaServer'),
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'When enabled, TVProxy advertises as a DLNA MediaServer on the network. DLNA clients (e.g. VLC, 4XVR) can discover and browse channels. Changes take effect within 30 seconds.'),
          ),
        ));

        const debugEnabled = (Array.isArray(settings) ? settings : []).some(s => s.key === 'debug_enabled' && s.value === 'true');
        const debugToggle = h('input', { type: 'checkbox', id: 'setting-debug-enabled' });
        debugToggle.checked = debugEnabled;
        debugToggle.onchange = async function() {
          debugToggle.disabled = true;
          try {
            await api.put('/api/settings', { debug_enabled: debugToggle.checked ? 'true' : 'false' });
            toast.success('Setting saved');
          } catch (err) {
            toast.error(err.message);
            debugToggle.checked = !debugToggle.checked;
          }
          debugToggle.disabled = false;
        };

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'Debug Mode')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:10px' },
              debugToggle,
              h('label', { for: 'setting-debug-enabled', style: 'cursor:pointer;margin:0' }, 'Enable debug logging'),
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'When enabled, all HTTP requests are logged (including static files, VOD status polls, and DLNA pings). DLNA SOAP actions, proxy request headers, and client detection rule evaluation are also logged. When disabled, noisy requests are suppressed for cleaner logs.'),
          ),
        ));

        const currentTmdbKey = ((Array.isArray(settings) ? settings : []).find(s => s.key === 'tmdb_api_key') || {}).value || '';
        const tmdbInput = h('input', { type: 'text', id: 'setting-tmdb-key', value: currentTmdbKey, placeholder: 'Enter TMDB API key', style: 'padding:6px 10px;border-radius:6px;border:1px solid var(--border);background:var(--bg-card);color:var(--text-primary);font-size:14px;width:300px' });
        const tmdbSaveBtn = h('button', { className: 'btn btn-sm btn-primary', style: 'margin-left:8px', onClick: async function() {
          tmdbSaveBtn.disabled = true;
          try {
            await api.put('/api/settings', { tmdb_api_key: tmdbInput.value.trim() });
            toast.success('TMDB API key saved');
          } catch(err) {
            toast.error(err.message);
          }
          tmdbSaveBtn.disabled = false;
        }}, 'Save');

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'TMDB Integration')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:8px' },
              h('label', { for: 'setting-tmdb-key', style: 'margin:0;min-width:80px' }, 'API Key:'),
              tmdbInput,
              tmdbSaveBtn,
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'Free API key from themoviedb.org. Enables programme artwork (backdrops, posters) and rich metadata in the EPG guide. Sign up at themoviedb.org/signup then go to Settings > API.'),
          ),
        ));

        const softResetBtn = h('button', { className: 'btn btn-danger', onClick: async () => {
          if (!confirm('Soft Reset will delete all channels, streams, EPG data, stream profiles, clients, and HDHR devices. M3U accounts, EPG sources, users, and settings will be preserved.\n\nAre you sure?')) return;
          softResetBtn.disabled = true;
          try {
            await api.post('/api/settings/soft-reset');
            toast.success('Soft reset complete');
          } catch (err) {
            toast.error(err.message);
          }
          softResetBtn.disabled = false;
        }}, 'Soft Reset');

        const hardResetBtn = h('button', { className: 'btn btn-danger', style: 'margin-left: 12px', onClick: async () => {
          if (!confirm('Hard Reset will delete ALL data and restore factory defaults. You will be logged out.\n\nAre you sure?')) return;
          if (!confirm('This cannot be undone. All users, accounts, channels, and settings will be permanently deleted.\n\nProceed with hard reset?')) return;
          hardResetBtn.disabled = true;
          try {
            await api.post('/api/settings/hard-reset');
            toast.success('Hard reset complete. Logging out...');
            setTimeout(() => auth.logout(), 1500);
          } catch (err) {
            toast.error(err.message);
            hardResetBtn.disabled = false;
          }
        }}, 'Hard Reset');

        var exportChannelsBtn = h('button', { className: 'btn btn-secondary', onClick: async () => {
          exportChannelsBtn.disabled = true;
          try {
            var resp = await fetch('/api/settings/export?scope=channels', { headers: { 'Authorization': 'Bearer ' + state.accessToken } });
            var blob = await resp.blob();
            var a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = 'tvproxy-channels.json';
            a.click();
            URL.revokeObjectURL(a.href);
          } catch (err) { toast.error(err.message); }
          exportChannelsBtn.disabled = false;
        }}, 'Export Channels');

        var exportFullBtn = h('button', { className: 'btn btn-secondary', style: 'margin-left: 8px', onClick: async () => {
          exportFullBtn.disabled = true;
          try {
            var resp = await fetch('/api/settings/export?scope=full', { headers: { 'Authorization': 'Bearer ' + state.accessToken } });
            var blob = await resp.blob();
            var a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = 'tvproxy-full.json';
            a.click();
            URL.revokeObjectURL(a.href);
          } catch (err) { toast.error(err.message); }
          exportFullBtn.disabled = false;
        }}, 'Export Full');

        var importFileInput = h('input', { type: 'file', accept: '.json', style: 'display:none' });
        importFileInput.addEventListener('change', async () => {
          var file = importFileInput.files[0];
          if (!file) return;
          try {
            var text = await file.text();
            var data = JSON.parse(text);
            var summary = [];
            if (data.channels) summary.push(data.channels.length + ' channels');
            if (data.channel_groups) summary.push(data.channel_groups.length + ' groups');
            if (data.stream_profiles) summary.push(data.stream_profiles.length + ' profiles');
            if (data.m3u_accounts) summary.push(data.m3u_accounts.length + ' accounts');
            if (data.epg_sources) summary.push(data.epg_sources.length + ' EPG sources');
            if (data.settings) summary.push(data.settings.length + ' settings');
            if (!confirm('Import ' + (summary.join(', ') || 'data') + '?\n\nExisting items with the same name will be skipped.')) {
              importFileInput.value = '';
              return;
            }
            var result = await api.post('/api/settings/import', data);
            toast.success('Import complete: ' + (result.imported || 0) + ' items imported');
            channelsCache.invalidate();
            channelGroupsCache.invalidate();
            streamsCache.invalidate();
            navigate('settings');
          } catch (err) { toast.error('Import failed: ' + err.message); }
          importFileInput.value = '';
        });
        var importBtn = h('button', { className: 'btn btn-primary', style: 'margin-left: 8px', onClick: () => importFileInput.click() }, 'Import');

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'Import / Export')),
          h('div', { style: 'padding: 16px' },
            h('p', { style: 'color: var(--text-muted); margin-bottom: 16px' },
              'Export your configuration as JSON. "Channels" exports channels, groups, and stream assignments. "Full" includes everything (profiles, clients, settings, accounts, EPG sources). Import merges with existing data — duplicates are skipped.'),
            h('div', null, exportChannelsBtn, exportFullBtn, importBtn, importFileInput),
          ),
        ));

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'Database Management')),
          h('div', { style: 'padding: 16px' },
            h('p', { style: 'color: var(--text-muted); margin-bottom: 16px' },
              'Soft Reset removes all derived data (channels, streams, EPG, profiles, clients, devices) but keeps your accounts, EPG sources, users, and settings. Hard Reset restores factory defaults — all data is deleted and default credentials are restored (admin/admin).'),
            h('div', null, softResetBtn, hardResetBtn),
          ),
        ));
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load settings: ' + err.message));
      }
    },

    'wireguard': async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading WireGuard...'));

      var pollTimer = null;
      var selectedProfileId = null;
      var contentDiv = null;

      function fmtBytes(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
        return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
      }

      function fmtDuration(since) {
        if (!since) return '-';
        var secs = Math.floor((Date.now() - new Date(since).getTime()) / 1000);
        if (secs < 0) secs = 0;
        var hrs = Math.floor(secs / 3600);
        var mins = Math.floor((secs % 3600) / 60);
        var s = secs % 60;
        if (hrs > 0) return hrs + 'h ' + mins + 'm ' + s + 's';
        if (mins > 0) return mins + 'm ' + s + 's';
        return s + 's';
      }

      function fmtRelative(ts) {
        if (!ts) return '-';
        var secs = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
        if (secs < 0) secs = 0;
        if (secs < 60) return secs + 's ago';
        if (secs < 3600) return Math.floor(secs / 60) + 'm ago';
        return Math.floor(secs / 3600) + 'h ago';
      }

      function stateColor(st) {
        if (st === 'connected') return 'var(--success)';
        if (st === 'connecting') return 'var(--info, #3b82f6)';
        if (st === 'error') return 'var(--danger)';
        return 'var(--text-muted)';
      }

      function stateLabel(st) {
        if (st === 'connected') return 'Connected';
        if (st === 'connecting') return 'Connecting...';
        if (st === 'disconnected') return 'Disconnected';
        if (st === 'error') return 'Error';
        return 'Unconfigured';
      }

      function renderActiveStatusPane(ps, profileName) {
        var card = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-left:3px solid var(--success);border-radius:8px;padding:16px;margin-bottom:20px;' });
        var st = ps.state || 'disconnected';

        if (st === 'error' && ps.error) {
          card.style.borderLeftColor = 'var(--danger)';
          card.appendChild(h('div', { style: 'color:var(--danger)' }, ps.error));
          return card;
        }

        if (st !== 'connected') {
          card.style.display = 'none';
          return card;
        }

        var grid = h('div', { style: 'display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:0.88em;color:var(--text-muted)' });
        function addStat(label, value) {
          grid.appendChild(h('span', { style: 'font-weight:500' }, label));
          grid.appendChild(h('span', null, value));
        }
        if (profileName) addStat('Profile', profileName);
        addStat('Exit IP', ps.exit_ip || 'Checking...');
        addStat('Session', fmtDuration(ps.connected_since));
        addStat('Last Handshake', fmtRelative(ps.last_handshake));
        addStat('TX', fmtBytes(ps.tx_bytes || 0));
        addStat('RX', fmtBytes(ps.rx_bytes || 0));
        if (ps.peer_endpoint) addStat('Peer', ps.peer_endpoint);
        card.appendChild(grid);
        return card;
      }

      function renderProfileLine(ps) {
        var row = h('div', { style: 'display:flex;align-items:center;gap:8px;padding:8px 0;' });
        row.appendChild(h('span', { style: 'font-weight:600;min-width:120px' }, ps.name || 'Unnamed'));
        var enabled = ps.state === 'connected' || ps.state === 'connecting';
        row.appendChild(h('span', { style: 'font-size:0.85em' }, enabled ? '\u2705 Enabled' : '\u26D4 Disabled'));
        if (enabled) {
          if (ps.healthy === true) row.appendChild(h('span', { style: 'font-size:0.85em;color:var(--success)' }, '\u2705 Passing'));
          else if (ps.healthy === false) row.appendChild(h('span', { style: 'font-size:0.85em;color:var(--danger)' }, '\u274C Failing'));
          else row.appendChild(h('span', { style: 'font-size:0.85em;color:var(--text-muted)' }, '\u23F3 Pending'));
        }
        return row;
      }

      function renderProfileForm(profile) {
        var form = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:8px;padding:16px;margin-bottom:16px;' });
        var isEdit = profile && profile.id;
        form.appendChild(h('h3', { style: 'margin:0 0 16px 0' }, isEdit ? 'Edit Profile' : 'New Profile'));

        var fields = [
          { key: 'name', label: 'Name', placeholder: 'e.g., US East, Switzerland' },
          { key: 'private_key', label: 'Private Key', type: 'password', placeholder: 'Base64-encoded WireGuard private key' },
          { key: 'address', label: 'Address', placeholder: '10.20.30.40/24' },
          { key: 'dns', label: 'DNS', placeholder: '1.1.1.1, 8.8.8.8' },
          { key: 'peer_public_key', label: 'Peer Public Key', placeholder: 'Base64-encoded peer public key' },
          { key: 'peer_endpoint', label: 'Peer Endpoint', placeholder: 'vpn.example.com:51820' },
          { key: 'route_hosts', label: 'Route Hosts', placeholder: 'Leave empty to route all traffic' },
          { key: 'healthcheck_url', label: 'Health Check URL', placeholder: 'https://example.com/stream.m3u' },
          { key: 'healthcheck_method', label: 'Health Check Method', placeholder: 'HEAD or GET' },
          { key: 'priority', label: 'Priority', placeholder: '0 = highest' },
        ];
        var inputs = {};
        var inputStyle = 'width:60%;padding:8px 10px;background:var(--bg-input);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text-primary);font-size:0.9em;box-sizing:border-box;max-width:100%;';
        if (window.innerWidth < 600) inputStyle = inputStyle.replace('width:60%', 'width:100%');

        fields.forEach(function(f) {
          var row = h('div', { style: 'margin-bottom:10px' });
          row.appendChild(h('label', { style: 'display:block;font-weight:500;margin-bottom:4px;font-size:0.9em' }, f.label));
          var input = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
          input.style.cssText = inputStyle;
          var val = profile ? (profile[f.key] || '') : '';
          if (f.key === 'private_key' && val === '***') { input.placeholder = 'Enter to change'; }
          else if (f.key === 'priority' && profile) { input.value = String(profile.priority || 0); }
          else { input.value = val; }
          inputs[f.key] = input;
          row.appendChild(input);
          form.appendChild(row);
        });

        var checkRow = h('div', { style: 'margin-bottom:16px;display:flex;align-items:center;gap:8px' });
        var enableCb = h('input', { type: 'checkbox' });
        if (profile && profile.is_enabled) enableCb.checked = true;
        checkRow.appendChild(enableCb);
        checkRow.appendChild(h('label', { style: 'font-weight:500;cursor:pointer' }, 'Enabled'));
        form.appendChild(checkRow);

        var btnRow = h('div', { style: 'display:flex;gap:8px' });
        var saveBtn = h('button', { className: 'btn btn-primary' }, isEdit ? 'Save' : 'Create');
        saveBtn.onclick = async function() {
          saveBtn.disabled = true;
          var body = {};
          fields.forEach(function(f) { body[f.key] = inputs[f.key].value.trim(); });
          body.is_enabled = enableCb.checked;
          body.priority = parseInt(body.priority) || 0;
          body.healthcheck_interval = 60;
          if (!body.healthcheck_method) body.healthcheck_method = 'HEAD';
          try {
            if (isEdit) {
              await api.put('/api/wireguard/profiles/' + profile.id, body);
              toast.success('Profile saved');
            } else {
              await api.post('/api/wireguard/profiles', body);
              toast.success('Profile created');
            }
            renderPage();
          } catch (err) { toast.error(err.message); }
          saveBtn.disabled = false;
        };
        btnRow.appendChild(saveBtn);

        if (isEdit) {
          var deleteBtn = h('button', { className: 'btn btn-danger' }, 'Delete');
          deleteBtn.onclick = async function() {
            if (!confirm('Delete profile "' + (profile.name || '') + '"?')) return;
            try {
              await api.del('/api/wireguard/profiles/' + profile.id);
              toast.success('Profile deleted');
              selectedProfileId = null;
              renderPage();
            } catch (err) { toast.error(err.message); }
          };
          btnRow.appendChild(deleteBtn);

          var reconnectBtn = h('button', { className: 'btn btn-secondary' }, 'Reconnect');
          reconnectBtn.onclick = async function() {
            reconnectBtn.disabled = true;
            try {
              await api.post('/api/wireguard/profiles/' + profile.id + '/reconnect');
              toast.success('Reconnecting...');
              renderPage();
            } catch (err) { toast.error(err.message); }
            reconnectBtn.disabled = false;
          };
          btnRow.appendChild(reconnectBtn);

          var activateBtn = h('button', { className: 'btn btn-secondary' }, 'Set Active');
          activateBtn.onclick = async function() {
            try {
              await api.post('/api/wireguard/profiles/' + profile.id + '/activate');
              toast.success('Activated');
              renderPage();
            } catch (err) { toast.error(err.message); }
          };
          btnRow.appendChild(activateBtn);
        }

        form.appendChild(btnRow);
        return form;
      }

      async function renderPage() {
        try {
          var [profiles, multiStatus] = await Promise.all([
            api.get('/api/wireguard/profiles'),
            api.get('/api/wireguard/multi/status')
          ]);

          container.innerHTML = '';

          var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:16px;flex-wrap:wrap;gap:8px' });
          header.appendChild(h('h2', { style: 'margin:0' }, 'WireGuard VPN'));

          var activeLabel = multiStatus.active_profile_name || 'None';
          var activeColor = multiStatus.active_profile_id ? 'var(--success)' : 'var(--text-muted)';
          var statusBadge = h('div', { style: 'display:flex;align-items:center;gap:6px' });
          statusBadge.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:' + activeColor + ';display:inline-block' }));
          statusBadge.appendChild(h('span', { style: 'font-weight:500;color:' + activeColor }, activeLabel));
          header.appendChild(statusBadge);
          statusBadgeRef = statusBadge;
          container.appendChild(header);

          var controls = h('div', { style: 'display:flex;align-items:center;gap:8px;margin-bottom:16px;flex-wrap:wrap' });
          var profileSelect = h('select', { style: 'padding:8px 10px;background:var(--bg-input);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text-primary);font-size:0.9em;' });
          profileSelect.appendChild(h('option', { value: '' }, '-- Select Profile --'));
          profiles.forEach(function(p) {
            var label = p.name + (p.id === multiStatus.active_profile_id ? ' \u2705' : '');
            var opt = h('option', { value: p.id }, label);
            if (p.id === selectedProfileId) opt.selected = true;
            profileSelect.appendChild(opt);
          });
          profileSelect.onchange = function() {
            selectedProfileId = profileSelect.value || null;
            if (selectedProfileId) stopPolling(); else startPolling();
            renderPage();
          };
          controls.appendChild(profileSelect);

          var addBtn = h('button', { className: 'btn btn-primary btn-sm' }, '+ Add Profile');
          addBtn.onclick = function() { selectedProfileId = 'new'; stopPolling(); renderPage(); };
          controls.appendChild(addBtn);
          container.appendChild(controls);

          var activeStatusDiv = h('div');
          if (multiStatus.active_profile_id) {
            var activePs = (multiStatus.profiles || []).find(function(p) { return p.id === multiStatus.active_profile_id; });
            if (activePs) activeStatusDiv.appendChild(renderActiveStatusPane(activePs, multiStatus.active_profile_name));
          }
          container.appendChild(activeStatusDiv);
          activeStatusRef = activeStatusDiv;

          contentDiv = h('div');

          selectedStatusRef = null;
          overviewRef = null;

          if (selectedProfileId === 'new') {
            contentDiv.appendChild(renderProfileForm(null));
          } else if (selectedProfileId) {
            var profile = profiles.find(function(p) { return p.id === selectedProfileId; });
            if (profile) {
              contentDiv.appendChild(renderProfileForm(profile));
            }
          } else if (profiles.length > 0) {
            overviewRef = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:8px;padding:8px 16px;margin-bottom:16px;' });
            (multiStatus.profiles || []).forEach(function(ps) {
              var line = renderProfileLine(ps);
              line.style.cursor = 'pointer';
              line.onclick = function() { selectedProfileId = ps.id; renderPage(); };
              overviewRef.appendChild(line);
            });
            contentDiv.appendChild(overviewRef);
          } else {
            contentDiv.appendChild(h('p', { style: 'color:var(--text-muted);text-align:center;padding:32px' }, 'No profiles configured. Click "+ Add Profile" to create one.'));
          }

          container.appendChild(contentDiv);

        } catch (err) {
          container.innerHTML = '';
          container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        }
      }

      var statusBadgeRef = null;
      var activeStatusRef = null;
      var overviewRef = null;
      var selectedStatusRef = null;

      async function pollStatus() {
        try {
          var multiStatus = await api.get('/api/wireguard/multi/status');
          if (statusBadgeRef) {
            var activeLabel = multiStatus.active_profile_name || 'None';
            var activeColor = multiStatus.active_profile_id ? 'var(--success)' : 'var(--text-muted)';
            statusBadgeRef.innerHTML = '';
            statusBadgeRef.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:' + activeColor + ';display:inline-block' }));
            statusBadgeRef.appendChild(h('span', { style: 'font-weight:500;color:' + activeColor }, activeLabel));
          }
          if (activeStatusRef) {
            activeStatusRef.innerHTML = '';
            if (multiStatus.active_profile_id) {
              var activePs = (multiStatus.profiles || []).find(function(p) { return p.id === multiStatus.active_profile_id; });
              if (activePs) activeStatusRef.appendChild(renderActiveStatusPane(activePs, multiStatus.active_profile_name));
            }
          }
          if (overviewRef && !selectedProfileId) {
            overviewRef.innerHTML = '';
            (multiStatus.profiles || []).forEach(function(ps) {
              var line = renderProfileLine(ps);
              line.style.cursor = 'pointer';
              line.onclick = function() { selectedProfileId = ps.id; renderPage(); };
              overviewRef.appendChild(line);
            });
          }
        } catch (e) {}
      }

      function startPolling() {
        stopPolling();
        pollTimer = setInterval(pollStatus, 5000);
      }

      function stopPolling() {
        if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
      }

      await renderPage();
      startPolling();

      var observer = new MutationObserver(function() {
        if (!document.body.contains(container)) {
          stopPolling();
          observer.disconnect();
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
    },

    'now-playing': async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading activity...'));

      var pollTimer = null;

      function fmtDuration(startedAt) {
        var secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000);
        if (secs < 0) secs = 0;
        var hrs = Math.floor(secs / 3600);
        var mins = Math.floor((secs % 3600) / 60);
        var s = secs % 60;
        return hrs > 0 ? hrs + ':' + String(mins).padStart(2, '0') + ':' + String(s).padStart(2, '0')
                       : mins + ':' + String(s).padStart(2, '0');
      }

      function statusLabel(idleSecs) {
        return idleSecs < 5 ? 'Playing' : 'Idle ' + Math.floor(idleSecs) + 's';
      }

      function statusColor(idleSecs) {
        return idleSecs < 5 ? 'var(--success)' : 'var(--warning)';
      }

      function typeBadgeColor(type) {
        if (type === 'channel') return 'badge-primary';
        if (type === 'stream') return 'badge-info';
        if (type === 'recording') return 'badge-danger';
        if (type === 'session') return 'badge-success';
        return 'badge-warning';
      }

      function fmtRemaining(stopAt) {
        if (!stopAt) return null;
        var secs = Math.floor((new Date(stopAt).getTime() - Date.now()) / 1000);
        if (secs <= 0) return 'Ending...';
        var hrs = Math.floor(secs / 3600);
        var mins = Math.floor((secs % 3600) / 60);
        var s = secs % 60;
        return hrs > 0 ? hrs + ':' + String(mins).padStart(2, '0') + ':' + String(s).padStart(2, '0')
                       : mins + ':' + String(s).padStart(2, '0');
      }

      function renderViewers(viewers, recordings) {
        container.innerHTML = '';

        var recStopMap = {};
        if (recordings) {
          recordings.forEach(function(rec) { if (rec.stop_at) recStopMap[rec.session_id] = rec.stop_at; });
        }

        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:16px' });
        var sessionCount = viewers ? viewers.filter(function(v) { return v.type === 'session'; }).length : 0;
        var streamCount = viewers ? viewers.filter(function(v) { return v.type !== 'session'; }).length : 0;
        header.appendChild(h('h2', { style: 'margin:0' }, 'Activity'));
        var countsRow = h('div', { style: 'display:flex;gap:16px;font-size:0.95em;color:var(--text-muted)' });
        countsRow.appendChild(h('span', null, '\ud83d\udc64 ' + sessionCount + ' user' + (sessionCount !== 1 ? 's' : '')));
        countsRow.appendChild(h('span', null, '\u25B6 ' + streamCount + ' stream' + (streamCount !== 1 ? 's' : '')));
        header.appendChild(countsRow);
        container.appendChild(header);

        if (!viewers || viewers.length === 0) {
          container.appendChild(h('div', { style: 'text-align:center;padding:48px 16px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\u25B6'),
            h('p', { style: 'font-size:1.1em;margin:0' }, 'No active users or streams')
          ));
          return;
        }

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(min(380px,100%),1fr));gap:16px' });
        viewers.forEach(function(v) {
          var isRecording = v.type === 'recording';
          var cardBorder = isRecording ? 'border-left:3px solid var(--danger);' : '';
          var card = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:8px;padding:16px;display:flex;flex-direction:column;gap:8px;' + cardBorder });

          var nameRow = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;gap:8px' });
          var isSession = v.type === 'session';
          var displayName = isSession ? (v.username || 'Unknown User') : (v.channel_name || v.stream_name || 'Unknown');

          if (isSession) {
            nameRow.appendChild(h('span', { style: 'display:flex;align-items:center;gap:8px;font-size:1.15em;font-weight:600' },
              h('span', null, '\ud83d\udc64'),
              h('span', null, displayName),
            ));
          } else {
            nameRow.appendChild(h('span', { style: 'font-size:1.15em;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, displayName));
          }

          var statusDot = h('span', { style: 'display:inline-flex;align-items:center;gap:4px;font-size:0.85em;white-space:nowrap' });
          if (isRecording) {
            statusDot.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:var(--danger);animation:pulse 1.5s ease-in-out infinite' }));
            statusDot.appendChild(document.createTextNode('Recording'));
          } else if (isSession) {
            statusDot.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:var(--success)' }));
            statusDot.appendChild(document.createTextNode('Online'));
          } else {
            statusDot.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:' + statusColor(v.idle_secs) }));
            statusDot.appendChild(document.createTextNode(statusLabel(v.idle_secs)));
          }
          nameRow.appendChild(statusDot);
          card.appendChild(nameRow);

          if (v.username && !isSession) {
            card.appendChild(h('div', { style: 'display:flex;align-items:center;gap:6px;font-size:0.9em;color:var(--text-secondary)' },
              h('span', { style: 'font-weight:500' }, '\ud83d\udc64'),
              h('span', null, v.username),
            ));
          }

          var badgeRow = h('div', { style: 'display:flex;flex-wrap:wrap;gap:4px' });
          if (isSession) {
            badgeRow.appendChild(h('span', { className: 'badge badge-success' }, v.client_name || 'Dashboard'));
          } else {
            var typeLabel = isRecording ? '\u23FA Recording' : v.type.charAt(0).toUpperCase() + v.type.slice(1);
            badgeRow.appendChild(h('span', { className: 'badge ' + typeBadgeColor(v.type) }, typeLabel));
            if (v.profile_name) badgeRow.appendChild(h('span', { className: 'badge badge-secondary' }, v.profile_name));
            if (v.client_name) badgeRow.appendChild(h('span', { className: 'badge badge-success' }, v.client_name));
          }
          card.appendChild(badgeRow);

          var detailsGrid = h('div', { style: 'display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:0.88em;color:var(--text-muted)' });

          if (isRecording) {
            var stopAt = recStopMap[v.id];
            var remaining = fmtRemaining(stopAt);
            detailsGrid.appendChild(h('span', { style: 'font-weight:500' }, 'Remaining'));
            detailsGrid.appendChild(h('span', { title: stopAt ? fmtLocalDateTime(stopAt) : '' }, remaining || 'Unknown'));
          } else {
            detailsGrid.appendChild(h('span', { style: 'font-weight:500' }, 'IP'));
            detailsGrid.appendChild(h('span', null, v.remote_addr || '-'));
          }

          detailsGrid.appendChild(h('span', { style: 'font-weight:500' }, 'Duration'));
          detailsGrid.appendChild(h('span', null, fmtDuration(v.started_at)));

          if (v.user_agent) {
            var ua = v.user_agent.length > 60 ? v.user_agent.substring(0, 60) + '...' : v.user_agent;
            detailsGrid.appendChild(h('span', { style: 'font-weight:500' }, 'User Agent'));
            detailsGrid.appendChild(h('span', { style: 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap', title: v.user_agent }, ua));
          }

          card.appendChild(detailsGrid);

          if (isRecording && v.channel_id) {
            var stopBtn = h('button', {
              className: 'btn btn-sm btn-danger',
              style: 'align-self:flex-start;margin-top:4px;font-size:0.85em;padding:4px 12px;',
              onclick: async function() {
                stopBtn.disabled = true;
                stopBtn.textContent = 'Stopping...';
                try {
                  await api.del('/api/vod/record/' + v.channel_id);
                  await fetchAndRender();
                } catch(e) {
                  stopBtn.textContent = 'Failed';
                  setTimeout(function() { stopBtn.textContent = 'Stop Recording'; stopBtn.disabled = false; }, 2000);
                }
              }
            }, 'Stop Recording');
            card.appendChild(stopBtn);
          }

          grid.appendChild(card);
        });
        container.appendChild(grid);
      }

      async function fetchAndRender() {
        var [activity, scheduled] = await Promise.all([
          api.get('/api/activity'),
          api.get('/api/recordings/schedule').catch(function() { return []; })
        ]);
        var activeRecordings = (scheduled || []).filter(function(r) { return r.status === 'recording'; });
        renderViewers(activity, activeRecordings);
      }

      try {
        await fetchAndRender();
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load activity: ' + err.message));
        return;
      }

      pollTimer = setInterval(async function() {
        try { await fetchAndRender(); } catch (e) {}
      }, 5000);

      var observer = new MutationObserver(function() {
        if (!document.body.contains(container)) {
          if (pollTimer) clearInterval(pollTimer);
          observer.disconnect();
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
    },
  };

  var mobileNav = {
    _sidebar: null,
    _overlay: null,
    _open: false,
    _touchStartX: 0,
    _touchStartY: 0,
    _touchStartTime: 0,
    _swiping: false,

    open: function() {
      if (!mobileNav._sidebar) return;
      mobileNav._open = true;
      mobileNav._sidebar.classList.add('open');
      mobileNav._overlay.classList.add('open');
      document.body.style.overflow = 'hidden';
    },

    close: function() {
      if (!mobileNav._sidebar) return;
      mobileNav._open = false;
      mobileNav._sidebar.classList.remove('open');
      mobileNav._overlay.classList.remove('open');
      document.body.style.overflow = '';
    },

    toggle: function() {
      if (mobileNav._open) mobileNav.close();
      else mobileNav.open();
    },

    isMobile: function() {
      return window.innerWidth <= 768;
    },
  };

  window.addEventListener('resize', function() {
    if (!mobileNav.isMobile() && mobileNav._open) {
      mobileNav.close();
    }
  });

  document.addEventListener('touchstart', function(e) {
    if (!mobileNav.isMobile()) return;
    var touch = e.touches[0];
    mobileNav._touchStartX = touch.clientX;
    mobileNav._touchStartY = touch.clientY;
    mobileNav._touchStartTime = Date.now();
    mobileNav._swiping = false;
  }, { passive: true });

  document.addEventListener('touchmove', function(e) {
    if (!mobileNav.isMobile()) return;
    var touch = e.touches[0];
    var dx = touch.clientX - mobileNav._touchStartX;
    var dy = touch.clientY - mobileNav._touchStartY;
    if (Math.abs(dx) > Math.abs(dy) && Math.abs(dx) > 10) {
      mobileNav._swiping = true;
    }
  }, { passive: true });

  document.addEventListener('touchend', function(e) {
    if (!mobileNav.isMobile() || !mobileNav._swiping) return;
    var touch = e.changedTouches[0];
    var dx = touch.clientX - mobileNav._touchStartX;
    var elapsed = Date.now() - mobileNav._touchStartTime;
    if (elapsed > 500) return;
    if (!mobileNav._open && dx > 60 && mobileNav._touchStartX < 40) {
      mobileNav.open();
    } else if (mobileNav._open && dx < -60) {
      mobileNav.close();
    }
  }, { passive: true });

  function render() {
    if (!auth.isLoggedIn()) {
      if (state.currentPage === 'invite') {
        renderInvitePage();
        return;
      }
      renderLoginPage();
      return;
    }

    const isAdmin = state.user && state.user.is_admin;
    const adminPages = navItems.filter(n => n.adminOnly && n.id).map(n => n.id);
    if (!isAdmin && (adminPages.indexOf(state.currentPage) !== -1 || state.currentPage === 'dashboard')) {
      state.currentPage = 'channels';
    }

    const app = document.getElementById('app');
    app.innerHTML = '';

    const pageTitle = navItems.find(n => n.id === state.currentPage);
    const contentArea = h('div', { className: 'page-content' });

    var sidebar = renderSidebar();
    var overlay = h('div', { className: 'sidebar-overlay', onClick: function() { mobileNav.close(); } });
    mobileNav._sidebar = sidebar;
    mobileNav._overlay = overlay;
    mobileNav._open = false;

    var hamburger = h('button', {
      className: 'hamburger',
      onClick: function() { mobileNav.toggle(); },
      'aria-label': 'Menu',
    }, '\u2630');

    app.appendChild(overlay);
    app.appendChild(
      h('div', { className: 'layout' },
        sidebar,
        h('div', { className: 'main-content' },
          h('div', { className: 'topbar' },
            hamburger,
            h('h1', null, pageTitle ? pageTitle.label : 'Dashboard'),
          ),
          contentArea,
        ),
      )
    );

    const pageRenderer = pages[state.currentPage];
    if (pageRenderer) {
      pageRenderer(contentArea);
    } else {
      contentArea.appendChild(h('p', { style: 'color: var(--text-muted)' }, 'Page not found'));
    }
  }

  async function init() {
    const hash = window.location.hash.replace(/^#\/?/, '');
    if (hash.startsWith('invite/')) {
      state.currentPage = 'invite';
    }
    if (state.accessToken) {
      await auth.fetchUser();
    }
    render();
    if (auth.isLoggedIn()) {
      rebuildStreamNav();
      streamsCache.getAll();
      epgCache.getAll();
      logosCache.getAll();
      channelsCache.getAll();
      channelGroupsCache.getAll();
    }
  }

  document.addEventListener('keydown', function(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
      e.preventDefault();
      var searchEl = document.querySelector('.table-header input[type="text"]');
      if (searchEl) { searchEl.focus(); searchEl.select(); }
      return;
    }
    if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      var active = document.activeElement;
      if (active && (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.tagName === 'SELECT')) return;
      e.preventDefault();
      var searchEl = document.querySelector('.table-header input[type="text"]');
      if (searchEl) { searchEl.focus(); searchEl.select(); }
      return;
    }
    if (e.key === 'Escape') {
      if (mobileNav._open) { mobileNav.close(); return; }
      var overlay = document.querySelector('.modal-overlay');
      if (overlay) { overlay.remove(); return; }
      var active = document.activeElement;
      if (active && active.tagName === 'INPUT') { active.value = ''; active.dispatchEvent(new Event('input')); active.blur(); }
    }
  });

  if (typeof window !== 'undefined') {
    window._testExports = { createDVRTracker: createDVRTracker };
  }

  init();
})();
