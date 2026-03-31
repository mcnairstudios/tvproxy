(function() {
  'use strict';

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
          if (this._etagEndpoint && this._etag) {
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
        api.get('/api/epg/now').catch(() => ({})),
      ]);
      channels.forEach(ch => {
        ch._now_playing = (ch.tvg_id && nowMap[ch.tvg_id]) || '';
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
    { section: 'Overview', adminOnly: true },
    { id: 'dashboard', label: 'Dashboard', icon: '\u2302', tip: 'Overview of your TVProxy system status', adminOnly: true },
    { section: 'Sources', adminOnly: true },
    { id: 'm3u-accounts', label: 'M3U Accounts', icon: '\u2630', tip: 'Add your SAT>IP or IPTV source M3U files', adminOnly: true },
    { id: 'satip-sources', label: 'SAT>IP Sources', icon: '\ud83d\udce1', tip: 'Scan MiniSAT>IP devices for channels', adminOnly: true },
    { id: 'epg-sources', label: 'EPG Sources', icon: '\ud83d\udcc5', tip: 'Manage XMLTV EPG data sources for programme guides', adminOnly: true },
    { section: 'Channels' },
    { id: 'channels', label: 'Channels', icon: '\ud83d\udcfa', tip: 'Define your custom channels and assign streams and EPG data' },
    { id: 'epg-guide', label: 'EPG Guide', icon: '\ud83d\udcf0', tip: 'TV programme guide grid for your channels' },
    { section: 'Configuration', adminOnly: true },
    { id: 'stream-profiles', label: 'Stream Profiles', icon: '\ud83d\udd27', tip: 'Configure transcoding profiles for stream processing', adminOnly: true },
    { id: 'hdhr-devices', label: 'HDHR Devices', icon: '\ud83d\udce1', tip: 'Virtual HDHomeRun devices for Plex, Jellyfin, and Emby', adminOnly: true },
    { id: 'clients', label: 'Client Detection', icon: '\ud83d\udd0d', tip: 'Auto-detect players by HTTP headers and assign stream profiles', adminOnly: true },
    { id: 'logos', label: 'Logos', icon: '\ud83d\uddbc', tip: 'Saved channel logos for quick reuse', adminOnly: true },
    { section: 'Streams' },
    { id: 'recordings', label: 'Recordings', icon: '\u23FA', tip: 'View active and completed recordings' },
    { section: 'System', adminOnly: true },
    { id: 'now-playing', label: 'Now Playing', icon: '\u25B6', tip: 'Active streams and viewers', adminOnly: true },
    { id: 'users', label: 'Users', icon: '\ud83d\udc65', tip: 'Manage admin and user accounts', adminOnly: true },
    { id: 'settings', label: 'Settings', icon: '\u2699', tip: 'Core application settings', adminOnly: true },
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

  function buildEpgGuidePage() {
    const HOUR_WIDTH = 240; // px per hour
    const PX_PER_MIN = HOUR_WIDTH / 60;
    const CHANNEL_COL = 270;
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
          programsHtml += '<div class="' + cls + '" style="left:' + leftPx + 'px;width:' + widthPx + 'px" title="' + tooltip + '">' +
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
          '<div class="epg-channel" data-chid="' + esc(String(ch.id)) + '" data-tvgid="' + esc(ch.tvg_id || '') + '" data-chname="' + esc(ch.name) + '">' +
            '<span class="epg-channel-num">' + channelCounter + '</span>' +
            logoHtml +
            '<span class="epg-channel-name">' + esc(ch.name) + '</span>' +
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
              api.post('/channel/' + ch.dataset.chid + '/record', body).catch(function() {
                recBtn.classList.remove('recording'); recBtn.disabled = false;
              });
            }
            return;
          }
          var ch = e.target.closest('.epg-channel');
          if (ch) {
            playChannelWithDVR(ch.dataset.chid, ch.dataset.chname, ch.dataset.tvgid || undefined);
            return;
          }
          var prog = e.target.closest('.epg-program');
          if (prog) {
            var row = prog.closest('.epg-row');
            if (!row) return;
            ch = row.querySelector('.epg-channel');
            if (!ch) return;
            playChannelWithDVR(ch.dataset.chid, ch.dataset.chname, ch.dataset.tvgid || undefined);
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
          groupDisplay[i] = g || '(No Group)';
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
        let streams = groups[sortedGroups[gIdx]];
        if (searchTerm) {
          streams = streams.filter(function(s) { return matchesSearch(s.name.toLowerCase(), searchTerm); });
        }
        const tableEl = document.createElement('table');
        tableEl.className = 'stream-group-table';
        tableEl.innerHTML = '<tbody>' + buildStreamRows(streams).join('') + '</tbody>';
        details.appendChild(tableEl);
      }, true);

      groupsContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-sid]');
        if (!btn) return;
        if (btn.dataset.qadd) {
          quickAddChannel(btn.dataset.sid, btn.dataset.sname, btn.dataset.tvgid || '', btn.dataset.slogo || '');
          return;
        }
        if (btn.dataset.radioPlay) {
          playRadioInline(btn, btn.dataset.sid, btn.dataset.sname);
          return;
        }
        if (btn.dataset.radioRec) {
          return;
        }
        playStreamWithVODDetection(btn.dataset.sid, btn.dataset.sname, btn.dataset.tvgid || undefined);
      });

      var activeRadio = null;
      function playRadioInline(btn, streamID, name) {
        if (activeRadio && activeRadio.streamID === streamID) {
          if (activeRadio.audio.paused) {
            activeRadio.audio.play();
            btn.textContent = '\u23F9';
            btn.title = 'Stop';
          } else {
            stopRadio();
          }
          return;
        }
        if (activeRadio) stopRadio();
        btn.textContent = '\u23F3';
        btn.title = 'Connecting...';
        fetch('/stream/' + streamID + '/vod?profile=Browser', { method: 'POST', headers: { 'Authorization': 'Bearer ' + (state.accessToken || '') } })
          .then(function(r) { return r.json(); })
          .then(function(resp) {
            if (!resp.session_id) { btn.textContent = '\u25B6'; return; }
            var audio = new Audio('/vod/' + resp.session_id + '/stream');
            audio.volume = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
            audio.onplaying = function() { btn.textContent = '\u23F9'; btn.title = 'Stop'; };
            audio.onerror = function() { btn.textContent = '\u25B6'; btn.title = 'Play'; stopRadio(); };
            audio.play().catch(function() { btn.textContent = '\u25B6'; });
            activeRadio = { streamID: streamID, sessionID: resp.session_id, consumerID: resp.consumer_id, audio: audio, btn: btn };
          }).catch(function() { btn.textContent = '\u25B6'; });
      }
      function stopRadio() {
        if (!activeRadio) return;
        activeRadio.audio.pause();
        activeRadio.audio.removeAttribute('src');
        activeRadio.btn.textContent = '\u25B6';
        activeRadio.btn.title = 'Play';
        api.del('/vod/' + activeRadio.sessionID + (activeRadio.consumerID ? '?consumer_id=' + activeRadio.consumerID : '')).catch(function() {});
        activeRadio = null;
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
          actionHtml += '<button class="btn btn-primary btn-sm btn-icon" title="Add as Channel" style="font-size:16px" data-qadd="1" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '" data-slogo="' + esc(s.logo || '') + '">+</button>';
          if (isRadio) {
            actionHtml += '<button class="btn btn-sm btn-icon" title="Record" data-radio-rec="1" data-sid="' + s.id + '" style="font-size:12px">\u23FA</button>';
            actionHtml += '<button class="btn btn-secondary btn-sm btn-icon" title="Play" data-radio-play="1" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '">\u25B6</button>';
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
          groupDisplay[i] = sortedGroups[i] || '(No Group)';
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
    Object.keys(pages).forEach(k => { if (k.startsWith('streams-') || k.startsWith('satip-streams-')) delete pages[k]; });
    const idx = navItems.findIndex(n => n.section === 'Streams');
    if (idx === -1) return;
    const accountNavItems = accounts.map(a => ({
      id: 'streams-' + a.id,
      label: a.name,
      icon: '\u25b6',
      tip: 'Streams from ' + a.name,
    }));
    accounts.forEach(a => {
      pages['streams-' + a.id] = buildStreamGroupsPage('streams-' + a.id, function(s) { return s.m3u_account_id === a.id; });
    });
    const satipNavItems = satipSources.map(function(s) {
      var pageId = 'satip-streams-' + s.id;
      pages[pageId] = buildStreamGroupsPage(pageId, function(ss) { return ss.satip_source_id === s.id; });
      return { id: pageId, label: s.name, icon: '\ud83d\udce1', tip: 'Streams from ' + s.name };
    });
    navItems.splice(idx + 1, 0, ...accountNavItems, ...satipNavItems);
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
        h('span', { className: 'icon' }, item.icon),
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
      const [accounts, satipSources, channels, groups, epgSources, devices] = await Promise.all([
        api.get('/api/m3u/accounts').catch(() => []),
        api.get('/api/satip/sources').catch(() => []),
        channelsCache.getAll().catch(() => []),
        channelGroupsCache.getAll().catch(() => []),
        api.get('/api/epg/sources').catch(() => []),
        api.get('/api/hdhr/devices').catch(() => []),
      ]);

      const m3uStreamCount = accounts.reduce((sum, a) => sum + (a.stream_count || 0), 0);
      const satipStreamCount = satipSources.reduce((sum, s) => sum + (s.stream_count || 0), 0);

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
          const base = 'http://' + hostname + ':' + d.port;
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
          const inp = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
          inp.value = group[f.key] != null ? String(group[f.key]) : (f.default != null ? String(f.default) : '');
          inputs[f.key] = inp;
          formEl.appendChild(h('div', { className: 'form-group' }, h('label', null, f.label), inp));
        });
        showModal('Edit ' + (gb.singular || 'Group'), formEl, async () => {
          const body = {};
          (gb.fields || []).forEach(f => {
            body[f.key] = f.type === 'number' ? Number(inputs[f.key].value) : inputs[f.key].value;
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

  function isAudioOnly(streamTracks, streamGroup) {
    if (streamGroup && streamGroup.toLowerCase() === 'radio') return true;
    if (!streamTracks || !streamTracks.length) return false;
    return !streamTracks.some(function(t) { return t.category === 'video'; });
  }

  async function playStreamWithVODDetection(streamID, name, tvgId) {
    if (playInProgress) return;
    playInProgress = true;
    document.body.style.cursor = 'wait';
    try {
      if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
      var streamTracks = null;
      var streamGroup = null;
      if (streamsCache._data) {
        var s = streamsCache._data.find(function(s) { return s.id === streamID; });
        if (s) {
          if (s.tracks) streamTracks = s.tracks;
          if (s.group) streamGroup = s.group;
        }
      }
      var defaultAudio = defaultAudioIndex(streamTracks);
      var audioParam = defaultAudio > 0 ? '&audio=' + defaultAudio : '';
      let session = null;
      try {
        const resp = await fetch('/stream/' + streamID + '/vod?profile=Browser' + audioParam, { method: 'POST' }).then(r => r.json());
        if (resp.session_id) {
          session = { id: resp.session_id, consumer_id: resp.consumer_id, duration: resp.duration, container: resp.container, request_headers: resp.request_headers };
        }
      } catch(e) {}
      openVideoPlayer(name, '/stream/' + streamID + '?profile=Browser', tvgId, session, undefined, undefined, streamTracks, streamGroup);
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

  async function playChannelWithDVR(channelID, name, tvgId) {
    if (playInProgress) return;
    playInProgress = true;
    document.body.style.cursor = 'wait';
    try {
      if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
      let session = null;
      try {
        const resp = await fetch('/channel/' + channelID + '/vod?profile=Browser', { method: 'POST' }).then(r => r.json());
        if (resp.session_id) {
          session = { id: resp.session_id, consumer_id: resp.consumer_id, duration: resp.duration, container: resp.container, request_headers: resp.request_headers };
        }
      } catch(e) {}
      openVideoPlayer(name, '/channel/' + channelID + '?profile=Browser', tvgId, session, channelID);
    } finally {
      playInProgress = false;
      document.body.style.cursor = '';
    }
  }

  function openVideoPlayer(title, url, tvgId, dvr, channelID, probeUrl, streamTracks, streamGroup) {
    if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
    const playerCtx = new AbortController();
    let pollInterval = null;
    let progInterval = null;
    let signalInterval = null;
    let signalData = null;
    let satipStreamUrl = null;
    let nowProgram = null;
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
      if (signalInterval) { clearInterval(signalInterval); signalInterval = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      if (shakaPlayer) {
        shakaPlayer.destroy();
        shakaPlayer = null;
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
    function startRecordingUI() {
      isRecording = true;
      recordBtn.style.color = '#e53935';
      recordBtn.title = 'Stop Recording';
    }
    function stopRecordingUI() {
      isRecording = false;
      recordBtn.style.color = '';
      recordBtn.title = 'Record';
    }
    recordBtn.onclick = function() {
      if (!dvr) return;
      if (isRecording) {
        api.del('/vod/' + dvr.id + '/recording').then(function() { stopRecordingUI(); }).catch(function() {});
      } else {
        api.post('/vod/' + dvr.id + '/recording').then(function() { startRecordingUI(); }).catch(function() {});
      }
    };

    var statsBtn = document.createElement('button');
    statsBtn.textContent = '\u2139';
    statsBtn.title = 'Stats';

    var closeBtn = document.createElement('button');
    closeBtn.textContent = '\u2715';
    closeBtn.title = 'Close';
    closeBtn.onclick = cleanup;

    var titleEl = document.createElement('span');
    titleEl.textContent = title;

    var floatBar = document.createElement('div');
    floatBar.style.cssText = 'position:absolute;top:0;left:0;right:0;display:flex;align-items:center;gap:8px;padding:8px 12px;background:linear-gradient(rgba(0,0,0,0.7),transparent);opacity:0;transition:opacity 0.2s;z-index:20;pointer-events:none;';
    var barBtns = [titleEl, recordBtn, statsBtn, closeBtn];
    titleEl.style.cssText = 'flex:1;color:#fff;font-size:14px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-shadow:0 1px 2px rgba(0,0,0,0.5);';
    var btnStyle = 'background:rgba(255,255,255,0.15);backdrop-filter:blur(8px);border:none;color:#fff;width:32px;height:32px;border-radius:50%;font-size:14px;cursor:pointer;pointer-events:auto;transition:background 0.15s;';
    recordBtn.style.cssText = btnStyle;
    statsBtn.style.cssText = btnStyle;
    closeBtn.style.cssText = btnStyle;
    barBtns.forEach(function(b) { floatBar.appendChild(b); });
    playerWrap.appendChild(floatBar);

    playerWrap.addEventListener('mouseenter', function() { floatBar.style.opacity = '1'; });
    playerWrap.addEventListener('mouseleave', function() { floatBar.style.opacity = '0'; });

    var statsOverlay = document.createElement('div');
    statsOverlay.style.cssText = 'display:none;position:absolute;top:8px;left:8px;background:rgba(0,0,0,0.8);color:#fff;padding:10px 12px;border-radius:6px;font-size:11px;font-family:monospace;line-height:1.6;z-index:100;max-height:80%;overflow-y:auto;pointer-events:none;';
    statsBtn.onclick = function() { statsOverlay.style.display = statsOverlay.style.display === 'none' ? 'block' : 'none'; };
    playerWrap.appendChild(statsOverlay);
    modal.appendChild(playerWrap);

    var statusEl = document.createElement('span');
    statusEl.style.cssText = 'background:rgba(255,255,255,0.15);backdrop-filter:blur(8px);color:#fff;font-size:11px;padding:4px 10px;border-radius:16px;pointer-events:none;white-space:nowrap;';
    statusEl.textContent = 'Idle';
    floatBar.insertBefore(statusEl, closeBtn);

    overlay.appendChild(modal);
    overlay.onclick = function(e) { if (e.target === overlay) cleanup(); };
    document.body.appendChild(overlay);

    var streamSrc = dvr ? '/vod/' + dvr.id + '/dash/manifest.mpd' : url;

    var savedVol = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
    videoEl.volume = savedVol;
    videoEl.addEventListener('volumechange', function() { localStorage.setItem('tvproxy_volume', String(videoEl.volume)); });

    var shakaPlayer = null;
    if (typeof shaka !== 'undefined') {
      shaka.polyfill.installAll();
      shakaPlayer = new shaka.Player();
      shakaPlayer.configure({
        streaming: {
          bufferingGoal: 10,
          rebufferingGoal: 2,
          bufferBehind: 30,
          retryParameters: { maxAttempts: 5, baseDelay: 500 }
        }
      });
      shakaPlayer.attach(videoEl).then(function() {
        var ui = new shaka.ui.Overlay(shakaPlayer, playerWrap, videoEl);
        ui.configure({
          addBigPlayButton: false,
          fadeDelay: 2,
          controlPanelElements: ['play_pause', 'time_and_duration', 'spacer', 'mute', 'volume', 'fullscreen'],
          overflowMenuButtons: []
        });
        return shakaPlayer.load(streamSrc).then(function() {
          videoEl.play().catch(function() {});
        });
      }).catch(function(e) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Errored';
        statusEl.style.cursor = 'pointer';
        statusEl.style.pointerEvents = 'auto';
        statusEl.title = e.message || String(e);
        statusEl.onclick = function() { alert('Player error:\n\n' + (e.detail || e.message || e)); };
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
      if (shakaPlayer) {
        shakaPlayer.load(streamSrc).catch(function() {});
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
      statusEl.textContent = state + buildStatusSuffix();
    }

    function fetchNowPlaying() {
      if (!tvgId || playerCtx.signal.aborted) return;
      api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(function(program) {
        if (program && program.title) {
          nowProgram = program;
          updateStatusText();
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
            statusEl.onclick = function() { alert('Source error:\n\n' + st.error); };
            clearInterval(pollInterval);
            return;
          }
          if (dvrTracker) dvrTracker.updateBuffered(st.buffered);
          if (isLive && st.duration > 0 && !channelID) {
            isLive = false;
            if (dvrTracker) dvrTracker.setDuration(st.duration);
            dvr.duration = st.duration;
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
          }
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

    function updateStats() {
      if (playerCtx.signal.aborted || statsOverlay.style.display === 'none') return;
      var vi = probeData && probeData.video ? probeData.video : null;
      var at = probeData && probeData.audio_tracks ? probeData.audio_tracks : [];
      var activeAudio = at.length > 0 ? at[currentAudioIndex] || at[0] : null;
      var res = videoEl && videoEl.videoWidth ? videoEl.videoWidth + 'x' + videoEl.videoHeight : null;
      var buf = videoEl && videoEl.buffered.length > 0 ? (videoEl.buffered.end(0) - videoEl.currentTime).toFixed(1) + 's' : '0s';

      var left = [];
      if (res) left.push(res);
      if (vi) {
        left.push(esc(vi.codec) + (vi.profile ? ' (' + esc(vi.profile) + ')' : ''));
        if (vi.fps) left.push(esc(vi.fps) + ' fps');
        if (vi.bit_rate) left.push((parseInt(vi.bit_rate) / 1000).toFixed(0) + ' kbps');
        if (vi.field_order && vi.field_order !== 'unknown' && vi.field_order !== 'progressive') left.push(esc(vi.field_order));
        if (vi.pix_fmt) left.push(esc(vi.pix_fmt));
        if (vi.color_space && vi.color_space !== 'unknown') left.push(esc(vi.color_space));
      }
      if (activeAudio) {
        var aLabel = esc(activeAudio.codec || '?');
        if (activeAudio.language) aLabel += ' [' + esc(activeAudio.language) + ']';
        if (activeAudio.channels) aLabel += ' ' + activeAudio.channels + 'ch';
        left.push(aLabel);
      }
      left.push('buf ' + esc(buf));
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

    if (probeUrl) {
      api.get(probeUrl).then(function(pd) {
        if (pd) probeData = { video: pd.video || null, audio_tracks: pd.audio_tracks || [], duration: pd.duration || 0, profile: '' };
      }).catch(function() {});
    }
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

    // Match in priority order: exact tvg_id → exact name → stripped name (prefer same HD/SD suffix)
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

    // Build EPG autocomplete widget
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
        ],
      },
      columns: [
        { key: 'logo', label: '', thStyle: 'width:110px;padding-right:0;text-align:center', tdStyle: 'padding-right:0;text-align:center', render: item => {
          var copyUrl = function() {
            var url = window.location.origin + '/channel/' + item.id + '?profile=Copy';
            navigator.clipboard.writeText(url).then(function() { toast.success('Copied!'); }).catch(function() { toast.error('Copy failed'); });
          };
          if (item.logo) {
            var img = h('img', { src: item.logo, style: 'max-width:100px;max-height:40px;object-fit:contain;border-radius:2px;vertical-align:middle;cursor:pointer;' });
            img.onclick = copyUrl;
            return img;
          }
          var link = h('span', { style: 'cursor:pointer;font-size:18px;' }, '\uD83D\uDD17');
          link.onclick = copyUrl;
          return link;
        }},
        { key: 'name', label: 'Name', render: item => {
          const span = h('span', null, item.name);
          if (item.fail_count > 0) {
            span.appendChild(h('span', { className: 'play-fail-badge' }, '! ' + item.fail_count));
          }
          return span;
        }},
        { key: '_now_playing', label: 'Now Playing', tdStyle: 'font-weight:normal;color:var(--text-secondary);font-size:13px', render: item =>
          item._now_playing ? h('span', null, item._now_playing) : h('span', { style: 'color:var(--text-muted)' }, '-')
        },
        { key: 'is_enabled', label: 'Status', thStyle: 'width:80px', render: item =>
          h('span', { className: 'badge ' + (item.is_enabled ? 'badge-success' : 'badge-danger') }, item.is_enabled ? 'Enabled' : 'Disabled')
        },
      ],
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
      rowActions: (item, reload, openFormFn) => [
        { label: 'Play', icon: '\u25B6', handler: () => playChannelWithDVR(item.id, item.name, item.tvg_id || undefined) },
        { label: 'Record', icon: '\u23FA', handler: async () => {
          try {
            await api.post('/channel/' + item.id + '/record', { program_title: item.name, channel_name: item.name });
            toast.success('Recording started: ' + item.name);
          } catch (err) {
            toast.error('Record failed: ' + err.message);
          }
        }},
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

    'channel-groups': buildCrudPage({
      title: 'Channel Groups',
      singular: 'Channel Group',
      apiPath: '/api/channel-groups',
      create: true,
      update: true,
      onChange: () => channelGroupsCache.invalidate(),
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'sort_order', label: 'Sort Order' },
      ],
      fields: [
        { key: 'name', label: 'Group Name', placeholder: 'Entertainment' },
        { key: 'sort_order', label: 'Sort Order', type: 'number', default: 0 },
      ],
    }),

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
      create: true,
      update: true,
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
            h('th', null, 'Name'), h('th', null, 'Priority'), h('th', null, 'Match Rules'),
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
          table.innerHTML = '<thead><tr><th>Title</th><th>Channel</th><th>Size</th><th>Date</th><th>Actions</th></tr></thead>';
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
              var fileUrl = basePath + '/stream?profile=Browser&token=' + encodeURIComponent(state.accessToken || '');
              var probeUrl = basePath + '/probe';
              openVideoPlayer(title, fileUrl, null, null, null, probeUrl);
            }}, '\u25B6 Play');
            var deleteBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm('Delete ' + title + '?')) return;
              await api.del(basePath);
              renderCompleted(completedDiv);
            }}, 'Delete');
            actions.appendChild(playBtn);
            actions.appendChild(deleteBtn);
            var tr = h('tr', null,
              h('td', null, title),
              h('td', null, channelName),
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
          var recordings = (allRecordings || []).filter(function(r) { return r.status === 'pending'; });
          scheduledDiv.innerHTML = '';
          if (recordings.length === 0) {
            scheduledDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No scheduled recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Channel</th><th>Program</th><th>Start</th><th>Stop</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var startStr = fmtLocalDateTime(rec.start_at);
            var stopStr = fmtLocalDateTime(rec.stop_at);
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var deleteBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm('Delete scheduled recording "' + (rec.program_title || '') + '"?')) return;
              await api.del('/api/recordings/schedule/' + rec.id);
              renderScheduled(scheduledDiv);
            }}, 'Delete');
            actions.appendChild(deleteBtn);
            var tr = h('tr', null,
              h('td', null, rec.channel_name),
              h('td', null, rec.program_title),
              h('td', { title: fmtUTC(rec.start_at) }, startStr),
              h('td', { title: fmtUTC(rec.stop_at) }, stopStr),
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

      container.innerHTML = '';

      var scheduledSection = h('div', { className: 'table-container' },
        h('div', { className: 'table-header' }, h('h3', null, 'Scheduled Recordings'))
      );
      var scheduledDiv = h('div');
      scheduledSection.appendChild(scheduledDiv);

      var completedSection = h('div', { className: 'table-container', style: 'margin-top: 16px;' },
        h('div', { className: 'table-header' }, h('h3', null, 'Completed Recordings'))
      );
      var completedDiv = h('div');
      completedSection.appendChild(completedDiv);

      container.appendChild(scheduledSection);
      container.appendChild(completedSection);

      renderScheduled(scheduledDiv);
      renderCompleted(completedDiv);

      pollTimer = setInterval(function() { renderScheduled(scheduledDiv); }, 5000);

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
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading WireGuard status...'));

      var pollTimer = null;
      var statusEl = null;
      var statusTextEl = null;
      var statsCardEl = null;
      var formFields = {};
      var enableCheckbox = null;
      var saveBtn = null;
      var currentState = 'unconfigured';

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

      function updateStatus(status) {
        var st = status.state || 'unconfigured';
        currentState = st;

        var color = stateColor(st);
        statusEl.innerHTML = '';
        statusEl.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:' + color + ';display:inline-block' }));
        if (st === 'connecting') {
          statusEl.appendChild(h('div', { className: 'spinner', style: 'width:14px;height:14px' }));
        }
        statusTextEl.textContent = stateLabel(st);
        statusTextEl.setAttribute('style', 'font-size:0.95em;font-weight:500;color:' + color);

        statsCardEl.innerHTML = '';
        var cardBase = 'background:var(--bg-card);border:1px solid var(--border);border-radius:8px;padding:16px;margin-bottom:20px;';
        if (st === 'error' && status.error) {
          statsCardEl.setAttribute('style', cardBase + 'display:block;border-left:3px solid var(--danger);');
          statsCardEl.appendChild(h('div', { style: 'color:var(--danger)' }, status.error));
          return;
        }

        if (st === 'connected') {
          statsCardEl.setAttribute('style', cardBase + 'display:block;border-left:3px solid var(--success);');

          var grid = h('div', { style: 'display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:0.88em;color:var(--text-muted)' });

          function addStat(label, value) {
            grid.appendChild(h('span', { style: 'font-weight:500' }, label));
            grid.appendChild(h('span', null, value));
          }

          addStat('Exit IP', status.exit_ip || 'Checking...');
          addStat('Session', fmtDuration(status.connected_since));
          addStat('Last Handshake', fmtRelative(status.last_handshake));
          addStat('TX', fmtBytes(status.tx_bytes || 0));
          addStat('RX', fmtBytes(status.rx_bytes || 0));
          if (status.peer_endpoint) addStat('Peer', status.peer_endpoint);

          statsCardEl.appendChild(grid);
          return;
        }

        statsCardEl.setAttribute('style', cardBase + 'display:none;');
      }

      function setFieldsDisabled(disabled) {
        Object.keys(formFields).forEach(function(key) {
          formFields[key].disabled = disabled;
        });
      }

      function clearErrors() {
        Object.keys(formFields).forEach(function(key) {
          var errEl = formFields[key]._errorEl;
          if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
        });
      }

      function showErrors(errors) {
        clearErrors();
        Object.keys(errors).forEach(function(key) {
          if (formFields[key] && formFields[key]._errorEl) {
            formFields[key]._errorEl.textContent = errors[key];
            formFields[key]._errorEl.style.display = 'block';
          }
        });
      }

      function hasRequiredFields() {
        var required = ['address', 'dns', 'peer_public_key', 'peer_endpoint'];
        for (var i = 0; i < required.length; i++) {
          if (!formFields[required[i]] || !formFields[required[i]].value.trim()) return false;
        }
        return true;
      }

      function updateButtonState() {
        var hasFields = hasRequiredFields();
        if (saveBtn) saveBtn.disabled = !hasFields;
        if (enableCheckbox) enableCheckbox.disabled = !hasFields && !enableCheckbox.checked;
      }

      async function doConnect() {
        clearErrors();
        var body = {
          private_key: formFields.private_key.value.trim(),
          address: formFields.address.value.trim(),
          dns: formFields.dns.value.trim(),
          peer_public_key: formFields.peer_public_key.value.trim(),
          peer_endpoint: formFields.peer_endpoint.value.trim(),
          route_hosts: '',
        };

        try {
          var resp = await fetch('/api/wireguard/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state.accessToken },
            body: JSON.stringify(body),
          });
          var data = await resp.json();

          if (resp.status === 422 && data.errors) {
            showErrors(data.errors);
            enableCheckbox.checked = false;
            return;
          }
          if (!resp.ok) {
            toast.error(data.error || 'Connection failed');
            enableCheckbox.checked = false;
            return;
          }

          enableCheckbox.checked = true;
          setFieldsDisabled(true);
          updateStatus(data);
          startPolling();
          toast.success('WireGuard connected');
        } catch (err) {
          toast.error(err.message);
          enableCheckbox.checked = false;
        }
      }

      async function doDisconnect() {
        clearErrors();
        try {
          var resp = await fetch('/api/wireguard/disconnect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state.accessToken },
          });
          var data = await resp.json();
          enableCheckbox.checked = false;
          setFieldsDisabled(false);
          updateStatus(data);
          stopPolling();
          toast.success('WireGuard disconnected');
        } catch (err) {
          toast.error(err.message);
        }
      }

      function startPolling() {
        stopPolling();
        pollTimer = setInterval(async function() {
          try {
            var status = await api.get('/api/wireguard/status');
            updateStatus(status);
          } catch (e) {}
        }, 5000);
      }

      function stopPolling() {
        if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
      }

      function buildForm(config) {
        var form = h('div', null);

        var heading = h('h3', { style: 'margin:0 0 16px 0' }, 'Configuration');
        form.appendChild(heading);

        var fields = [
          { key: 'private_key', label: 'Private Key', type: 'password', placeholder: 'Base64-encoded WireGuard private key' },
          { key: 'address', label: 'Address', placeholder: '10.20.30.40/24' },
          { key: 'dns', label: 'DNS', placeholder: '1.1.1.1, 8.8.8.8' },
          { key: 'peer_public_key', label: 'Peer Public Key', placeholder: 'Base64-encoded peer public key' },
          { key: 'peer_endpoint', label: 'Peer Endpoint', placeholder: 'vpn.example.com:51820' },
        ];

        fields.forEach(function(f) {
          var row = h('div', { style: 'margin-bottom:12px' });
          row.appendChild(h('label', { style: 'display:block;font-weight:500;margin-bottom:4px;font-size:0.9em' }, f.label));

          var input = h('input', { type: f.type || 'text', placeholder: f.placeholder || '' });
          input.style.cssText = 'width:60%;padding:8px 10px;background:var(--bg-input);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text-primary);font-size:0.9em;box-sizing:border-box;max-width:100%;';
          if (window.innerWidth < 600) input.style.width = '100%';

          var val = config[f.key] || '';
          if (f.key === 'private_key' && val === '***') {
            input.placeholder = 'Enter to change';
          } else {
            input.value = val;
          }

          input.addEventListener('input', updateButtonState);
          formFields[f.key] = input;
          row.appendChild(input);

          var errEl = h('div', { style: 'display:none;color:var(--danger);font-size:0.85em;margin-top:2px' });
          input._errorEl = errEl;
          row.appendChild(errEl);

          if (f.hint) {
            row.appendChild(h('div', { style: 'color:var(--text-muted);font-size:0.82em;margin-top:2px' }, f.hint));
          }

          form.appendChild(row);
        });

        var checkRow = h('div', { style: 'margin-bottom:16px;display:flex;align-items:center;gap:8px' });
        enableCheckbox = h('input', { type: 'checkbox' });
        enableCheckbox.addEventListener('change', function() {
          if (enableCheckbox.checked) {
            doConnect();
          } else {
            doDisconnect();
          }
        });
        checkRow.appendChild(enableCheckbox);
        checkRow.appendChild(h('label', { style: 'font-weight:500;cursor:pointer' }, 'Enable WireGuard VPN'));
        form.appendChild(checkRow);

        saveBtn = h('button', { className: 'btn btn-primary' }, 'Save & Connect');
        saveBtn.addEventListener('click', function() {
          enableCheckbox.checked = true;
          doConnect();
        });
        form.appendChild(saveBtn);

        var reconnectBtn = h('button', { className: 'btn btn-secondary', style: 'margin-left:8px;display:none' }, 'Reconnect');
        reconnectBtn.addEventListener('click', async function() {
          reconnectBtn.disabled = true;
          reconnectBtn.textContent = 'Reconnecting...';
          try {
            var resp = await fetch('/api/wireguard/reconnect', {
              method: 'POST',
              headers: { 'Authorization': 'Bearer ' + state.accessToken },
            });
            var data = await resp.json();
            if (!resp.ok) { toast.error(data.error || 'Reconnect failed'); return; }
            updateStatus(data);
            toast.success('WireGuard reconnected');
          } catch (err) { toast.error(err.message); }
          finally { reconnectBtn.disabled = false; reconnectBtn.textContent = 'Reconnect'; }
        });
        form.appendChild(reconnectBtn);

        var origUpdateStatus = updateStatus;
        updateStatus = function(status) {
          origUpdateStatus(status);
          reconnectBtn.style.display = (status.state === 'connected' || status.state === 'error') ? 'inline-block' : 'none';
        };

        return form;
      }

      try {
        var status = await api.get('/api/wireguard/status');
        container.innerHTML = '';

        var header = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;margin-bottom:16px' });
        header.appendChild(h('h2', { style: 'margin:0' }, 'WireGuard VPN'));
        var statusWrap = h('div', { style: 'display:flex;align-items:center;gap:6px' });
        statusEl = h('span', { style: 'display:inline-flex;align-items:center;gap:4px' });
        statusTextEl = h('span', { style: 'font-size:0.95em;font-weight:500' });
        statusWrap.appendChild(statusEl);
        statusWrap.appendChild(statusTextEl);
        header.appendChild(statusWrap);
        container.appendChild(header);

        statsCardEl = h('div', { style: 'display:none;background:var(--bg-card);border:1px solid var(--border);border-left:3px solid var(--success);border-radius:8px;padding:16px;margin-bottom:20px;' });
        container.appendChild(statsCardEl);

        updateStatus(status);

        var config = status.config || {};
        var formEl = buildForm(config);
        container.appendChild(formEl);

        var isConnected = status.state === 'connected' || status.state === 'connecting';
        if (isConnected) {
          enableCheckbox.checked = true;
          setFieldsDisabled(true);
          startPolling();
        }
        updateButtonState();

      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        return;
      }

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
        var count = viewers ? viewers.length : 0;
        header.appendChild(h('h2', { style: 'margin:0' }, 'Now Playing'));
        header.appendChild(h('span', { style: 'color:var(--text-muted);font-size:0.95em' }, count + ' active stream' + (count !== 1 ? 's' : '')));
        container.appendChild(header);

        if (!viewers || viewers.length === 0) {
          container.appendChild(h('div', { style: 'text-align:center;padding:48px 16px;color:var(--text-muted)' },
            h('div', { style: 'font-size:3em;margin-bottom:12px;opacity:0.4' }, '\u25B6'),
            h('p', { style: 'font-size:1.1em;margin:0' }, 'No active streams')
          ));
          return;
        }

        var grid = h('div', { style: 'display:grid;grid-template-columns:repeat(auto-fill,minmax(min(380px,100%),1fr));gap:16px' });
        viewers.forEach(function(v) {
          var isRecording = v.type === 'recording';
          var cardBorder = isRecording ? 'border-left:3px solid var(--danger);' : '';
          var card = h('div', { style: 'background:var(--bg-card);border:1px solid var(--border);border-radius:8px;padding:16px;display:flex;flex-direction:column;gap:8px;' + cardBorder });

          var nameRow = h('div', { style: 'display:flex;align-items:center;justify-content:space-between;gap:8px' });
          var displayName = v.channel_name || v.stream_name || 'Unknown';
          nameRow.appendChild(h('span', { style: 'font-size:1.15em;font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, displayName));

          var statusDot = h('span', { style: 'display:inline-flex;align-items:center;gap:4px;font-size:0.85em;white-space:nowrap' });
          if (isRecording) {
            statusDot.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:var(--danger);animation:pulse 1.5s ease-in-out infinite' }));
            statusDot.appendChild(document.createTextNode('Recording'));
          } else {
            statusDot.appendChild(h('span', { style: 'width:8px;height:8px;border-radius:50%;background:' + statusColor(v.idle_secs) }));
            statusDot.appendChild(document.createTextNode(statusLabel(v.idle_secs)));
          }
          nameRow.appendChild(statusDot);
          card.appendChild(nameRow);

          var badgeRow = h('div', { style: 'display:flex;flex-wrap:wrap;gap:4px' });
          var typeLabel = isRecording ? '\u23FA Recording' : v.type.charAt(0).toUpperCase() + v.type.slice(1);
          badgeRow.appendChild(h('span', { className: 'badge ' + typeBadgeColor(v.type) }, typeLabel));
          if (v.profile_name) badgeRow.appendChild(h('span', { className: 'badge badge-secondary' }, v.profile_name));
          if (v.client_name) badgeRow.appendChild(h('span', { className: 'badge badge-success' }, v.client_name));
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
          grid.appendChild(card);
        });
        container.appendChild(grid);
      }

      async function fetchAndRender() {
        var viewers = await api.get('/api/activity');
        var recordings = [];
        try { recordings = await api.get('/api/recordings', { cache: 'no-store' }) || []; } catch (e) {}
        renderViewers(viewers, recordings);
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

  // Test exports
  if (typeof window !== 'undefined') {
    window._testExports = { createDVRTracker: createDVRTracker };
  }

  init();
})();
