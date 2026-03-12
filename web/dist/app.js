// TVProxy Web UI - Single Page Application

(function() {
  'use strict';

  // ─── State ────────────────────────────────────────────────────────────
  const state = {
    user: null,
    accessToken: localStorage.getItem('access_token'),
    refreshToken: localStorage.getItem('refresh_token'),
    currentPage: 'dashboard',
  };

  // ─── API Client ───────────────────────────────────────────────────────
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
        state.refreshToken = data.refresh_token;
        localStorage.setItem('access_token', data.access_token);
        localStorage.setItem('refresh_token', data.refresh_token);
        return true;
      } catch {
        return false;
      }
    },
  };

  // ─── Auth ─────────────────────────────────────────────────────────────
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
      await auth.fetchUser();
    },

    async fetchUser() {
      try {
        state.user = await api.get('/api/auth/me');
      } catch {
        state.user = null;
      }
    },

    logout() {
      api.post('/api/auth/logout').catch(() => {});
      state.user = null;
      state.accessToken = null;
      state.refreshToken = null;
      localStorage.removeItem('access_token');
      localStorage.removeItem('refresh_token');
      render();
    },

    isLoggedIn() {
      return !!state.accessToken && !!state.user;
    },
  };

  // ─── Toast ────────────────────────────────────────────────────────────
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

  // ─── Data Cache Utility ───────────────────────────────────────────────
  class DataCache {
    constructor({ loader, searchKeys, label }) {
      this._loader = loader;
      this._searchKeys = searchKeys;
      this._data = null;
      this._index = null;
      this._promise = null;
      this.label = label || 'Data';
      this.state = 'idle'; // idle | loading | ready
      this.count = 0;
    }

    async getAll() {
      if (this._data) return this._data;
      if (this._promise) return this._promise;
      this.state = 'loading';
      DataCache._notify();
      this._promise = (async () => {
        try { this._data = await this._loader(); } catch { this._data = []; }
        this._buildIndex();
        this.state = 'ready';
        this.count = this._data.length;
        this._promise = null;
        DataCache._notify();
        return this._data;
      })();
      return this._promise;
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
      DataCache._notify();
    }
  }
  DataCache._listeners = [];
  DataCache.onChange = function(fn) { DataCache._listeners.push(fn); return fn; };
  DataCache.offChange = function(fn) { DataCache._listeners = DataCache._listeners.filter(function(f) { return f !== fn; }); };
  DataCache._notify = function() { DataCache._listeners.forEach(function(fn) { fn(); }); };

  // ─── Data Caches ──────────────────────────────────────────────────────
  const epgCache = new DataCache({
    label: 'EPG',
    loader: () => api.get('/api/epg/data'),
    searchKeys: ['name', 'channel_id'],
  });

  const logosCache = new DataCache({
    label: 'Logos',
    loader: () => api.get('/api/logos'),
    searchKeys: ['name', 'url'],
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
      return streams;
    },
    searchKeys: ['_display_name', 'group'],
  });

  const channelsCache = new DataCache({
    label: 'Channels',
    loader: () => api.get('/api/channels'),
    searchKeys: ['name', 'tvg_id'],
  });

  const channelGroupsCache = new DataCache({
    label: 'Groups',
    loader: () => api.get('/api/channel-groups'),
    searchKeys: ['name'],
  });

  // ─── Router ───────────────────────────────────────────────────────────
  function navigate(page) {
    state.currentPage = page;
    render();
  }

  // ─── HTML Helpers ─────────────────────────────────────────────────────
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

  // ─── Modal ────────────────────────────────────────────────────────────
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

  // ─── Login Page ───────────────────────────────────────────────────────
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

  // ─── Navigation ───────────────────────────────────────────────────────
  let navItems = [
    { section: 'Overview' },
    { id: 'dashboard', label: 'Dashboard', icon: '\u2302', tip: 'Overview of your TVProxy system status' },
    { section: 'Sources' },
    { id: 'm3u-accounts', label: 'M3U Accounts', icon: '\u2630', tip: 'Add your SAT>IP or IPTV source M3U files' },
    { id: 'epg-sources', label: 'EPG Sources', icon: '\ud83d\udcc5', tip: 'Manage XMLTV EPG data sources for programme guides' },
    { section: 'Channels' },
    { id: 'channels', label: 'Channels', icon: '\ud83d\udcfa', tip: 'Define your custom channels and assign streams and EPG data' },
    { id: 'channel-groups', label: 'Channel Groups', icon: '\ud83d\udcc2', tip: 'Organize channels into groups like Sports, Entertainment, News' },
    { id: 'epg-guide', label: 'EPG Guide', icon: '\ud83d\udcf0', tip: 'TV programme guide grid for your channels' },
    { id: 'channel-profiles', label: 'Channel Profiles', icon: '\u2699', tip: 'Control which channels are exposed to each HDHR device' },
    { section: 'Configuration' },
    { id: 'stream-profiles', label: 'Stream Profiles', icon: '\ud83d\udd27', tip: 'Configure transcoding profiles for stream processing' },
    { id: 'hdhr-devices', label: 'HDHR Devices', icon: '\ud83d\udce1', tip: 'Virtual HDHomeRun devices for Plex, Jellyfin, and Emby' },
    { id: 'clients', label: 'Client Detection', icon: '\ud83d\udd0d', tip: 'Auto-detect players by HTTP headers and assign stream profiles' },
    { id: 'logos', label: 'Logos', icon: '\ud83d\uddbc', tip: 'Saved channel logos for quick reuse' },
    { section: 'Streams' },
    { section: 'System' },
    { id: 'users', label: 'Users', icon: '\ud83d\udc65', tip: 'Manage admin and user accounts' },
    { id: 'settings', label: 'Settings', icon: '\u2699', tip: 'Core application settings' },
  ];

  // Shared tooltip element
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

  // ─── EPG Guide Page ──────────────────────────────────────────────────
  function buildEpgGuidePage() {
    const HOUR_WIDTH = 240; // px per hour
    const PX_PER_MIN = HOUR_WIDTH / 60;
    const CHANNEL_COL = 180;
    let currentHours = 6;
    let windowOffset = 0; // in hours from initial start

    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading guide...'));

      let channels, groups, guideData;
      try {
        [channels, groups, guideData] = await Promise.all([
          channelsCache.getAll(),
          channelGroupsCache.getAll(),
          api.get('/api/epg/guide?hours=' + currentHours),
        ]);
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load: ' + err.message));
        return;
      }

      // Only show enabled channels with tvg_id
      channels = channels.filter(function(c) { return c.is_enabled; });
      channels.sort(function(a, b) { return a.channel_number - b.channel_number; });

      // Build group lookup
      var groupMap = {};
      groups.forEach(function(g) { groupMap[g.id] = g; });

      // Group channels by channel_group_id, sorted by group sort_order
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

      // Parse guide window timestamps
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

      // Build hour marks HTML
      var hourMarksHtml = '';
      for (var m = 0; m < windowMinutes; m += 60) {
        hourMarksHtml += '<div class="epg-hour-mark" style="width:' + HOUR_WIDTH + 'px">' + formatTime(windowStart + m * 60000) + '</div>';
      }

      // Build channel row HTML
      function buildChannelRow(ch) {
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

          programsHtml += '<div class="' + cls + '" style="left:' + leftPx + 'px;width:' + widthPx + 'px" title="' + tooltip + '">' +
            '<div class="epg-program-title">' + esc(p.title) + '</div>' +
            '<div class="epg-program-time">' + timeStr + '</div>' +
            '</div>';
        }

        // Empty slot if no programs
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
            '<span class="epg-channel-num">' + ch.channel_number + '</span>' +
            logoHtml +
            '<span class="epg-channel-name">' + esc(ch.name) + '</span>' +
          '</div>' +
          '<div class="epg-programs" style="width:' + totalWidth + 'px">' + programsHtml + '</div>' +
        '</div>';
      }

      // Build all rows grouped
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

      // Now line position
      var nowMin = (now - windowStart) / 60000;
      var nowPx = nowMin * PX_PER_MIN;
      var nowLineHtml = (nowMin >= 0 && nowMin <= windowMinutes)
        ? '<div class="epg-now-line" style="left:' + (CHANNEL_COL + nowPx) + 'px"></div>'
        : '';

      // Build toolbar
      var timeLabelEl = h('span', { className: 'epg-time-label' }, formatDate(windowStart) + ' \u2014 ' + formatDate(windowStop));

      var prevBtn = h('button', { className: 'btn btn-secondary btn-sm', onClick: navigate.bind(null, -3) }, '\u2190 Earlier');
      var nowBtn = h('button', { className: 'btn btn-primary btn-sm', onClick: navigate.bind(null, 0) }, 'Now');
      var nextBtn = h('button', { className: 'btn btn-secondary btn-sm', onClick: navigate.bind(null, 3) }, 'Later \u2192');

      var guideLoading = false;

      function navigate(offsetDelta) {
        if (guideLoading) return;
        if (offsetDelta === 0) {
          windowOffset = 0;
        } else {
          windowOffset += offsetDelta;
        }
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
        // Rebuild dynamic parts
        timeLabelEl.textContent = formatDate(windowStart) + ' \u2014 ' + formatDate(windowStop);

        hourMarksHtml = '';
        for (var m = 0; m < windowMinutes; m += 60) {
          hourMarksHtml += '<div class="epg-hour-mark" style="width:' + HOUR_WIDTH + 'px">' + formatTime(windowStart + m * 60000) + '</div>';
        }

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
          h('div', { className: 'epg-nav' }, prevBtn, nowBtn, nextBtn),
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

        // Event delegation for channel and program clicks (play)
        scrollEl.addEventListener('click', function(e) {
          var ch = e.target.closest('.epg-channel');
          if (ch) {
            openVideoPlayer(ch.dataset.chname, '/channel/' + ch.dataset.chid + '?profile=Browser', ch.dataset.tvgid || undefined);
            return;
          }
          var prog = e.target.closest('.epg-program');
          if (prog) {
            var row = prog.closest('.epg-row');
            if (!row) return;
            ch = row.querySelector('.epg-channel');
            if (!ch) return;
            openVideoPlayer(ch.dataset.chname, '/channel/' + ch.dataset.chid + '?profile=Browser', ch.dataset.tvgid || undefined);
          }
        });

        container.appendChild(toolbar);
        container.appendChild(scrollEl);

        // Auto-scroll so the now-line is centered in the visible viewport
        // nowPx is relative to timeline start; subtract half the viewport, add back the sticky channel column
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

  // ─── Stream Groups Page ──────────────────────────────────────────────
  const streamGroupsCache = Object.create(null); // accountId -> { groups, sortedGroups, groupDisplay, groupSearch }

  function buildStreamGroupsPage(accountId) {
    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      let groups, sortedGroups, groupDisplay, groupSearch;

      if (streamGroupsCache[accountId]) {
        // Use cached grouped data
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

        // Group streams by group field using plain object (faster than Map for string keys)
        groups = Object.create(null);
        for (let i = 0; i < allStreams.length; i++) {
          const g = allStreams[i].group || '';
          if (!groups[g]) groups[g] = [];
          groups[g].push(allStreams[i]);
        }

        // Sort group names: non-empty alphabetically, empty last
        sortedGroups = Object.keys(groups).sort((a, b) => {
          if (!a) return 1;
          if (!b) return -1;
          return a.localeCompare(b);
        });

        // Pre-build lowercased display names for search
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

      // Escape HTML to prevent XSS from stream names/groups
      function esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }

      // Persistent DOM
      const summaryEl = h('h3', null, '');
      const groupsContainer = h('div', null);

      const searchInput = h('input', {
        type: 'text',
        placeholder: 'Filter groups...',
        style: 'padding: 6px 10px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-primary); font-size: 13px; width: 220px; outline: none;',
      });
      searchInput.addEventListener('input', () => {
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => {
          searchTerm = searchInput.value.toLowerCase();
          renderGroups();
        }, 300);
      });

      // Event delegation: handle toggle (lazy-render tables) and play button clicks
      groupsContainer.addEventListener('toggle', (e) => {
        const details = e.target;
        if (!details.open || details.tagName !== 'DETAILS') return;
        const gIdx = details.dataset.gidx;
        if (rendered[gIdx]) return;
        rendered[gIdx] = true;
        const streams = groups[sortedGroups[gIdx]];
        const rows = [];
        for (let j = 0; j < streams.length; j++) {
          const s = streams[j];
          const logo = s.logo_url
            ? '<img class="stream-group-logo" src="' + esc(s.logo_url) + '" loading="lazy" alt="">'
            : '';
          rows.push('<tr><td style="width:40px;padding-left:40px">' + logo + '</td><td>' + esc(s.name) + '</td><td style="width:60px;text-align:right"><button class="btn btn-secondary btn-sm" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '">Play</button></td></tr>');
        }
        const tableEl = document.createElement('table');
        tableEl.className = 'stream-group-table';
        tableEl.innerHTML = '<tbody>' + rows.join('') + '</tbody>';
        details.appendChild(tableEl);
      }, true);

      groupsContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-sid]');
        if (!btn) return;
        openVideoPlayer(btn.dataset.sname, '/stream/' + btn.dataset.sid + '?profile=Browser', btn.dataset.tvgid || undefined);
      });

      function renderGroups() {
        // Track which groups were open so we can restore
        const openSet = new Set();
        groupsContainer.querySelectorAll('details[open]').forEach(el => {
          openSet.add(el.dataset.gidx);
        });

        // Filter visible groups
        let totalVisible = 0;
        const html = [];

        for (let i = 0; i < sortedGroups.length; i++) {
          if (searchTerm && groupSearch[i].indexOf(searchTerm) === -1) continue;
          const count = groups[sortedGroups[i]].length;
          totalVisible += count;
          const open = openSet.has(String(i)) ? ' open' : '';
          html.push('<details class="stream-group" data-gidx="' + i + '"' + open + '><summary>' + esc(groupDisplay[i]) + '<span class="stream-group-count">' + count + '</span></summary></details>');
        }

        summaryEl.textContent = totalVisible.toLocaleString() + ' streams in ' + html.length + ' group' + (html.length !== 1 ? 's' : '');

        if (html.length === 0) {
          groupsContainer.innerHTML = '<div style="padding:40px 16px;text-align:center;color:var(--text-muted)">' +
            (searchTerm ? 'No groups match "' + esc(searchInput.value) + '"' : 'No streams found') + '</div>';
          return;
        }

        // Re-rendered groups lose their lazy-rendered tables, so clear rendered tracking for non-open groups
        // (open groups that were re-rendered via innerHTML need their tables rebuilt on next toggle)
        for (const key in rendered) {
          if (!openSet.has(key)) delete rendered[key];
        }

        groupsContainer.innerHTML = html.join('');

        // For groups that were open, re-trigger lazy render
        groupsContainer.querySelectorAll('details[open]').forEach(el => {
          const gIdx = el.dataset.gidx;
          if (!rendered[gIdx]) {
            rendered[gIdx] = true;
            const streams = groups[sortedGroups[gIdx]];
            const rows = [];
            for (let j = 0; j < streams.length; j++) {
              const s = streams[j];
              const logo = s.logo_url
                ? '<img class="stream-group-logo" src="' + esc(s.logo_url) + '" loading="lazy" alt="">'
                : '';
              rows.push('<tr><td style="width:40px;padding-left:40px">' + logo + '</td><td>' + esc(s.name) + '</td><td style="width:60px;text-align:right"><button class="btn btn-secondary btn-sm" data-sid="' + s.id + '" data-sname="' + esc(s.name) + '" data-tvgid="' + esc(s.tvg_id || '') + '">Play</button></td></tr>');
            }
            const tableEl = document.createElement('table');
            tableEl.className = 'stream-group-table';
            tableEl.innerHTML = '<tbody>' + rows.join('') + '</tbody>';
            el.appendChild(tableEl);
          }
        });
      }

      // Build shell
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
    // Remove existing dynamic stream nav items
    navItems = navItems.filter(n => !n.id || !n.id.startsWith('streams-'));
    // Remove old dynamic page entries
    Object.keys(pages).forEach(k => { if (k.startsWith('streams-')) delete pages[k]; });
    // Find the Streams section header and insert account items after it
    const idx = navItems.findIndex(n => n.section === 'Streams');
    if (idx === -1) return;
    const accountNavItems = accounts.map(a => ({
      id: 'streams-' + a.id,
      label: a.name,
      icon: '\u25b6',
      tip: 'Streams from ' + a.name,
    }));
    navItems.splice(idx + 1, 0, ...accountNavItems);
    // Register a page for each account
    accounts.forEach(a => {
      pages['streams-' + a.id] = buildStreamGroupsPage(a.id);
    });
    // Re-render sidebar if already on screen
    if (auth.isLoggedIn()) render();
  }

  function renderSidebar() {
    const items = navItems.map(item => {
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

  // ─── Dashboard ────────────────────────────────────────────────────────
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

      // Stream count from account totals to avoid loading all streams
      const streamCount = accounts.reduce((sum, a) => sum + (a.stream_count || 0), 0);

      container.innerHTML = '';

      const cards = [
        { label: 'M3U Accounts', value: accounts.length, icon: '\u2630', page: 'm3u-accounts' },
        { label: 'Streams', value: streamCount, icon: '\u25b6', page: accounts.length ? 'streams-' + accounts[0].id : 'dashboard' },
        { label: 'Channels', value: channels.length, icon: '\ud83d\udcfa', page: 'channels' },
        { label: 'Channel Groups', value: groups.length, icon: '\ud83d\udcc2', page: 'channel-groups' },
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

      // Per-device URLs table
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

      // Data cache status section
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

      // Live-update the cache section
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

  // ─── Generic CRUD Page Builder ────────────────────────────────────────
  // Loads all data once into memory, then does client-side search/filter/pagination.
  // Only renders the visible page slice to the DOM.
  function buildCrudPage(config) {
    const perPage = config.perPage || 50;

    return async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      let allItems;
      let searchIndex; // parallel array of pre-lowercased search strings
      try {
        allItems = await api.get(config.apiPath);
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
          if (searchIndex[i].indexOf(q) !== -1) {
            result.push(allItems[i]);
          }
        }
        filteredCache = result;
        return result;
      }

      // Persistent DOM elements - built once, updated in place
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

      // Build the shell once
      function buildShell() {
        container.innerHTML = '';

        const headerActions = [];
        if (config.create) {
          headerActions.push(
            h('button', { className: 'btn btn-primary btn-sm', onClick: () => openForm(null) }, '+ Add New')
          );
        }
        if (config.extraActions) {
          config.extraActions.forEach(a => {
            headerActions.push(
              h('button', { className: 'btn btn-secondary btn-sm', onClick: () => a.handler(reloadData) }, a.label)
            );
          });
        }

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
                ...config.columns.map(col => h('th', null, col.label)),
                h('th', { style: 'width: 120px' }, 'Actions'),
              ),
            ),
            tbodyEl,
          ),
        ));
        container.appendChild(paginationEl);
      }

      // Fast update - only swaps tbody rows, count text, and pagination
      function updateTable() {
        const filtered = getFiltered();
        const totalPages = Math.max(1, Math.ceil(filtered.length / perPage));
        if (currentPage > totalPages) currentPage = totalPages;
        const start = (currentPage - 1) * perPage;
        const pageItems = filtered.slice(start, start + perPage);

        // Update count
        const countText = searchTerm
          ? config.title + ' (' + filtered.length + ' of ' + allItems.length + ')'
          : config.title + ' (' + allItems.length + ')';
        countEl.textContent = countText;

        // Swap tbody contents
        tbodyEl.innerHTML = '';
        if (pageItems.length === 0) {
          tbodyEl.appendChild(
            h('tr', { className: 'empty-row' },
              h('td', { colspan: String(config.columns.length + 1) }, searchTerm ? 'No matching items' : 'No items found'))
          );
        } else {
          for (let i = 0; i < pageItems.length; i++) {
            const item = pageItems[i];
            const tr = document.createElement('tr');
            for (let c = 0; c < config.columns.length; c++) {
              const col = config.columns[c];
              const val = col.render ? col.render(item) : item[col.key];
              const td = document.createElement('td');
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
              editBtn.className = 'btn btn-secondary btn-sm';
              editBtn.textContent = 'Edit';
              editBtn.onclick = () => openForm(item);
              actionsTd.appendChild(editBtn);
            }
            if (config.delete !== false && (typeof config.delete !== 'function' || config.delete(item))) {
              const delBtn = document.createElement('button');
              delBtn.className = 'btn btn-danger btn-sm';
              delBtn.textContent = 'Del';
              delBtn.onclick = () => deleteItem(item);
              actionsTd.appendChild(delBtn);
            }
            if (config.rowActions) {
              const actions = config.rowActions(item, reloadData);
              for (let a = 0; a < actions.length; a++) {
                const btn = document.createElement('button');
                btn.className = 'btn btn-secondary btn-sm';
                btn.textContent = actions[a].label;
                btn.onclick = actions[a].handler;
                actionsTd.appendChild(btn);
              }
            }
            tr.appendChild(actionsTd);
            tbodyEl.appendChild(tr);
          }
        }

        // Update pagination
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
          allItems = await api.get(config.apiPath);
          if (config.onDataLoaded) await config.onDataLoaded(allItems);
          buildSearchIndex();
          filteredCache = null;
          updateTable();
        } catch (err) {
          toast.error('Failed to reload: ' + err.message);
        }
      }

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
                  inp.value = opt[field.valueKey] || '';
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

            // Close dropdown on outside click
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
            if (field.loadOptions) {
              field.loadOptions().then(options => {
                for (const opt of (options || [])) {
                  const optEl = h('option', { value: String(opt[field.valueKey || 'id']) },
                    opt[field.displayKey || 'name']);
                  if (currentVal != null && String(currentVal) === String(opt[field.valueKey || 'id'])) {
                    optEl.selected = true;
                  }
                  sel.appendChild(optEl);
                }
              }).catch(() => {});
            }
            inputs[field.key] = sel;
            formEl.appendChild(h('div', { className: 'form-group' },
              h('label', { for: 'field-' + field.key }, field.label),
              sel,
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

        // showWhen: conditionally show/hide fields based on other field values
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
          // Listen for changes on all inputs
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
                body[field.key] = el.value ? (field.stringValue ? el.value : Number(el.value)) : null;
              } else if (field.type === 'async-multi-select') {
                const checked = [];
                el.querySelectorAll('input[type="checkbox"]:checked').forEach(cb => checked.push(Number(cb.value)));
                body[field.key] = checked;
              } else if (field.type === 'logo-picker') {
                body[field.key] = el._selectedLogoId || null;
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

  // ─── Video Player Modal ──────────────────────────────────────────────
  const CODEC_NAMES = {
    avc1:'H.264', h264:'H.264', hev1:'H.265', hvc1:'H.265',
    vp8:'VP8', vp9:'VP9', av01:'AV1', mp4a:'AAC', aac:'AAC',
    'ac-3':'AC-3', opus:'Opus', mp3:'MP3', flac:'FLAC'
  };
  function codecName(s) { if (!s) return '?'; return CODEC_NAMES[s.split('.')[0].toLowerCase()] || s; }

  function openVideoPlayer(title, url, tvgId) {
    let mpegtsPlayer = null;
    let retryCount = 0;
    const MAX_RETRIES = 3;
    let retryTimeout = null;
    let statsInterval = null;
    let progInterval = null;
    let nowProgram = null;
    let currentContainer = '';
    let currentCodec = '';

    function destroyPlayer() {
      if (mpegtsPlayer) {
        try { mpegtsPlayer.pause(); } catch(e) {}
        try { mpegtsPlayer.unload(); } catch(e) {}
        try { mpegtsPlayer.detachMediaElement(); } catch(e) {}
        try { mpegtsPlayer.destroy(); } catch(e) {}
        mpegtsPlayer = null;
      }
    }

    function cleanup() {
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      if (progInterval) { clearInterval(progInterval); progInterval = null; }
      destroyPlayer();
      video.oncanplay = null;
      video.onerror = null;
      video.pause();
      video.removeAttribute('src');
      video.load();
      overlay.remove();
    }

    // ── Build UI ──
    const overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.8);z-index:10000;display:flex;align-items:center;justify-content:center;';
    const modal = document.createElement('div');
    modal.style.cssText = 'background:var(--bg-card);border-radius:8px;padding:16px;max-width:800px;width:90%;position:relative;';

    // Header with title + buttons
    const header = document.createElement('div');
    header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;';
    const titleEl = document.createElement('h3');
    titleEl.style.cssText = 'margin:0;color:#e0e0e0;font-size:16px;flex:1;';
    titleEl.textContent = title;

    const btnGroup = document.createElement('div');
    btnGroup.style.cssText = 'display:flex;gap:6px;';

    const statsBtn = document.createElement('button');
    statsBtn.className = 'btn btn-sm';
    statsBtn.textContent = 'Stats';
    statsBtn.title = 'Toggle stream statistics';

    const refreshBtn = document.createElement('button');
    refreshBtn.className = 'btn btn-sm';
    refreshBtn.textContent = 'Refresh';
    refreshBtn.title = 'Restart stream';
    refreshBtn.onclick = () => { retryCount = 0; startPlayback(); };

    const closeBtn = document.createElement('button');
    closeBtn.className = 'btn btn-danger btn-sm';
    closeBtn.textContent = 'Close';
    closeBtn.onclick = cleanup;

    btnGroup.appendChild(statsBtn);
    btnGroup.appendChild(refreshBtn);
    btnGroup.appendChild(closeBtn);
    header.appendChild(titleEl);
    header.appendChild(btnGroup);
    modal.appendChild(header);

    // Programme progress bar
    const progBar = document.createElement('div');
    progBar.style.cssText = 'height:2px;background:var(--border);border-radius:1px;overflow:hidden;display:none;';
    const progFill = document.createElement('div');
    progFill.style.cssText = 'height:100%;background:var(--accent);width:0%;transition:width 1s linear;';
    progBar.appendChild(progFill);
    modal.appendChild(progBar);

    // Video container (relative for stats overlay)
    const videoWrap = document.createElement('div');
    videoWrap.style.cssText = 'position:relative;';
    const video = document.createElement('video');
    video.style.cssText = 'width:100%;max-height:450px;background:#000;border-radius:4px;';
    video.controls = true;
    video.autoplay = true;
    video.volume = parseFloat(localStorage.getItem('tvproxy_volume') || '0.5');
    video.addEventListener('volumechange', () => localStorage.setItem('tvproxy_volume', video.volume));

    // Stats overlay
    const statsOverlay = document.createElement('div');
    statsOverlay.style.cssText = 'display:none;position:absolute;top:8px;left:8px;background:rgba(0,0,0,0.75);color:#fff;padding:8px 10px;border-radius:6px;font-size:11px;font-family:monospace;pointer-events:none;line-height:1.6;z-index:10;';
    statsBtn.onclick = () => {
      statsOverlay.style.display = statsOverlay.style.display === 'none' ? 'block' : 'none';
    };
    videoWrap.appendChild(video);
    videoWrap.appendChild(statsOverlay);
    modal.appendChild(videoWrap);

    // Status bar
    const statusEl = document.createElement('div');
    statusEl.style.cssText = 'color:#999;font-size:12px;margin-top:8px;';
    modal.appendChild(statusEl);

    overlay.appendChild(modal);
    overlay.onclick = (e) => { if (e.target === overlay) cleanup(); };
    document.body.appendChild(overlay);

    // ── Stats updater ──
    function updateStats() {
      if (!mpegtsPlayer || !mpegtsPlayer.statisticsInfo) return;
      const stats = mpegtsPlayer.statisticsInfo;
      const mi = mpegtsPlayer.mediaInfo || {};
      const res = (video.videoWidth && video.videoHeight) ? video.videoWidth + 'x' + video.videoHeight : '?';
      const speed = stats.speed != null ? (stats.speed / 1024).toFixed(2) + ' MB/s' : '?';
      const fps = mi.fps || '?';
      const dropped = (stats.droppedFrames != null) ? stats.droppedFrames : '?';
      const buf = video.buffered.length > 0 ? (video.buffered.end(0) - video.currentTime).toFixed(1) + 's' : '0s';
      statsOverlay.innerHTML =
        'Res: ' + res + '<br>' +
        'Speed: ' + speed + '<br>' +
        'FPS: ' + fps + '<br>' +
        'Dropped: ' + dropped + '<br>' +
        'Buffer: ' + buf + '<br>' +
        'Video: ' + codecName(mi.videoCodec) + '<br>' +
        'Audio: ' + codecName(mi.audioCodec);
    }

    // ── EPG Now Playing ──
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

    function updateProgress() {
      if (!nowProgram || !nowProgram.start || !nowProgram.stop) return;
      const start = new Date(nowProgram.start).getTime();
      const stop = new Date(nowProgram.stop).getTime();
      const now = Date.now();
      const pct = Math.max(0, Math.min(100, ((now - start) / (stop - start)) * 100));
      progFill.style.width = pct + '%';
      progBar.style.display = 'block';
      if (pct >= 100 && progInterval) {
        clearInterval(progInterval);
        progInterval = null;
        fetchNowPlaying();
      }
    }

    function fetchNowPlaying() {
      if (!tvgId) return;
      api.get('/api/epg/now?channel_id=' + encodeURIComponent(tvgId)).then(program => {
        if (program && program.title) {
          nowProgram = program;
          updateStatusText();
          updateProgress();
          if (progInterval) clearInterval(progInterval);
          progInterval = setInterval(updateProgress, 10000);
        }
      }).catch(() => {});
    }

    // ── Playback ──
    // Detect if the URL uses a profile that outputs fMP4/MP4 (native playback)
    const isBrowserProfile = url.includes('profile=Browser');

    function startPlayback() {
      destroyPlayer();
      if (retryTimeout) { clearTimeout(retryTimeout); retryTimeout = null; }
      if (statsInterval) { clearInterval(statsInterval); statsInterval = null; }
      if (progInterval) { clearInterval(progInterval); progInterval = null; }
      currentContainer = '';
      currentCodec = '';
      nowProgram = null;
      progBar.style.display = 'none';
      progFill.style.width = '0%';
      video.removeAttribute('src');

      if (isBrowserProfile) {
        // Browser profile outputs fMP4 — use native HTML5 video
        statusEl.style.color = '#999';
        statusEl.textContent = 'Connecting (fMP4)...';
        video.src = url;
        video.oncanplay = () => {
          statusEl.style.color = '#4caf50';
          currentContainer = 'fMP4';
          retryCount = 0;
          updateStatusText();
          fetchNowPlaying();
        };
        video.play().catch(() => handleRetry());
      } else if (typeof mpegts !== 'undefined' && mpegts.isSupported()) {
        // MPEG-TS streams — use mpegts.js
        statusEl.style.color = '#999';
        statusEl.textContent = 'Connecting via mpegts.js...';
        mpegtsPlayer = mpegts.createPlayer({
          type: 'mse',
          isLive: true,
          url: url,
        }, {
          enableStashBuffer: true,
          stashInitialSize: 4096,
          liveBufferLatency: 2.0,
        });

        mpegtsPlayer.attachMediaElement(video);
        mpegtsPlayer.load();
        mpegtsPlayer.play();

        mpegtsPlayer.on(mpegts.Events.ERROR, (errorType, errorDetail) => {
          console.warn('mpegts.js error:', errorType, errorDetail);
          if (errorType === 'NetworkError' || errorType === 'MediaError') {
            handleRetry();
          } else {
            statusEl.style.color = '#ff6b6b';
            statusEl.textContent = 'Error: ' + errorDetail;
          }
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
        // No mpegts.js — try native video as last resort
        statusEl.style.color = '#999';
        statusEl.textContent = 'Trying native playback...';
        video.src = url;
        video.play().catch(() => {
          statusEl.style.color = '#ff6b6b';
          statusEl.textContent = 'Playback failed. Try a Browser (fMP4) profile.';
        });
      }
    }

    function handleRetry() {
      if (retryCount >= MAX_RETRIES) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Stream failed after ' + MAX_RETRIES + ' retries.';
        destroyPlayer();
        return;
      }
      retryCount++;
      statusEl.style.color = '#ffa726';
      statusEl.textContent = 'Retrying... (' + retryCount + '/' + MAX_RETRIES + ')';
      destroyPlayer();
      retryTimeout = setTimeout(startPlayback, 2000);
    }

    video.onerror = () => {
      if (!mpegtsPlayer) {
        statusEl.style.color = '#ff6b6b';
        statusEl.textContent = 'Playback failed. Try a Browser (fMP4) profile.';
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

  // ─── Page Definitions ─────────────────────────────────────────────────
  const pages = {
    dashboard: renderDashboard,

    'm3u-accounts': buildCrudPage({
      title: 'M3U Accounts',
      singular: 'M3U Account',
      apiPath: '/api/m3u/accounts',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
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
      create: true,
      update: true,
      onChange: () => channelsCache.invalidate(),
      columns: [
        { key: 'channel_number', label: '#' },
        { key: 'name', label: 'Name' },
        { key: 'tvg_id', label: 'EPG ID', render: item => item.tvg_id || '-' },
        { key: 'logo', label: 'Logo', render: item =>
          item.logo ? h('img', { src: item.logo, style: 'height:24px;width:24px;object-fit:contain;border-radius:2px;' }) : '-'
        },
        { key: 'is_enabled', label: 'Status', render: item =>
          h('span', { className: 'badge ' + (item.is_enabled ? 'badge-success' : 'badge-danger') }, item.is_enabled ? 'Enabled' : 'Disabled')
        },
      ],
      fields: [
        { key: 'name', label: 'Channel Name', placeholder: 'BBC One' },
        { key: 'channel_number', label: 'Channel Number', type: 'number', default: 0, help: 'Leave as 0 for auto-assign' },
        {
          key: 'tvg_id', label: 'EPG Channel ID', type: 'autocomplete',
          placeholder: 'Search EPG channels...',
          help: 'Type to search EPG channels. Auto-matches when you enter a channel name above.',
          cache: epgCache,
          valueKey: 'channel_id',
          displayKey: 'name',
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
        },
        {
          key: 'channel_profile_id', label: 'Channel Profile', type: 'async-select',
          emptyLabel: '-- No Profile --',
          loadOptions: () => api.get('/api/channel-profiles'),
          valueKey: 'id', displayKey: 'name',
          help: 'Assign a profile to control which HDHR devices expose this channel',
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
        { label: 'Play', handler: () => openVideoPlayer(item.name, '/channel/' + item.id + '?profile=Browser', item.tvg_id || undefined) },
      ],
      postFormSetup: (inputs, isEdit, item) => {
        // Load existing stream assignment when editing
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
        // Auto-match EPG when channel name is entered
        if (inputs.name) {
          inputs.name.addEventListener('blur', async () => {
            const nameVal = inputs.name.value.trim();
            if (!nameVal) return;
            // Only auto-match if tvg_id is empty
            if (inputs.tvg_id && inputs.tvg_id.value) return;

            const epgData = await epgCache.getAll();
            if (!epgData.length) return;

            // Normalize name for matching: lowercase, remove common suffixes
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
              inputs.tvg_id.value = bestMatch.channel_id;
              if (bestMatch.icon && inputs.logo_id && !inputs.logo_id._selectedLogoId) {
                findOrCreateLogoByUrl(bestMatch.icon, bestMatch.name, inputs);
              }
              toast.info('Auto-matched EPG: ' + bestMatch.name + ' (' + bestMatch.channel_id + ')');
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

    'channel-profiles': buildCrudPage({
      title: 'Channel Profiles',
      singular: 'Channel Profile',
      apiPath: '/api/channel-profiles',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'stream_profile', label: 'Stream Profile', render: item => item.stream_profile || '-' },
        { key: 'sort_order', label: 'Sort Order' },
      ],
      fields: [
        { key: 'name', label: 'Profile Name', placeholder: 'Default Profile' },
        {
          key: 'stream_profile', label: 'Stream Profile', type: 'async-select',
          emptyLabel: '-- Default --',
          stringValue: true,
          loadOptions: async () => {
            const profiles = await api.get('/api/stream-profiles');
            return (profiles || []).map(p => ({ id: p.name, name: p.name }));
          },
          valueKey: 'id', displayKey: 'name',
          help: 'Stream processing profile to use for channels in this profile',
        },
        { key: 'sort_order', label: 'Sort Order', type: 'number', default: 0 },
      ],
    }),

    'epg-sources': buildCrudPage({
      title: 'EPG Sources',
      singular: 'EPG Source',
      apiPath: '/api/epg/sources',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'url', label: 'URL', render: item => {
          const url = item.url || '';
          return url.length > 50 ? url.substring(0, 50) + '...' : url;
        }},
      ],
      fields: [
        { key: 'name', label: 'Source Name', placeholder: 'TV Guide' },
        { key: 'url', label: 'XMLTV URL', placeholder: 'http://epg-provider.com/guide.xml' },
      ],
      rowActions: (item, reload) => [
        {
          label: 'Refresh',
          handler: async () => {
            try {
              await api.post('/api/epg/sources/' + item.id + '/refresh');
              epgCache.invalidate(); // invalidate EPG cache after refresh
              toast.success('EPG refresh started for ' + item.name);
              setTimeout(reload, 2000);
            } catch (err) {
              toast.error(err.message);
            }
          },
        },
      ],
    }),

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
          key: 'channel_profile_id', label: 'Channel Profile', type: 'async-select',
          emptyLabel: '-- Default --',
          loadOptions: () => api.get('/api/channel-profiles'),
          valueKey: 'id', displayKey: 'name',
          help: 'Stream profile used for channels on this device.',
        },
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

        const profileSelect = h('select');
        profiles.forEach(p => {
          const opt = h('option', { value: String(p.id) }, p.name);
          if (existing && existing.stream_profile_id === p.id) opt.selected = true;
          profileSelect.appendChild(opt);
        });

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
                stream_profile_id: parseInt(profileSelect.value, 10),
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
              // Refresh profiles since a new one was auto-created
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
          formContent.appendChild(h('div', { className: 'form-group' }, h('label', null, 'Stream Profile'), profileSelect,
            h('small', { style: 'color: var(--text-muted); display: block' }, 'Auto-created on client creation. Edit the profile to change encoding settings.')));
        }

        formContent.appendChild(h('div', { className: 'form-group' },
          h('label', { style: 'display:flex;align-items:center;gap:8px' }, enabledChk, 'Enabled')));
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
      ],
      fields: [
        { key: 'username', label: 'Username', placeholder: 'john' },
        { key: 'password', label: 'Password', type: 'password', placeholder: 'Enter password' },
        { key: 'is_admin', label: 'Administrator', type: 'checkbox', default: false },
      ],
    }),

    settings: async function(container) {
      container.innerHTML = '';
      container.appendChild(h('div', { className: 'loading-page' }, h('div', { className: 'spinner' }), 'Loading...'));

      try {
        const settings = await api.get('/api/settings');
        container.innerHTML = '';

        const inputs = {};
        const formEl = h('div');

        (Array.isArray(settings) ? settings : []).forEach(s => {
          const inp = h('input', { type: 'text', id: 'setting-' + s.key });
          inp.value = s.value || '';
          inputs[s.key] = inp;
          formEl.appendChild(h('div', { className: 'form-group' },
            h('label', { for: 'setting-' + s.key }, s.key),
            inp,
          ));
        });

        if (Object.keys(inputs).length === 0) {
          formEl.appendChild(h('p', { style: 'color: var(--text-muted)' }, 'No settings configured yet.'));
        }

        const saveBtn = h('button', { className: 'btn btn-primary', onClick: async () => {
          saveBtn.disabled = true;
          try {
            const body = {};
            for (const [k, inp] of Object.entries(inputs)) {
              body[k] = inp.value;
            }
            await api.put('/api/settings', body);
            toast.success('Settings saved');
          } catch (err) {
            toast.error(err.message);
          }
          saveBtn.disabled = false;
        }}, 'Save Settings');

        container.appendChild(h('div', { className: 'table-container' },
          h('div', { className: 'table-header' }, h('h3', null, 'Application Settings')),
          h('div', { style: 'padding: 16px' }, formEl,
            Object.keys(inputs).length > 0 ? h('div', { style: 'margin-top: 16px' }, saveBtn) : null,
          ),
        ));
      } catch (err) {
        container.innerHTML = '';
        container.appendChild(h('p', { style: 'color: var(--danger)' }, 'Failed to load settings: ' + err.message));
      }
    },
  };

  // ─── Main Render ──────────────────────────────────────────────────────
  function render() {
    if (!auth.isLoggedIn()) {
      renderLoginPage();
      return;
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

  // ─── Init ─────────────────────────────────────────────────────────────
  async function init() {
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

  init();
})();
