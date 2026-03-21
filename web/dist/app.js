(function() {
  'use strict';

  function createDVRTracker(isLive, duration) {
    var buffered = 0;
    var seekOffset = 0;
    var seeking = false;
    var dur = duration || 0;
    var live = isLive;

    return {
      getPos: function(videoCurrentTime) {
        return seekOffset + (videoCurrentTime || 0);
      },

      updateBuffered: function(b) { buffered = b; },
      getBuffered: function() { return buffered; },
      isSeeking: function() { return seeking; },
      getSeekOffset: function() { return seekOffset; },

      startSeek: function(videoCurrentTime) {
        if (seeking || buffered <= 0) return null;
        var pos = seekOffset + (videoCurrentTime || 0);
        var seekTime = Math.min(pos, buffered);
        seeking = true;
        seekOffset = seekTime;
        return seekTime;
      },

      seekTo: function(seekTime) {
        if (seeking) return null;
        if (seekTime > buffered) return null;
        seeking = true;
        seekOffset = seekTime;
        return seekTime;
      },

      completeSeek: function() { seeking = false; },

      setDuration: function(d) {
        if (d > 0) { dur = d; live = false; }
      },
      isLive: function() { return live; },

      reset: function() {
        seekOffset = 0;
        seeking = false;
      },

      getDisplay: function(videoCurrentTime) {
        var pos = seekOffset + (videoCurrentTime || 0);
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
    async request(method, path, body) {
      const headers = { 'Content-Type': 'application/json' };
      if (state.accessToken) {
        headers['Authorization'] = 'Bearer ' + state.accessToken;
      }
      const opts = { method, headers };
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

      const text = await resp.text();
      return text ? JSON.parse(text) : null;
    },

    get(path) { return this.request('GET', path); },
    post(path, body) { return this.request('POST', path, body); },
    put(path, body) { return this.request('PUT', path, body); },
    del(path) { return this.request('DELETE', path); },

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
    constructor({ loader, searchKeys, label, storageKey }) {
      this._loader = loader;
      this._searchKeys = searchKeys;
      this._storageKey = storageKey ? 'tvproxy_' + storageKey : null;
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
        this.state = 'ready';
        this.count = this._data.length;
        DataCache._notify();
        return true;
      } catch { return false; }
    }

    _saveToStorage() {
      if (!this._storageKey || !this._data) return;
      try { localStorage.setItem(this._storageKey, JSON.stringify(this._data)); } catch {}
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
          const fresh = await this._loader();
          this._data = fresh;
          this._buildIndex();
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
      this.state = 'idle';
      this.count = 0;
      if (this._storageKey) {
        try { localStorage.removeItem(this._storageKey); } catch {}
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
      epgData.forEach(e => { e._display_name = (nameMap[e.epg_source_id] || 'Unknown') + '/' + e.name; });
      return epgData;
    },
    searchKeys: ['_display_name', 'channel_id'],
    storageKey: 'epg',
  });

  const logosCache = new DataCache({
    label: 'Logos',
    loader: () => api.get('/api/logos'),
    searchKeys: ['name', 'url'],
    storageKey: 'logos',
  });

  const streamsCache = new DataCache({
    label: 'Streams',
    loader: async () => {
      const [streams, accounts] = await Promise.all([
        api.get('/api/streams'),
        api.get('/api/m3u/accounts').catch(() => []),
      ]);
      const nameMap = {};
      accounts.forEach(a => { nameMap[a.id] = a.name; });
      streams.forEach(s => { s._display_name = (nameMap[s.m3u_account_id] || 'Unknown') + '/' + s.name; });
      for (var k in streamGroupsCache) delete streamGroupsCache[k];
      return streams;
    },
    searchKeys: ['_display_name', 'group'],
    storageKey: 'streams',
  });

  function getNowPlaying(guide) {
    var now = Date.now();
    var result = {};
    var progs = guide.programs || {};
    Object.keys(progs).forEach(function(chId) {
      for (var i = 0; i < progs[chId].length; i++) {
        var p = progs[chId][i];
        if (new Date(p.start).getTime() <= now && new Date(p.stop).getTime() > now) {
          result[chId] = p.title;
          break;
        }
      }
    });
    return result;
  }

  const channelsCache = new DataCache({
    label: 'Channels',
    loader: async () => {
      const [channels, guide] = await Promise.all([
        api.get('/api/channels'),
        api.get('/api/epg/guide?hours=1').catch(() => ({})),
      ]);
      var nowMap = getNowPlaying(guide);
      channels.forEach(ch => {
        ch._now_playing = (ch.tvg_id && nowMap[ch.tvg_id]) || '';
      });
      return channels;
    },
    searchKeys: ['name', 'tvg_id'],
  });

  const channelGroupsCache = new DataCache({
    label: 'Groups',
    loader: () => api.get('/api/channel-groups'),
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
    { id: 'users', label: 'Users', icon: '\ud83d\udc65', tip: 'Manage admin and user accounts', adminOnly: true },
    { id: 'settings', label: 'Settings', icon: '\u2699', tip: 'Core application settings', adminOnly: true },
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

      function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

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

  function buildStreamGroupsPage(accountId) {
    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      let groups, sortedGroups, groupDisplay, groupSearch;

      if (streamGroupsCache[accountId]) {
        var c = streamGroupsCache[accountId];
        groups = c.groups;
        sortedGroups = c.sortedGroups;
        groupDisplay = c.groupDisplay;
        groupSearch = c.groupSearch;
      } else {
        let allStreams;
        try {
          var cached = await streamsCache.getAll();
          allStreams = cached.filter(function(s) { return s.m3u_account_id === accountId; });
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

        streamGroupsCache[accountId] = { groups: groups, sortedGroups: sortedGroups, groupDisplay: groupDisplay, groupSearch: groupSearch };
      }

      let searchTerm = '';
      let searchTimer = null;
      const rendered = Object.create(null); // tracks which groups have had their table built

      function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

      const summaryEl = h('h3', null, '');
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
          if (!matchesSearch(groupSearch[gIdx], searchTerm)) {
            streams = streams.filter(function(s) { return matchesSearch(s.name.toLowerCase(), searchTerm); });
          }
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
        playStreamWithVODDetection(btn.dataset.sid, btn.dataset.sname, btn.dataset.tvgid || undefined);
      });

      function buildStreamRows(streams) {
        const rows = [];
        for (let j = 0; j < streams.length; j++) {
          const s = streams[j];
          const logo = s.logo
            ? '<img class="stream-group-logo" src="' + esc(s.logo) + '" loading="lazy" alt="">'
            : '';
          rows.push('<tr><td>' + logo + '</td><td>' + esc(s.name) + '</td><td style="width:80px"><div class="actions-cell" style="justify-content:flex-end"><button class="btn btn-primary btn-sm btn-icon" title="Add as Channel" style="font-size:16px" data-qadd="1" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '" data-slogo="' + esc(s.logo || '') + '">+</button><button class="btn btn-secondary btn-sm btn-icon" title="Play" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '">\u25B6</button></div></td></tr>');
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
            if (matchesSearch(groupSearch[i], searchTerm)) {
            } else {
              streams = streams.filter(function(s) { return matchesSearch(s.name.toLowerCase(), searchTerm); });
              if (streams.length === 0) continue;
            }
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
    };
  }

  async function rebuildStreamNav() {
    const accounts = await api.get('/api/m3u/accounts').catch(() => []);
    navItems = navItems.filter(n => !n.id || !n.id.startsWith('streams-'));
    Object.keys(pages).forEach(k => { if (k.startsWith('streams-')) delete pages[k]; });
    const idx = navItems.findIndex(n => n.section === 'Streams');
    if (idx === -1) return;
    const accountNavItems = accounts.map(a => ({
      id: 'streams-' + a.id,
      label: a.name,
      icon: '\u25b6',
      tip: 'Streams from ' + a.name,
    }));
    navItems.splice(idx + 1, 0, ...accountNavItems);
    accounts.forEach(a => {
      pages['streams-' + a.id] = buildStreamGroupsPage(a.id);
    });
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
        onClick: () => navigate(item.id),
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
      const [accounts, channels, groups, epgSources, devices] = await Promise.all([
        api.get('/api/m3u/accounts').catch(() => []),
        channelsCache.getAll().catch(() => []),
        channelGroupsCache.getAll().catch(() => []),
        api.get('/api/epg/sources').catch(() => []),
        api.get('/api/hdhr/devices').catch(() => []),
      ]);

      const streamCount = accounts.reduce((sum, a) => sum + (a.stream_count || 0), 0);

      container.innerHTML = '';

      const cards = [
        { label: 'M3U Accounts', value: accounts.length, icon: '\u2630', page: 'm3u-accounts' },
        { label: 'Streams', value: streamCount, icon: '\u25b6', page: accounts.length ? 'streams-' + accounts[0].id : 'dashboard' },
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
          const actions = config.rowActions(item, reloadData);
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

      function openForm(item) {
        const isEdit = item !== null;
        const formEl = h('div');
        const fields = config.fields || [];
        const inputs = {};

        fields.forEach(field => {
          if (field.type === 'checkbox') {
            const checked = isEdit ? item[field.key] : (field.default || false);
            const cb = h('input', { type: 'checkbox', id: 'field-' + field.key });
            cb.checked = checked;
            inputs[field.key] = cb;
            formEl.appendChild(h('div', { className: 'form-check' }, cb, h('label', { for: 'field-' + field.key }, field.label)));
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

  async function playStreamWithVODDetection(streamID, name, tvgId) {
    if (playInProgress) return;
    playInProgress = true;
    document.body.style.cursor = 'wait';
    try {
      if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
      let session = null;
      try {
        const resp = await fetch('/stream/' + streamID + '/vod?profile=Browser', { method: 'POST' }).then(r => r.json());
        if (resp.session_id) {
          session = { id: resp.session_id, duration: resp.duration };
        }
      } catch(e) {}
      openVideoPlayer(name, '/stream/' + streamID + '?profile=Browser', tvgId, session);
    } finally {
      playInProgress = false;
      document.body.style.cursor = '';
    }
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
          session = { id: resp.session_id, duration: resp.duration };
        }
      } catch(e) {}
      openVideoPlayer(name, '/channel/' + channelID + '?profile=Browser', tvgId, session, channelID);
    } finally {
      playInProgress = false;
      document.body.style.cursor = '';
    }
  }

  function openVideoPlayer(title, url, tvgId, dvr, channelID) {
    if (activePlayerCleanup) { activePlayerCleanup(); activePlayerCleanup = null; }
    let mpegtsPlayer = null;
    let retryCount = 0;
    const MAX_RETRIES = 3;
    let retryTimeout = null;
    let statsInterval = null;
    let progInterval = null;
    let dvrPollInterval = null;
    let dvrPosInterval = null;
    let nowProgram = null;
    let currentContainer = '';
    let currentCodec = '';
    let stallTimeout = null;
    let isRecording = false;
    let activeSegmentID = null;
    let dragState = null;
    let pollFailures = 0;
    let audioSelect = null;
    let currentAudioIndex = 0;
    let profileSelect = null;
    let currentProfile = 'Browser';
    let probeData = null;
    const playerCtx = new AbortController();
    let isLive = !dvr || !dvr.duration;
    const dvrTracker = dvr ? createDVRTracker(isLive, dvr.duration) : null;

    function destroyPlayer() {
      if (mpegtsPlayer) {
        try { mpegtsPlayer.pause(); } catch(e) {}
        try { mpegtsPlayer.unload(); } catch(e) {}
        try { mpegtsPlayer.detachMediaElement(); } catch(e) {}
        try { mpegtsPlayer.destroy(); } catch(e) {}
        mpegtsPlayer = null;
      }
    }

    function clearDvrIntervals() {
      if (dvrPollInterval) { clearInterval(dvrPollInterval); dvrPollInterval = null; }
      if (dvrPosInterval) { clearInterval(dvrPosInterval); dvrPosInterval = null; }
    }

    function cleanup() {
      activePlayerCleanup = null;
      playerCtx.abort();
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      if (progInterval) { clearInterval(progInterval); progInterval = null; }
      clearDvrIntervals();
      if (stallTimeout) { clearTimeout(stallTimeout); stallTimeout = null; }
      destroyPlayer();
      document.removeEventListener('fullscreenchange', onFullscreenChange);
      document.removeEventListener('mousemove', onDocMouseMove);
      document.removeEventListener('mouseup', onDocMouseUp);
      if (dvr && !isRecording) {
        api.del('/vod/' + dvr.id).catch(() => {});
      }
      video.oncanplay = null;
      video.onerror = null;
      video.pause();
      video.removeAttribute('src');
      video.load();
      overlay.remove();
      if (channelID && state.currentPage === 'channels') {
        document.dispatchEvent(new CustomEvent('tvproxy-reload-page'));
      }
    }
    activePlayerCleanup = cleanup;

    function fmtTime(secs) {
      const h = Math.floor(secs / 3600);
      const m = Math.floor((secs % 3600) / 60);
      const s = Math.floor(secs % 60);
      return h > 0 ? h + ':' + String(m).padStart(2,'0') + ':' + String(s).padStart(2,'0')
                    : m + ':' + String(s).padStart(2,'0');
    }

    const overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.8);z-index:10000;display:flex;align-items:center;justify-content:center;';
    const modal = document.createElement('div');
    modal.style.cssText = 'background:var(--bg-card);border-radius:8px;padding:16px;max-width:800px;width:90%;position:relative;';

    const header = document.createElement('div');
    header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;margin-bottom:8px;';
    const titleEl = document.createElement('h3');
    titleEl.style.cssText = 'margin:0;color:#e0e0e0;font-size:16px;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;';
    titleEl.textContent = title;
    const hdrBtns = document.createElement('div');
    hdrBtns.style.cssText = 'display:flex;gap:6px;flex-shrink:0;';
    const recordBtn = document.createElement('button');
    recordBtn.className = 'btn btn-sm';
    recordBtn.title = 'Record';
    recordBtn.style.cssText = 'padding:4px 8px;line-height:0;';
    recordBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 215.11" width="24" height="10" fill="currentColor"><path fill-rule="nonzero" d="M0 83.78V41.11c0-11.31 4.62-21.6 12.06-29.05h.05C19.55 4.63 29.82 0 41.11 0h41.53v23.53H41.11c-4.84 0-9.24 1.98-12.43 5.15-3.17 3.21-5.15 7.61-5.15 12.43v42.67H0zm418.09 13.78h-20.34c-.14-1.7-.51-3.22-1.14-4.59-.61-1.37-1.45-2.56-2.52-3.55-1.07-1-2.35-1.77-3.87-2.31-1.51-.54-3.24-.81-5.17-.81-3.35 0-6.19.81-8.51 2.45-2.34 1.63-4.08 3.98-5.28 7.03-1.18 3.06-1.78 6.72-1.78 11 0 4.52.61 8.31 1.83 11.34 1.2 3.05 2.96 5.33 5.28 6.85 2.3 1.53 5.08 2.29 8.33 2.29 1.84 0 3.5-.22 4.97-.7 1.48-.47 2.75-1.15 3.83-2.05 1.08-.88 1.96-1.96 2.64-3.21.69-1.27 1.15-2.69 1.39-4.28l20.34.15c-.24 3.11-1.12 6.29-2.62 9.53-1.53 3.23-3.68 6.23-6.45 8.95-2.78 2.73-6.21 4.93-10.29 6.58-4.1 1.66-8.84 2.49-14.25 2.49-6.77 0-12.85-1.45-18.23-4.37-5.36-2.91-9.61-7.19-12.73-12.84-3.11-5.64-4.67-12.56-4.67-20.73 0-8.23 1.59-15.15 4.76-20.78 3.18-5.64 7.46-9.91 12.84-12.82 5.38-2.89 11.39-4.33 18.03-4.33 4.67 0 8.95.63 12.88 1.91 3.92 1.27 7.36 3.14 10.31 5.57 2.96 2.44 5.34 5.44 7.13 8.99 1.81 3.57 2.9 7.63 3.29 12.24zm-132.13 46.15V69.85h53.23v16.16h-33.17v12.7h30.43v16.14h-30.43v12.7h33.03v16.16h-53.09zm-67.79 0V69.85h31.88c5.49 0 10.27 1 14.39 3 4.11 2 7.31 4.87 9.59 8.61 2.29 3.76 3.42 8.26 3.42 13.49 0 5.3-1.16 9.75-3.52 13.38-2.33 3.63-5.62 6.37-9.83 8.23-4.23 1.84-9.14 2.78-14.77 2.78h-19.04v-15.59h15.01c2.35 0 4.36-.29 6.02-.88 1.68-.59 2.97-1.54 3.86-2.83.91-1.3 1.35-2.99 1.35-5.09 0-2.11-.44-3.84-1.35-5.16-.89-1.34-2.18-2.34-3.86-2.96-1.66-.65-3.67-.97-6.02-.97h-7.08v57.85h-20.05zm43.27-33.9 18.47 33.9h-21.78l-18.03-33.9h21.34zm-178.8 105.3H41.11c-11.29 0-21.58-4.63-29.03-12.08C4.64 195.59 0 185.31 0 174v-42.73h23.53V174c0 4.81 1.99 9.2 5.18 12.39 3.2 3.2 7.6 5.19 12.4 5.19h41.53v23.53zM488.47 83.78V41.11c0-4.82-1.98-9.22-5.17-12.41a17.464 17.464 0 0 0-12.41-5.17h-41.53V0h41.53c11.29 0 21.56 4.63 29 12.06h.05C507.38 19.51 512 29.8 512 41.11v42.67h-23.53zm-59.11 107.8h41.53c4.8 0 9.2-1.99 12.4-5.19 3.19-3.19 5.18-7.58 5.18-12.39v-42.73H512V174c0 11.31-4.64 21.59-12.08 29.03-7.45 7.45-17.74 12.08-29.03 12.08h-41.53v-23.53z"/><circle cx="138.03" cy="106.79" r="44.12"/></svg>';
    const recDot = recordBtn.querySelector('circle');
    let recFlash = null;
    function startRecordingUI() {
      isRecording = true;
      recDot.setAttribute('fill', '#e53935');
      recordBtn.title = 'Stop Recording';
      recFlash = recDot.animate([{ opacity: 1 }, { opacity: 0.3 }, { opacity: 1 }], { duration: 1200, iterations: Infinity });
    }
    function stopRecordingUI() {
      isRecording = false;
      activeSegmentID = null;
      recDot.removeAttribute('fill');
      recordBtn.title = 'Record';
      if (recFlash) { recFlash.cancel(); recFlash = null; }
    }
    async function switchProfile(profileName) {
      if (isRecording) {
        toast.error('Cannot switch profile while recording');
        if (profileSelect) profileSelect.value = currentProfile;
        return;
      }
      currentProfile = profileName;
      audioSelect = null;
      currentAudioIndex = 0;
      destroyPlayer();
      try {
        await api.del('/vod/' + dvr.id).catch(function() {});
        var resp;
        if (channelID) {
          resp = await fetch('/channel/' + channelID + '/vod?profile=' + encodeURIComponent(profileName), { method: 'POST' }).then(function(r) { return r.json(); });
        } else {
          var streamID = url.split('/stream/')[1];
          if (streamID) streamID = streamID.split('/')[0].split('?')[0];
          resp = await fetch('/stream/' + streamID + '/vod?profile=' + encodeURIComponent(profileName), { method: 'POST' }).then(function(r) { return r.json(); });
        }
        if (resp.session_id) {
          dvr = { id: resp.session_id, duration: resp.duration };
          if (dvrTracker) dvrTracker.reset();
        }
      } catch(e) {
        toast.error('Profile switch failed: ' + e.message);
      }
      startPlayback();
    }

    async function switchAudioTrack(trackIndex) {
      if (isRecording) {
        toast.error('Cannot switch audio while recording');
        if (audioSelect) audioSelect.value = currentAudioIndex;
        return;
      }
      currentAudioIndex = trackIndex;
      destroyPlayer();
      try {
        await api.del('/vod/' + dvr.id).catch(function() {});
        var audioParam = trackIndex > 0 ? '&audio=' + trackIndex : '';
        var resp;
        if (channelID) {
          resp = await fetch('/channel/' + channelID + '/vod?profile=' + encodeURIComponent(currentProfile) + audioParam, { method: 'POST' }).then(function(r) { return r.json(); });
        } else {
          var streamID = url.split('/stream/')[1];
          if (streamID) streamID = streamID.split('/')[0].split('?')[0];
          resp = await fetch('/stream/' + streamID + '/vod?profile=' + encodeURIComponent(currentProfile) + audioParam, { method: 'POST' }).then(function(r) { return r.json(); });
        }
        if (resp.session_id) {
          dvr = { id: resp.session_id, duration: resp.duration };
          if (dvrTracker) dvrTracker.reset();
        }
      } catch(e) {
        toast.error('Audio switch failed: ' + e.message);
      }
      startPlayback();
    }

    recordBtn.onclick = async function() {
      if (!dvr || !dvrTracker) return;
      if (isRecording) {
        try {
          await api.post('/vod/' + dvr.id + '/stop');
          stopRecordingUI();
        } catch(e) { toast.error('Stop recording failed: ' + e.message); }
        return;
      }
      var body = { program_title: nowProgram ? nowProgram.title : title, channel_name: title };
      if (nowProgram && nowProgram.stop) body.stop_at = new Date(nowProgram.stop).toISOString();
      body.start_offset = dvrTracker.getPos(video.currentTime);
      body.end_offset = dvrTracker.getBuffered();
      try {
        var resp = await api.post('/vod/' + dvr.id + '/record', body);
        if (resp && resp.segment) activeSegmentID = resp.segment.id;
        startRecordingUI();
      } catch(e) { toast.error('Record failed: ' + e.message); }
    };
    if (!dvr) recordBtn.style.display = 'none';
    const statsBtn = document.createElement('button');
    statsBtn.className = 'btn btn-sm'; statsBtn.title = 'Toggle stream statistics';
    statsBtn.style.cssText = 'padding:4px 8px;line-height:0;';
    statsBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 294 294" width="14" height="14" fill="currentColor"><path d="M279,250H15c-8.284,0-15,6.716-15,15s6.716,15,15,15h264c8.284,0,15-6.716,15-15S287.284,250,279,250z"/><path d="M30.5,228h47c5.247,0,9.5-4.253,9.5-9.5v-130c0-5.247-4.253-9.5-9.5-9.5h-47c-5.247,0-9.5,4.253-9.5,9.5v130C21,223.747,25.253,228,30.5,228z"/><path d="M123.5,228h47c5.247,0,9.5-4.253,9.5-9.5v-195c0-5.247-4.253-9.5-9.5-9.5h-47c-5.247,0-9.5,4.253-9.5,9.5v195C114,223.747,118.253,228,123.5,228z"/><path d="M216.5,228h47c5.247,0,9.5-4.253,9.5-9.5v-105c0-5.247-4.253-9.5-9.5-9.5h-47c-5.247,0-9.5,4.253-9.5,9.5v105C207,223.747,211.253,228,216.5,228z"/></svg>';
    const closeBtn = document.createElement('button');
    closeBtn.className = 'btn btn-danger btn-sm btn-icon-circle'; closeBtn.textContent = '\u2715'; closeBtn.title = 'Close'; closeBtn.onclick = cleanup;
    if (dvr) {
      api.get('/api/settings').then(function(allSettings) {
        var enabled = false;
        if (Array.isArray(allSettings)) {
          allSettings.forEach(function(s) {
            if (s.key === 'vod_profile_selector' && s.value === 'true') enabled = true;
          });
        }
        if (!enabled) return;
        profileSelect = document.createElement('select');
        profileSelect.className = 'btn btn-sm';
        profileSelect.title = 'Stream Profile';
        profileSelect.style.cssText = 'padding:2px 6px;font-size:12px;background:var(--bg-card);color:#e0e0e0;border:1px solid var(--border);border-radius:4px;cursor:pointer;max-width:140px;';
        var defaultOpt = document.createElement('option');
        defaultOpt.value = 'Browser';
        defaultOpt.textContent = 'Browser';
        profileSelect.appendChild(defaultOpt);
        profileSelect.value = currentProfile;
        profileSelect.onchange = function() { switchProfile(profileSelect.value); };
        hdrBtns.insertBefore(profileSelect, recordBtn);
        return api.get('/api/stream-profiles');
      }).then(function(profiles) {
        if (!profileSelect || !Array.isArray(profiles)) return;
        profileSelect.innerHTML = '';
        profiles.forEach(function(p) {
          if (p.is_system && p.name === 'Direct') return;
          var opt = document.createElement('option');
          opt.value = p.name;
          opt.textContent = p.name;
          profileSelect.appendChild(opt);
        });
        profileSelect.value = currentProfile;
      }).catch(function() {});
    }
    hdrBtns.appendChild(recordBtn);
    hdrBtns.appendChild(statsBtn);
    hdrBtns.appendChild(closeBtn);
    header.appendChild(titleEl);
    header.appendChild(hdrBtns);
    modal.appendChild(header);

    const videoWrap = document.createElement('div');
    videoWrap.style.cssText = 'position:relative;background:#000;border-radius:4px;overflow:hidden;';
    const video = document.createElement('video');
    video.style.cssText = 'width:100%;max-height:450px;display:block;';
    video.autoplay = true;
    video.volume = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
    video.addEventListener('volumechange', () => localStorage.setItem('tvproxy_volume', video.volume));

    const statsOverlay = document.createElement('div');
    statsOverlay.style.cssText = 'display:none;position:absolute;top:8px;left:8px;background:rgba(0,0,0,0.75);color:#fff;padding:8px 10px;border-radius:6px;font-size:11px;font-family:monospace;pointer-events:none;line-height:1.6;z-index:10;';
    statsBtn.onclick = () => { statsOverlay.style.display = statsOverlay.style.display === 'none' ? 'block' : 'none'; };

    videoWrap.appendChild(video);
    videoWrap.appendChild(statsOverlay);
    modal.appendChild(videoWrap);

    const controls = document.createElement('div');
    controls.style.cssText = 'background:var(--bg-card);padding:6px 0 0;';

    const seekOuter = document.createElement('div');
    seekOuter.style.cssText = 'height:6px;background:var(--border);border-radius:3px;cursor:pointer;position:relative;margin-top:20px;margin-bottom:6px;';
    const seekBuf = document.createElement('div');
    seekBuf.style.cssText = 'height:100%;background:rgba(76,175,80,0.3);border-radius:3px;width:0%;position:absolute;top:0;left:0;transition:width 1s linear;';
    const seekPos = document.createElement('div');
    seekPos.style.cssText = 'height:100%;background:var(--accent);border-radius:3px;width:0%;position:absolute;top:0;left:0;';
    const epgMarker = document.createElement('div');
    epgMarker.style.cssText = 'display:none;position:absolute;top:-2px;bottom:-2px;width:2px;background:#ffa726;border-radius:1px;z-index:2;pointer-events:none;';
    const segOverlayContainer = document.createElement('div');
    segOverlayContainer.style.cssText = 'position:absolute;top:0;left:0;right:0;bottom:0;pointer-events:none;z-index:3;';
    const segHandleContainer = document.createElement('div');
    segHandleContainer.style.cssText = 'position:absolute;top:-4px;left:0;right:0;bottom:-4px;pointer-events:none;z-index:5;';
    const segDeleteContainer = document.createElement('div');
    segDeleteContainer.style.cssText = 'position:absolute;top:-18px;left:0;right:0;height:16px;pointer-events:none;z-index:6;';
    seekOuter.appendChild(seekBuf);
    seekOuter.appendChild(seekPos);
    seekOuter.appendChild(epgMarker);
    seekOuter.appendChild(segOverlayContainer);
    seekOuter.appendChild(segHandleContainer);
    seekOuter.appendChild(segDeleteContainer);
    controls.appendChild(seekOuter);

    const btnRow = document.createElement('div');
    btnRow.style.cssText = 'display:flex;align-items:center;gap:8px;font-size:12px;color:#ccc;';

    const playPauseBtn = document.createElement('button');
    playPauseBtn.style.cssText = 'background:none;border:none;color:#ccc;font-size:16px;cursor:pointer;padding:0 2px;line-height:1;';
    playPauseBtn.innerHTML = '&#9646;&#9646;';
    playPauseBtn.onclick = () => {
      if (video.paused) {
        if (video.readyState < 2) {
          retryCount = 0;
          if (dvrTracker && dvrTracker.getBuffered() > 0 && !dvrTracker.isSeeking()) {
            var pos = dvrTracker.getPos(video.currentTime);
            seekTo(Math.min(pos, dvrTracker.getBuffered()));
          } else {
            startPlayback();
          }
        } else {
          video.play();
        }
      } else {
        video.pause();
      }
    };
    video.onpause = () => { playPauseBtn.innerHTML = '&#9654;'; };
    video.onplay = () => { playPauseBtn.innerHTML = '&#9646;&#9646;'; };

    const timeLabel = document.createElement('span');
    timeLabel.style.cssText = 'font-family:monospace;font-size:11px;min-width:80px;';
    timeLabel.textContent = '0:00';

    const spacer = document.createElement('div');
    spacer.style.cssText = 'flex:1;';

    const liveBadge = document.createElement('span');
    liveBadge.style.cssText = 'display:none;background:#e53935;color:#fff;font-size:10px;font-weight:bold;padding:1px 6px;border-radius:3px;cursor:pointer;';
    liveBadge.textContent = 'LIVE';
    liveBadge.title = 'Jump to live';

    const volWrap = document.createElement('div');
    volWrap.style.cssText = 'display:flex;align-items:center;gap:4px;';
    const volIcon = document.createElement('span');
    volIcon.style.cssText = 'font-size:14px;cursor:pointer;';
    volIcon.textContent = '\u{1F50A}';
    let savedVol = video.volume;
    volIcon.onclick = () => {
      if (video.volume > 0) { savedVol = video.volume; video.volume = 0; volIcon.textContent = '\u{1F507}'; }
      else { video.volume = savedVol || 0.5; volIcon.textContent = '\u{1F50A}'; }
    };
    const volSlider = document.createElement('input');
    volSlider.type = 'range'; volSlider.min = '0'; volSlider.max = '1'; volSlider.step = '0.05';
    volSlider.value = video.volume;
    volSlider.style.cssText = 'width:60px;height:4px;accent-color:var(--accent);';
    volSlider.oninput = () => {
      video.volume = parseFloat(volSlider.value);
      volIcon.textContent = video.volume > 0 ? '\u{1F50A}' : '\u{1F507}';
    };
    video.addEventListener('volumechange', () => { volSlider.value = video.volume; });
    volWrap.appendChild(volIcon);
    volWrap.appendChild(volSlider);

    const fsBtn = document.createElement('button');
    fsBtn.style.cssText = 'background:none;border:none;color:#ccc;font-size:14px;cursor:pointer;padding:0 2px;';
    fsBtn.innerHTML = '&#x26F6;';
    fsBtn.title = 'Fullscreen';
    fsBtn.onclick = () => {
      if (document.fullscreenElement) document.exitFullscreen();
      else videoWrap.requestFullscreen().catch(() => {});
    };
    function onFullscreenChange() {
      if (document.fullscreenElement === videoWrap) {
        video.style.maxHeight = '100vh';
        videoWrap.style.borderRadius = '0';
      } else {
        video.style.maxHeight = '450px';
        videoWrap.style.borderRadius = '4px';
      }
    }
    document.addEventListener('fullscreenchange', onFullscreenChange);

    btnRow.appendChild(playPauseBtn);
    btnRow.appendChild(timeLabel);
    btnRow.appendChild(spacer);
    btnRow.appendChild(liveBadge);
    btnRow.appendChild(volWrap);
    btnRow.appendChild(fsBtn);
    controls.appendChild(btnRow);
    modal.appendChild(controls);

    const statusEl = document.createElement('div');
    statusEl.style.cssText = 'color:#999;font-size:12px;margin-top:6px;';
    modal.appendChild(statusEl);

    overlay.appendChild(modal);
    overlay.onclick = (e) => { if (e.target === overlay) cleanup(); };
    document.body.appendChild(overlay);

    function seekTo(seekTime) {
      if (!dvrTracker || !dvr) return;
      var result = dvrTracker.seekTo(seekTime);
      if (result === null) return;
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Seeking to ' + fmtTime(result) + '...';
      videoWrap.style.minHeight = videoWrap.offsetHeight + 'px';
      destroyPlayer();
      video.pause();
      video.removeAttribute('src');
      video.src = '/vod/' + dvr.id + '/seek?t=' + result.toFixed(1);
      video.oncanplay = () => {
        videoWrap.style.minHeight = '';
        dvrTracker.completeSeek();
        statusEl.style.color = '#4caf50';
        currentContainer = 'fMP4';
        updateStatusText();
        if (channelID) api.del('/api/channels/' + channelID + '/fail').catch(() => {});
      };
      video.play().catch(() => { dvrTracker.completeSeek(); });
    }

    seekOuter.onclick = (e) => {
      const rect = seekOuter.getBoundingClientRect();
      if (rect.width === 0) return;
      const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      if (!dvrTracker) {
        if (video.duration && isFinite(video.duration)) {
          video.currentTime = pct * video.duration;
        }
        return;
      }
      if (dvrTracker.getBuffered() <= 0) return;
      const epg = getEpgTiming();
      const buf = dvrTracker.getBuffered();
      let seekTime;
      if (isLive && epg) {
        const progTime = pct * epg.duration;
        const bufStart = epg.elapsed - buf;
        seekTime = progTime - bufStart;
        if (seekTime < 0 || seekTime > buf) return;
      } else {
        const totalEnd = isLive ? buf : (dvr.duration || buf);
        seekTime = pct * totalEnd;
        if (seekTime > buf) return;
      }
      seekTo(seekTime);
    };

    function segPctFromOffset(offset, buffered) {
      var epg = getEpgTiming();
      if (isLive && epg) {
        var bufStart = epg.elapsed - buffered;
        return ((bufStart + offset) / epg.duration) * 100;
      }
      var totalEnd = isLive ? buffered : (dvr.duration || buffered);
      return totalEnd > 0 ? (offset / totalEnd) * 100 : 0;
    }

    function segOffsetFromPct(pct, buffered) {
      var epg = getEpgTiming();
      if (isLive && epg) {
        var bufStart = epg.elapsed - buffered;
        return pct * epg.duration - bufStart;
      }
      var totalEnd = isLive ? buffered : (dvr.duration || buffered);
      return pct * totalEnd;
    }

    var segColorMap = {
      recording: 'rgba(229,57,53,0.4)',
      defined: 'rgba(255,167,38,0.4)',
      extracting: 'rgba(255,167,38,0.25)',
      completed: 'rgba(76,175,80,0.4)'
    };

    function renderSegmentOverlays(segments, buffered) {
      segOverlayContainer.innerHTML = '';
      segHandleContainer.innerHTML = '';
      segDeleteContainer.innerHTML = '';
      if (!segments || !dvrTracker) return;
      segments.forEach(function(seg) {
        var segEnd = (seg.status === 'recording' && seg.end_offset != null) ? Math.max(seg.end_offset, buffered) : (seg.end_offset != null ? seg.end_offset : buffered);
        var startPct = Math.max(0, Math.min(100, segPctFromOffset(seg.start_offset, buffered)));
        var endPct = Math.max(0, Math.min(100, segPctFromOffset(segEnd, buffered)));
        var bg = segColorMap[seg.status] || segColorMap.recording;
        var ov = document.createElement('div');
        ov.style.cssText = 'position:absolute;top:0;bottom:0;border-radius:3px;background:' + bg + ';';
        ov.style.left = startPct + '%';
        ov.style.width = Math.max(0, endPct - startPct) + '%';
        if (seg.status === 'extracting') {
          ov.style.backgroundImage = 'repeating-linear-gradient(45deg,transparent,transparent 4px,rgba(255,255,255,0.15) 4px,rgba(255,255,255,0.15) 8px)';
        }
        segOverlayContainer.appendChild(ov);

        var isEditable = seg.status === 'recording' || seg.status === 'defined';
        if (isEditable) {
          var startHandle = document.createElement('div');
          startHandle.style.cssText = 'position:absolute;top:0;bottom:0;width:8px;background:#e53935;border-radius:2px;cursor:ew-resize;pointer-events:auto;';
          startHandle.style.left = 'calc(' + startPct + '% - 4px)';
          startHandle.dataset.segId = seg.id;
          startHandle.dataset.edge = 'start';
          segHandleContainer.appendChild(startHandle);

          var endHandle = document.createElement('div');
          endHandle.style.cssText = 'position:absolute;top:0;bottom:0;width:8px;border-radius:2px;cursor:ew-resize;pointer-events:auto;';
          endHandle.style.background = seg.status === 'defined' ? '#ffa726' : '#e53935';
          endHandle.style.left = 'calc(' + endPct + '% - 4px)';
          endHandle.dataset.segId = seg.id;
          endHandle.dataset.edge = 'end';
          segHandleContainer.appendChild(endHandle);

          var delBtn = document.createElement('div');
          delBtn.style.cssText = 'position:absolute;width:14px;height:14px;background:#e53935;color:#fff;border-radius:50%;font-size:10px;line-height:14px;text-align:center;cursor:pointer;pointer-events:auto;user-select:none;';
          delBtn.style.left = 'calc(' + ((startPct + endPct) / 2) + '% - 7px)';
          delBtn.textContent = '\u2715';
          delBtn.title = 'Delete segment';
          delBtn.dataset.segId = seg.id;
          delBtn.onclick = function(ev) {
            ev.stopPropagation();
            api.del('/vod/' + dvr.id + '/record/' + seg.id).catch(function(err) {
              toast.error('Delete segment failed: ' + err.message);
            });
          };
          segDeleteContainer.appendChild(delBtn);
        }
      });
    }

    segHandleContainer.addEventListener('mousedown', function(e) {
      var handle = e.target;
      if (!handle.dataset || !handle.dataset.segId || !dvrTracker) return;
      e.preventDefault();
      e.stopPropagation();
      dragState = { segID: handle.dataset.segId, edge: handle.dataset.edge, rect: seekOuter.getBoundingClientRect(), handle: handle };
    });

    function onDocMouseMove(e) {
      if (!dragState || !dragState.handle) return;
      if (dragState.rect.width === 0) return;
      e.preventDefault();
      var pct = Math.max(0, Math.min(1, (e.clientX - dragState.rect.left) / dragState.rect.width));
      dragState.handle.style.left = 'calc(' + (pct * 100) + '% - 4px)';
    }
    document.addEventListener('mousemove', onDocMouseMove);

    function onDocMouseUp(e) {
      if (!dragState || !dragState.handle) return;
      var rect = dragState.rect;
      var segID = dragState.segID;
      var edge = dragState.edge;
      dragState = null;
      if (rect.width === 0) return;
      var pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      var buf = dvrTracker.getBuffered();
      var newOffset = Math.max(0, Math.min(buf, segOffsetFromPct(pct, buf)));
      var body = {};
      if (edge === 'start') {
        body.start_offset = newOffset;
      } else {
        body.end_offset = (pct >= 0.98) ? -1 : newOffset;
      }
      api.put('/vod/' + dvr.id + '/record/' + segID, body).catch(function(err) {
        toast.error('Update segment failed: ' + err.message);
      });
    }
    document.addEventListener('mouseup', onDocMouseUp);

    liveBadge.onclick = () => {
      if (!dvr) return;
      videoWrap.style.minHeight = videoWrap.offsetHeight + 'px';
      destroyPlayer();
      video.pause();
      video.removeAttribute('src');
      dvrTracker.reset();
      var buf = dvrTracker.getBuffered();
      video.src = buf > 5 ? '/vod/' + dvr.id + '/seek?t=' + Math.max(0, buf - 5).toFixed(1) : '/vod/' + dvr.id + '/stream';
      video.oncanplay = () => {
        videoWrap.style.minHeight = '';
        statusEl.style.color = '#4caf50';
        currentContainer = 'fMP4';
        updateStatusText();
        if (channelID) api.del('/api/channels/' + channelID + '/fail').catch(() => {});
      };
      video.play().catch(() => {});
    };

    function getEpgTiming() {
      if (!nowProgram || !nowProgram.start || !nowProgram.stop) return null;
      const start = new Date(nowProgram.start).getTime();
      const stop = new Date(nowProgram.stop).getTime();
      const dur = (stop - start) / 1000;
      if (dur <= 0) return null;
      const elapsed = (Date.now() - start) / 1000;
      return { duration: dur, elapsed: Math.max(0, Math.min(dur, elapsed)) };
    }

    if (dvr && dvrTracker) {
      dvrPollInterval = setInterval(async () => {
        if (playerCtx.signal.aborted) { clearDvrIntervals(); return; }
        try {
          const resp = await fetch('/vod/' + dvr.id + '/status', { signal: playerCtx.signal });
          if (resp.status === 404) {
            clearDvrIntervals();
            statusEl.style.color = '#ff6b6b';
            statusEl.textContent = 'Session ended';
            return;
          }
          if (!resp.ok) {
            pollFailures++;
            if (pollFailures >= 3) {
              clearDvrIntervals();
            }
            return;
          }
          pollFailures = 0;
          const st = await resp.json();
          if (st.error && !st.recording) {
            statusEl.style.color = '#ff6b6b';
            statusEl.textContent = 'Source failed: ' + st.error;
            clearDvrIntervals();
            return;
          }
          dvrTracker.updateBuffered(st.buffered);
          if (isLive && st.duration > 0) {
            isLive = false;
            dvrTracker.setDuration(st.duration);
            dvr.duration = st.duration;
          }
          if (st.profile && st.profile !== currentProfile) {
            currentProfile = st.profile;
            if (profileSelect) profileSelect.value = currentProfile;
          }
          if (st.segments && st.segments.length > 0) {
            var activeSeg = st.segments.find(function(seg) { return seg.status === 'recording'; });
            if (activeSeg) {
              activeSegmentID = activeSeg.id;
              if (!isRecording) startRecordingUI();
            } else {
              if (isRecording) stopRecordingUI();
            }
            renderSegmentOverlays(st.segments, st.buffered);
          } else {
            if (isRecording) stopRecordingUI();
            renderSegmentOverlays([], st.buffered);
          }
          if (st.video || st.audio_tracks) {
            probeData = { video: st.video || null, audio_tracks: st.audio_tracks || [], duration: st.duration, profile: st.profile || '' };
          }
          if (st.audio_tracks && st.audio_tracks.length > 1 && !audioSelect) {
            audioSelect = document.createElement('select');
            audioSelect.className = 'btn btn-sm';
            audioSelect.title = 'Audio Track';
            audioSelect.style.cssText = 'padding:2px 6px;font-size:12px;background:var(--bg-card);color:#e0e0e0;border:1px solid var(--border);border-radius:4px;cursor:pointer;max-width:120px;';
            st.audio_tracks.forEach(function(t) {
              var opt = document.createElement('option');
              opt.value = t.index;
              var label = t.language ? t.language.toUpperCase() : 'Track ' + (t.index + 1);
              if (t.codec) label += ' (' + t.codec + ')';
              opt.textContent = label;
              audioSelect.appendChild(opt);
            });
            audioSelect.value = st.audio_index || 0;
            currentAudioIndex = st.audio_index || 0;
            audioSelect.onchange = function() { switchAudioTrack(parseInt(audioSelect.value, 10)); };
            hdrBtns.insertBefore(audioSelect, recordBtn);
          }
          var buf = st.buffered;
          const epg = getEpgTiming();
          if (isLive && epg) {
            const bufStartPct = Math.max(0, ((epg.elapsed - buf) / epg.duration) * 100);
            const bufWidthPct = Math.min(100 - bufStartPct, (buf / epg.duration) * 100);
            seekBuf.style.left = bufStartPct + '%';
            seekBuf.style.width = bufWidthPct + '%';
            epgMarker.style.display = 'block';
            epgMarker.style.left = Math.min(100, (epg.elapsed / epg.duration) * 100) + '%';
          } else if (!isLive) {
            seekBuf.style.left = '0%';
            epgMarker.style.display = 'none';
            const total = dvr.duration || buf;
            if (total > 0) seekBuf.style.width = Math.min(100, (buf / total) * 100) + '%';
            if (st.ready) {
              seekBuf.style.width = '100%';
              seekBuf.style.background = 'rgba(76,175,80,0.5)';
            }
          } else {
            seekBuf.style.left = '0%';
            seekBuf.style.width = '100%';
            epgMarker.style.display = 'none';
          }
        } catch(e) {
          pollFailures++;
          if (pollFailures >= 3) {
            clearDvrIntervals();
          }
        }
      }, 2000);

      dvrPosInterval = setInterval(() => {
        if (playerCtx.signal.aborted) { clearDvrIntervals(); return; }
        if (!video || dvrTracker.isSeeking()) return;
        var d = dvrTracker.getDisplay(video.currentTime);
        const epg = getEpgTiming();

        if (isLive && epg) {
          const progPos = epg.elapsed - dvrTracker.getBuffered() + d.pos;
          seekPos.style.width = Math.min(100, Math.max(0, (progPos / epg.duration) * 100)) + '%';
          const progRemain = Math.max(0, epg.duration - epg.elapsed);
          timeLabel.textContent = fmtTime(d.pos) + ' / ' + fmtTime(d.total) + ' (' + fmtTime(progRemain) + ' left)';
          liveBadge.style.display = 'inline-block';
          liveBadge.style.opacity = (dvrTracker.getSeekOffset() > 0) ? '1' : '0.5';
        } else if (isLive) {
          seekPos.style.width = d.pct + '%';
          timeLabel.textContent = fmtTime(d.pos) + ' / ' + fmtTime(d.total);
          liveBadge.style.display = 'inline-block';
          liveBadge.style.opacity = (dvrTracker.getSeekOffset() > 0) ? '1' : '0.5';
        } else {
          seekPos.style.width = d.pct + '%';
          timeLabel.textContent = fmtTime(d.pos) + ' / ' + fmtTime(d.total);
        }
      }, 500);
    } else {
      dvrPosInterval = setInterval(() => {
        if (playerCtx.signal.aborted) { clearDvrIntervals(); return; }
        if (!video || !video.duration || !isFinite(video.duration)) return;
        var pct = (video.currentTime / video.duration) * 100;
        seekPos.style.width = pct + '%';
        seekBuf.style.width = '100%';
        timeLabel.textContent = fmtTime(video.currentTime) + ' / ' + fmtTime(video.duration);
      }, 500);
    }

    function updateStats() {
      if (playerCtx.signal.aborted) return;
      var lines = [];
      var res = (video.videoWidth && video.videoHeight) ? video.videoWidth + 'x' + video.videoHeight : null;
      var buf = video.buffered.length > 0 ? (video.buffered.end(0) - video.currentTime).toFixed(1) + 's' : '0s';
      var vi = probeData && probeData.video ? probeData.video : null;
      var at = probeData && probeData.audio_tracks ? probeData.audio_tracks : [];
      var activeAudio = at.length > 0 ? at[currentAudioIndex] || at[0] : null;

      if (mpegtsPlayer && mpegtsPlayer.statisticsInfo) {
        var stats = mpegtsPlayer.statisticsInfo;
        var mi = mpegtsPlayer.mediaInfo || {};
        lines.push('Resolution: ' + (res || '?'));
        lines.push('Video: ' + codecName(mi.videoCodec) + (vi && vi.profile ? ' (' + vi.profile + ')' : ''));
        lines.push('FPS: ' + (vi && vi.fps ? vi.fps : (mi.fps || '?')));
        lines.push('Audio: ' + codecName(mi.audioCodec) + (activeAudio && activeAudio.language ? ' [' + activeAudio.language + ']' : ''));
        if (activeAudio && activeAudio.channels) lines.push('Channels: ' + activeAudio.channels + 'ch' + (activeAudio.sample_rate ? ' @ ' + activeAudio.sample_rate + ' Hz' : ''));
        if (vi && vi.color_space && vi.color_space !== 'unknown') lines.push('Color: ' + vi.color_space + (vi.color_transfer && vi.color_transfer !== 'unknown' ? '/' + vi.color_transfer : ''));
        if (vi && vi.pix_fmt) lines.push('Pixel: ' + vi.pix_fmt);
        if (vi && vi.field_order && vi.field_order !== 'unknown' && vi.field_order !== 'progressive') lines.push('Scan: ' + vi.field_order);
        lines.push('Container: ' + (currentContainer || '?'));
        lines.push('Buffer: ' + buf);
        lines.push('Speed: ' + (stats.speed != null ? (stats.speed / 1024).toFixed(2) + ' MB/s' : '?'));
        lines.push('Dropped: ' + (stats.droppedFrames != null ? stats.droppedFrames : '?'));
      } else {
        lines.push('Resolution: ' + (res || (vi ? vi.codec : '?')));
        if (vi) {
          lines.push('Video: ' + vi.codec + (vi.profile ? ' (' + vi.profile + ')' : ''));
          if (vi.fps) lines.push('FPS: ' + vi.fps);
          if (vi.bit_rate) lines.push('Video BR: ' + (parseInt(vi.bit_rate) / 1000).toFixed(0) + ' kbps');
        }
        if (activeAudio) {
          lines.push('Audio: ' + activeAudio.codec + (activeAudio.language ? ' [' + activeAudio.language + ']' : '') + (activeAudio.profile ? ' (' + activeAudio.profile + ')' : ''));
          if (activeAudio.channels) lines.push('Channels: ' + activeAudio.channels + 'ch' + (activeAudio.sample_rate ? ' @ ' + activeAudio.sample_rate + ' Hz' : ''));
          if (activeAudio.bit_rate) lines.push('Audio BR: ' + (parseInt(activeAudio.bit_rate) / 1000).toFixed(0) + ' kbps');
        }
        if (vi && vi.color_space && vi.color_space !== 'unknown') lines.push('Color: ' + vi.color_space + (vi.color_transfer && vi.color_transfer !== 'unknown' ? '/' + vi.color_transfer : '') + (vi.color_primaries && vi.color_primaries !== 'unknown' ? '/' + vi.color_primaries : ''));
        if (vi && vi.pix_fmt) lines.push('Pixel: ' + vi.pix_fmt);
        if (vi && vi.field_order && vi.field_order !== 'unknown' && vi.field_order !== 'progressive') lines.push('Scan: ' + vi.field_order);
        lines.push('Container: ' + (currentContainer || '?'));
        lines.push('Buffer: ' + buf);
      }
      if (probeData && probeData.duration > 0) lines.push('Duration: ' + fmtTime(probeData.duration));
      if (probeData && probeData.profile) lines.push('Profile: ' + probeData.profile);
      statsOverlay.innerHTML = lines.join('<br>');
    }

    function formatTime(d) {
      return new Date(d).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }

    function buildStatusSuffix() {
      if (!nowProgram) return '';
      let suffix = ' \u2014 ' + nowProgram.title;
      if (nowProgram.start && nowProgram.stop) {
        suffix += ' (' + formatTime(nowProgram.start) + ' - ' + formatTime(nowProgram.stop) + ')';
      }
      return suffix;
    }

    function updateStatusText() {
      let codecInfo = '';
      if (currentCodec) {
        codecInfo = currentContainer ? '(' + currentCodec + '/' + currentContainer + ')' : '(' + currentCodec + ')';
      } else if (currentContainer) {
        codecInfo = '(' + currentContainer + ')';
      }
      statusEl.textContent = 'Playing' + (codecInfo ? ' ' + codecInfo : '') + buildStatusSuffix();
    }

    function fetchNowPlaying() {
      if (!tvgId || playerCtx.signal.aborted) return;
      api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(program => {
        if (program && program.title) {
          nowProgram = program;
          updateStatusText();
        }
      }).catch(() => {});
    }

    fetchNowPlaying();
    if (tvgId) {
      progInterval = setInterval(fetchNowPlaying, 60000);
    }

    const isBrowserProfile = url.includes('profile=Browser');

    function startPlayback() {
      destroyPlayer();
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      currentContainer = '';
      currentCodec = '';
      nowProgram = null;
      video.removeAttribute('src');

      if (isBrowserProfile) {
        statusEl.style.color = '#999';
        statusEl.textContent = 'Connecting...';
        video.src = dvr ? '/vod/' + dvr.id + '/stream' : url;
        video.oncanplay = () => {
          statusEl.style.color = '#4caf50';
          currentContainer = 'fMP4';
          retryCount = 0;
          updateStatusText();
          fetchNowPlaying();
          if (channelID) api.del('/api/channels/' + channelID + '/fail').catch(() => {});
        };
        video.play().catch(() => handleRetry());
        statsInterval = setInterval(updateStats, 2000);
      } else if (typeof mpegts !== 'undefined' && mpegts.isSupported()) {
        statusEl.style.color = '#999';
        statusEl.textContent = 'Connecting...';
        mpegtsPlayer = mpegts.createPlayer({
          type: 'mse', isLive: true, url: url,
        }, {
          enableStashBuffer: true, stashInitialSize: 4096, liveBufferLatency: 2.0,
        });
        mpegtsPlayer.attachMediaElement(video);
        mpegtsPlayer.load();
        mpegtsPlayer.play();
        mpegtsPlayer.on(mpegts.Events.ERROR, (errorType, errorDetail) => {
          console.warn('mpegts.js error:', errorType, errorDetail);
          if (errorType === 'NetworkError' || errorType === 'MediaError') handleRetry();
          else { statusEl.style.color = '#ff6b6b'; statusEl.textContent = 'Error: ' + errorDetail; }
        });
        mpegtsPlayer.on(mpegts.Events.MEDIA_INFO, () => {
          retryCount = 0;
          if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
          statusEl.style.color = '#4caf50';
          const mi = mpegtsPlayer.mediaInfo || {};
          currentCodec = codecName(mi.videoCodec);
          currentContainer = 'MPEG-TS';
          updateStatusText();
          fetchNowPlaying();
        });
        statsInterval = setInterval(updateStats, 2000);
      } else {
        statusEl.style.color = '#999';
        statusEl.textContent = 'Connecting...';
        video.src = url;
        video.play().catch(() => {
          statusEl.style.color = '#ff6b6b';
          statusEl.textContent = 'Playback failed.';
        });
      }
    }

    function handleRetry() {
      if (playerCtx.signal.aborted) return;
      if (retryCount >= MAX_RETRIES) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Source unavailable. ';
        const retryBtn = document.createElement('a');
        retryBtn.textContent = 'Retry';
        retryBtn.href = '#';
        retryBtn.style.cssText = 'color:#4fc3f7;cursor:pointer;text-decoration:underline;';
        retryBtn.onclick = (e) => { e.preventDefault(); retryCount = 0; handleRetry(); };
        statusEl.appendChild(retryBtn);
        destroyPlayer();
        return;
      }
      retryCount++;
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Retrying... (' + retryCount + '/' + MAX_RETRIES + ')';
      destroyPlayer();
      if (dvr && channelID) {
        retryTimeout = setTimeout(async () => {
          try {
            await api.del('/vod/' + dvr.id).catch(() => {});
            var audioParam = currentAudioIndex > 0 ? '&audio=' + currentAudioIndex : '';
            const resp = await fetch('/channel/' + channelID + '/vod?profile=' + encodeURIComponent(currentProfile) + audioParam, { method: 'POST' }).then(r => r.json());
            if (resp.session_id) {
              dvr = { id: resp.session_id, duration: resp.duration };
              if (dvrTracker) dvrTracker.reset();
            }
          } catch(e) {}
          startPlayback();
        }, 2000);
      } else {
        retryTimeout = setTimeout(startPlayback, 2000);
      }
    }

    video.addEventListener('waiting', () => {
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Buffering...';
      if (stallTimeout) clearTimeout(stallTimeout);
      stallTimeout = setTimeout(() => {
        if (!video.paused && video.readyState < 3) {
          if (dvrTracker && dvrTracker.getBuffered() > 0 && !dvrTracker.isSeeking()) {
            var pos = dvrTracker.getPos(video.currentTime);
            seekTo(Math.min(pos, dvrTracker.getBuffered()));
          }
        }
      }, dvr ? 5000 : 30000);
    });
    video.addEventListener('playing', () => {
      if (stallTimeout) { clearTimeout(stallTimeout); stallTimeout = null; }
      statusEl.style.color = '#4caf50';
      updateStatusText();
    });

    video.onerror = () => {
      if (!mpegtsPlayer && !(dvrTracker && dvrTracker.isSeeking())) {
        if (channelID) api.post('/api/channels/' + channelID + '/fail').catch(() => {});
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Source error';
        handleRetry();
      }
    };

    startPlayback();
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

    let matchedEpg = null;
    if (tvgId) {
      const epgData = await epgCache.getAll().catch(() => []);
      matchedEpg = epgData.find(e => e.channel_id === tvgId);
    }
    if (!matchedEpg) {
      const epgData = await epgCache.getAll().catch(() => []);
      const norm = streamName.toLowerCase().replace(/\s*(hd|sd|fhd|uhd|\+1|_hd|_sd)\s*$/i, '').trim();
      for (let i = 0; i < epgData.length; i++) {
        const en = (epgData[i].name || '').toLowerCase().replace(/\s*(hd|sd|fhd|uhd|\+1|_hd|_sd)\s*$/i, '').trim();
        if (en === norm) { matchedEpg = epgData[i]; break; }
      }
    }

    const tvgInp = h('input', { type: 'text', value: matchedEpg ? matchedEpg.channel_id : (tvgId || '') });
    const statusEl = matchedEpg
      ? h('small', { style: 'color:var(--success)' }, 'Auto-matched: ' + (matchedEpg._display_name || matchedEpg.name))
      : h('small', { style: 'color:var(--text-muted)' }, tvgId ? 'Using stream tvg-id' : 'No EPG match found');

    const bodyEl = h('div', null,
      h('div', { className: 'form-group' }, h('label', null, 'Channel Name'), nameInp),
      h('div', { className: 'form-group' }, h('label', null, 'EPG Channel ID'), tvgInp, statusEl),
      h('div', { className: 'form-group' }, h('label', null, 'Channel Group'), groupSelect),
    );

    showModal('Quick Add Channel', bodyEl, async () => {
      if (!nameInp.value.trim()) throw new Error('Channel name is required');

      const channelData = {
        name: nameInp.value.trim(),
        tvg_id: tvgInp.value.trim(),
        channel_group_id: groupSelect.value || null,
        is_enabled: true,
      };

      if (logoUrl || (matchedEpg && matchedEpg.icon)) {
        channelData.logo = logoUrl || matchedEpg.icon;
      }

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
          handler: async () => {
            try {
              await api.post('/api/m3u/accounts/' + item.id + '/refresh');
              streamsCache.invalidate();
              for (var k in streamGroupsCache) delete streamGroupsCache[k];
              rebuildStreamNav();
              toast.success('Refresh started for ' + item.name);
              setTimeout(reload, 2000);
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
        { key: 'logo', label: '', thStyle: 'width:30px;padding-right:0;text-align:center', tdStyle: 'padding-right:0;text-align:center', render: item =>
          item.logo ? h('img', { src: item.logo, style: 'height:24px;width:24px;object-fit:contain;border-radius:2px;' }) : null
        },
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
      rowActions: (item) => [
        { label: 'Play', icon: '\u25B6', handler: () => playChannelWithDVR(item.id, item.name, item.tvg_id || undefined) },
      ],
      postFormSetup: (inputs, isEdit, item) => {
        if (isEdit && inputs._stream && item.id) {
          Promise.all([
            api.get('/api/channels/' + item.id + '/streams'),
            api.get('/api/m3u/accounts').catch(() => []),
          ]).then(([streams, accounts]) => {
            if (streams && streams.length > 0) {
              const nameMap = {};
              accounts.forEach(a => { nameMap[a.id] = a.name; });
              const s = streams[0];
              inputs._stream.value = (nameMap[s.m3u_account_id] || 'Unknown') + '/' + s.name;
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
            handler: async () => {
              try {
                await api.post('/api/epg/sources/' + item.id + '/refresh');
                epgCache.invalidate();
                toast.success('EPG refresh started for ' + item.name);
                setTimeout(reload, 2000);
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
        { key: 'source_type', label: 'Source', render: item => ({direct:'Direct',satip:'SAT>IP',m3u:'M3U'})[item.source_type] || item.source_type },
        { key: 'hwaccel', label: 'HW Accel', render: item => ({none:'None (Software)',qsv:'Intel QSV',nvenc:'NVIDIA NVENC',vaapi:'VAAPI (AMD/Intel)',videotoolbox:'VideoToolbox (macOS)'})[item.hwaccel] || item.hwaccel },
        { key: 'video_codec', label: 'Codec', render: item => ({copy:'Copy',h264:'H.264',h265:'H.265',av1:'AV1'})[item.video_codec] || item.video_codec },
        { key: 'container', label: 'Container', render: item => ({mpegts:'MPEG-TS',matroska:'Matroska',mp4:'MP4',webm:'WebM'})[item.container] || item.container },
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
        { key: 'source_type', label: 'Source Type', type: 'select', options: [
          { value: 'satip', label: 'SAT>IP' },
          { value: 'm3u', label: 'M3U' },
        ], showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'hwaccel', label: 'Hardware Acceleration', type: 'select', options: [
          { value: 'none', label: 'None (Software)' },
          { value: 'qsv', label: 'Intel QSV (Arc/iGPU)' },
          { value: 'nvenc', label: 'NVIDIA NVENC' },
          { value: 'vaapi', label: 'VAAPI (AMD/Intel)' },
          { value: 'videotoolbox', label: 'VideoToolbox (macOS only)' },
        ], showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'video_codec', label: 'Video Codec', type: 'select', options: [
          { value: 'copy', label: 'Copy (No Transcode)' },
          { value: 'h264', label: 'H.264 / AVC' },
          { value: 'h265', label: 'H.265 / HEVC' },
          { value: 'av1', label: 'AV1' },
        ], showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'container', label: 'Container Format', type: 'select', options: [
          { value: 'mpegts', label: 'MPEG-TS (HDHR/Plex)' },
          { value: 'matroska', label: 'Matroska (VLC)' },
          { value: 'mp4', label: 'MP4 (Browser/Plex)' },
          { value: 'webm', label: 'WebM (Browser, requires Opus audio)' },
        ], showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'use_custom_args', label: 'Use Custom Args', type: 'checkbox', default: false, help: 'When checked, the FFmpeg Args field below is used as the complete command (dropdowns are ignored).', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
        { key: 'custom_args', label: 'FFmpeg Args', type: 'textarea', placeholder: '-b:v 4M -maxrate 5M', help: 'Extra flags appended to the composed command. When "Use Custom Args" is checked, this is the full command.', showWhen: form => (form.stream_mode || 'ffmpeg') === 'ffmpeg' },
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

        formContent.appendChild(h('div', { className: 'form-check' }, enabledChk, h('label', null, 'Enabled')));
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

      function fmtDur(secs) {
        var hrs = Math.floor(secs / 3600);
        var mins = Math.floor((secs % 3600) / 60);
        var s = Math.floor(secs % 60);
        return hrs > 0 ? hrs + ':' + String(mins).padStart(2,'0') + ':' + String(s).padStart(2,'0')
                       : mins + ':' + String(s).padStart(2,'0');
      }

      async function renderActive(activeDiv) {
        try {
          var recordings = await api.get('/api/recordings');
          activeDiv.innerHTML = '';
          if (!recordings || recordings.length === 0) {
            activeDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No active recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Channel</th><th>Program</th><th>Buffered</th><th>Segments</th><th>Stop At</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var stopStr = rec.stop_at ? new Date(rec.stop_at).toLocaleTimeString() : '-';
            var segCount = rec.segments ? rec.segments.length : 0;
            var activeSeg = rec.segments ? rec.segments.find(function(s) { return s.status === 'recording'; }) : null;
            var segLabel = segCount + (activeSeg ? ' (recording)' : '');
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var playBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: function() {
              playChannelWithDVR(rec.session_id, rec.channel_name || rec.program_title, null);
            }}, '\u25B6 Play');
            var stopBtn = h('button', { className: 'btn btn-warning btn-sm', onClick: async function() {
              await api.post('/vod/' + rec.session_id + '/stop?extract=true');
              renderActive(activeDiv);
              renderCompleted(completedDiv);
            }}, 'Stop');
            var cancelBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm('Cancel and discard "' + (rec.program_title || 'recording') + '"?')) return;
              await api.post('/vod/' + rec.session_id + '/cancel');
              renderActive(activeDiv);
            }}, 'Cancel');
            actions.appendChild(playBtn);
            actions.appendChild(stopBtn);
            actions.appendChild(cancelBtn);
            var tr = h('tr', null,
              h('td', null, rec.channel_name),
              h('td', null, rec.program_title),
              h('td', null, fmtDur(rec.buffered_secs)),
              h('td', null, segLabel),
              h('td', null, stopStr),
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

      async function renderCompleted(completedDiv) {
        try {
          var recordings = await api.get('/api/recordings/completed');
          completedDiv.innerHTML = '';
          if (!recordings || recordings.length === 0) {
            completedDiv.appendChild(h('p', { style: 'color: var(--text-muted); padding: 16px;' }, 'No completed recordings.'));
            return;
          }
          var table = h('table', { className: 'table' });
          table.innerHTML = '<thead><tr><th>Filename</th><th>Size</th><th>Date</th><th>Actions</th></tr></thead>';
          var tbody = h('tbody');
          recordings.forEach(function(rec) {
            var dateStr = new Date(rec.mod_time).toLocaleString();
            var actions = h('td', { style: 'display:flex;gap:4px;' });
            var playBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: function() {
              var fileUrl = '/api/recordings/completed/' + encodeURIComponent(rec.filename) + '/stream?profile=Browser&user_id=' + encodeURIComponent(rec.user_id || '') + '&token=' + encodeURIComponent(state.accessToken || '');
              openVideoPlayer(rec.filename, fileUrl, null, null);
            }}, '\u25B6 Play');
            var deleteBtn = h('button', { className: 'btn btn-danger btn-sm', onClick: async function() {
              if (!confirm('Delete ' + rec.filename + '?')) return;
              await api.del('/api/recordings/completed/' + encodeURIComponent(rec.filename) + '?user_id=' + encodeURIComponent(rec.user_id || ''));
              renderCompleted(completedDiv);
            }}, 'Delete');
            actions.appendChild(playBtn);
            actions.appendChild(deleteBtn);
            var tr = h('tr', null,
              h('td', null, rec.filename),
              h('td', null, fmtSize(rec.size)),
              h('td', null, dateStr),
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
            var startStr = new Date(rec.start_at).toLocaleString();
            var stopStr = new Date(rec.stop_at).toLocaleString();
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
              h('td', null, startStr),
              h('td', null, stopStr),
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

      var activeSection = h('div', { className: 'table-container', style: 'margin-top: 16px;' },
        h('div', { className: 'table-header' }, h('h3', null, 'Active Recordings'))
      );
      var activeDiv = h('div');
      activeSection.appendChild(activeDiv);

      var completedSection = h('div', { className: 'table-container', style: 'margin-top: 16px;' },
        h('div', { className: 'table-header' }, h('h3', null, 'Completed Recordings'))
      );
      var completedDiv = h('div');
      completedSection.appendChild(completedDiv);

      container.appendChild(scheduledSection);
      container.appendChild(activeSection);
      container.appendChild(completedSection);

      renderScheduled(scheduledDiv);
      renderActive(activeDiv);
      renderCompleted(completedDiv);

      pollTimer = setInterval(function() { renderScheduled(scheduledDiv); renderActive(activeDiv); }, 5000);

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

        const logosDisabled = (Array.isArray(settings) ? settings : []).some(s => s.key === 'logos_enabled' && s.value === 'false');
        const logosToggle = h('input', { type: 'checkbox', id: 'setting-logos-disabled' });
        logosToggle.checked = logosDisabled;
        logosToggle.onchange = async function() {
          logosToggle.disabled = true;
          try {
            await api.put('/api/settings', { logos_enabled: logosToggle.checked ? 'false' : 'true' });
            toast.success('Setting saved');
          } catch (err) {
            toast.error(err.message);
            logosToggle.checked = !logosToggle.checked;
          }
          logosToggle.disabled = false;
        };

        container.appendChild(h('div', { className: 'table-container', style: 'margin-top: 24px' },
          h('div', { className: 'table-header' }, h('h3', null, 'Logo Caching')),
          h('div', { style: 'padding: 16px; font-size: 15px' },
            h('div', { style: 'display:flex;align-items:center;gap:10px' },
              logosToggle,
              h('label', { for: 'setting-logos-disabled', style: 'cursor:pointer;margin:0' }, 'Disable local logo caching'),
            ),
            h('p', { style: 'color: var(--text-muted); margin-top: 8px; font-size: 13px' },
              'Logo caching is on by default. Channel and stream logos are downloaded and served locally, improving performance for DLNA clients. Check this box to disable caching and use external logo URLs directly.'),
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
  };

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

    app.appendChild(
      h('div', { className: 'layout' },
        renderSidebar(),
        h('div', { className: 'main-content' },
          h('div', { className: 'topbar' },
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

  // Test exports
  if (typeof window !== 'undefined') {
    window._testExports = { createDVRTracker: createDVRTracker };
  }

  init();
})();
