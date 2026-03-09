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
  const navItems = [
    { section: 'Overview' },
    { id: 'dashboard', label: 'Dashboard', icon: '\u2302' },
    { section: 'Sources' },
    { id: 'm3u-accounts', label: 'M3U Accounts', icon: '\u2630' },
    { id: 'streams', label: 'Streams', icon: '\u25b6' },
    { id: 'epg-sources', label: 'EPG Sources', icon: '\ud83d\udcc5' },
    { section: 'Channels' },
    { id: 'channels', label: 'Channels', icon: '\ud83d\udcfa' },
    { id: 'channel-groups', label: 'Channel Groups', icon: '\ud83d\udcc2' },
    { id: 'channel-profiles', label: 'Channel Profiles', icon: '\u2699' },
    { section: 'Configuration' },
    { id: 'stream-profiles', label: 'Stream Profiles', icon: '\ud83d\udd27' },
    { id: 'hdhr-devices', label: 'HDHR Devices', icon: '\ud83d\udce1' },
    { id: 'user-agents', label: 'User Agents', icon: '\ud83c\udf10' },
    { id: 'logos', label: 'Logos', icon: '\ud83d\uddbc' },
    { section: 'System' },
    { id: 'users', label: 'Users', icon: '\ud83d\udc65' },
    { id: 'settings', label: 'Settings', icon: '\u2699' },
  ];

  function renderSidebar() {
    const items = navItems.map(item => {
      if (item.section) {
        return h('div', { className: 'nav-section' }, item.section);
      }
      return h('div', {
        className: 'nav-item' + (state.currentPage === item.id ? ' active' : ''),
        onClick: () => navigate(item.id),
      },
        h('span', { className: 'icon' }, item.icon),
        item.label,
      );
    });

    return h('nav', { className: 'sidebar' },
      h('div', { className: 'sidebar-header' },
        h('h2', null, 'TVProxy'),
        h('span', { className: 'version' }, 'IPTV Management'),
      ),
      h('div', { className: 'sidebar-nav' }, ...items),
      h('div', { className: 'sidebar-footer' },
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
        api.get('/api/channels').catch(() => []),
        api.get('/api/channel-groups').catch(() => []),
        api.get('/api/epg/sources').catch(() => []),
        api.get('/api/hdhr/devices').catch(() => []),
      ]);

      // Stream count from account totals to avoid loading all streams
      const streamCount = accounts.reduce((sum, a) => sum + (a.stream_count || 0), 0);

      container.innerHTML = '';

      const cards = [
        { label: 'M3U Accounts', value: accounts.length, icon: '\u2630' },
        { label: 'Streams', value: streamCount, icon: '\u25b6' },
        { label: 'Channels', value: channels.length, icon: '\ud83d\udcfa' },
        { label: 'Channel Groups', value: groups.length, icon: '\ud83d\udcc2' },
        { label: 'EPG Sources', value: epgSources.length, icon: '\ud83d\udcc5' },
        { label: 'HDHR Devices', value: devices.length, icon: '\ud83d\udce1' },
      ];

      const grid = h('div', { className: 'dashboard-grid' },
        ...cards.map(c =>
          h('div', { className: 'stat-card' },
            h('div', { className: 'stat-icon' }, c.icon),
            h('div', { className: 'stat-label' }, c.label),
            h('div', { className: 'stat-value' }, String(c.value)),
          )
        ),
      );

      const outputLinks = h('div', { className: 'table-container', style: 'margin-top: 24px' },
        h('div', { className: 'table-header' }, h('h3', null, 'Output Links')),
        h('div', { style: 'padding: 16px' },
          h('p', { style: 'margin-bottom: 12px' },
            h('strong', null, 'M3U Playlist: '),
            h('a', { href: '/output/m3u', target: '_blank' }, window.location.origin + '/output/m3u'),
          ),
          h('p', { style: 'margin-bottom: 12px' },
            h('strong', null, 'EPG (XMLTV): '),
            h('a', { href: '/output/epg', target: '_blank' }, window.location.origin + '/output/epg'),
          ),
          h('p', null,
            h('strong', null, 'HDHR Discover: '),
            h('a', { href: '/hdhr/discover.json', target: '_blank' }, window.location.origin + '/hdhr/discover.json'),
          ),
        ),
      );

      container.appendChild(grid);
      container.appendChild(outputLinks);
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
            if (config.update) {
              const editBtn = document.createElement('button');
              editBtn.className = 'btn btn-secondary btn-sm';
              editBtn.textContent = 'Edit';
              editBtn.onclick = () => openForm(item);
              actionsTd.appendChild(editBtn);
            }
            if (config.delete !== false) {
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

        showModal(
          isEdit ? 'Edit ' + config.singular : 'Add ' + config.singular,
          formEl,
          async () => {
            const body = {};
            fields.forEach(field => {
              if (field.readOnly && isEdit) return;
              const el = inputs[field.key];
              if (field.type === 'checkbox') {
                body[field.key] = el.checked;
              } else if (field.type === 'number') {
                body[field.key] = el.value ? Number(el.value) : 0;
              } else {
                body[field.key] = el.value;
              }
            });
            if (isEdit) {
              await api.put(config.apiPath + '/' + item.id, body);
              toast.success(config.singular + ' updated');
            } else {
              await api.post(config.apiPath, body);
              toast.success(config.singular + ' created');
            }
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
          await reloadData();
        } catch (err) {
          toast.error(err.message);
        }
      }

      buildShell();
      updateTable();
    };
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
              toast.success('Refresh started for ' + item.name);
              setTimeout(reload, 2000);
            } catch (err) {
              toast.error(err.message);
            }
          },
        },
      ],
    }),

    streams: buildCrudPage({
      title: 'Streams',
      singular: 'Stream',
      apiPath: '/api/streams',
      perPage: 100,
      create: false,
      update: false,
      delete: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'group', label: 'Group', render: item => item.group || '-' },
        { key: 'm3u_account_id', label: 'Account ID' },
      ],
      searchKeys: ['name', 'group'],
      fields: [],
    }),

    channels: buildCrudPage({
      title: 'Channels',
      singular: 'Channel',
      apiPath: '/api/channels',
      create: true,
      update: true,
      columns: [
        { key: 'channel_number', label: '#' },
        { key: 'name', label: 'Name' },
        { key: 'channel_group_id', label: 'Group ID' },
        { key: 'is_enabled', label: 'Status', render: item =>
          h('span', { className: 'badge ' + (item.is_enabled ? 'badge-success' : 'badge-danger') }, item.is_enabled ? 'Enabled' : 'Disabled')
        },
      ],
      fields: [
        { key: 'name', label: 'Channel Name', placeholder: 'BBC One' },
        { key: 'channel_number', label: 'Channel Number', type: 'number', default: 0 },
        { key: 'logo', label: 'Logo URL', placeholder: 'https://...' },
        { key: 'tvg_id', label: 'EPG Channel ID', placeholder: 'BBC.uk' },
        { key: 'channel_group_id', label: 'Group ID', type: 'number', default: 0, help: 'Channel group ID (0 for none)' },
        { key: 'is_enabled', label: 'Enabled', type: 'checkbox', default: true },
      ],
    }),

    'channel-groups': buildCrudPage({
      title: 'Channel Groups',
      singular: 'Channel Group',
      apiPath: '/api/channel-groups',
      create: true,
      update: true,
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
        { key: 'sort_order', label: 'Sort Order' },
      ],
      fields: [
        { key: 'name', label: 'Profile Name', placeholder: 'Default Profile' },
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
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'command', label: 'Command', render: item => item.command || '-' },
      ],
      fields: [
        { key: 'name', label: 'Profile Name', placeholder: 'Direct Proxy' },
        { key: 'command', label: 'Command', placeholder: 'ffmpeg -i {input} ...', help: 'Transcoding command template. Use {input} and {output} placeholders.' },
        { key: 'args', label: 'Arguments', type: 'textarea', placeholder: '-c copy' },
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
        { key: 'tuner_count', label: 'Tuners' },
      ],
      fields: [
        { key: 'name', label: 'Device Name', placeholder: 'TVProxy HDHR' },
        { key: 'device_id', label: 'Device ID', placeholder: '12345678', help: '8-character hex device ID' },
        { key: 'tuner_count', label: 'Tuner Count', type: 'number', default: 2 },
        { key: 'channel_profile_id', label: 'Channel Profile ID', type: 'number', default: 0 },
      ],
    }),

    logos: buildCrudPage({
      title: 'Logos',
      singular: 'Logo',
      apiPath: '/api/logos',
      create: true,
      update: false,
      columns: [
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

    'user-agents': buildCrudPage({
      title: 'User Agents',
      singular: 'User Agent',
      apiPath: '/api/user-agents',
      create: true,
      update: true,
      columns: [
        { key: 'name', label: 'Name' },
        { key: 'user_agent', label: 'User Agent String', render: item => {
          const ua = item.user_agent || '';
          return ua.length > 60 ? ua.substring(0, 60) + '...' : ua;
        }},
      ],
      fields: [
        { key: 'name', label: 'Name', placeholder: 'VLC Player' },
        { key: 'user_agent', label: 'User Agent String', placeholder: 'VLC/3.0.18 LibVLC/3.0.18' },
      ],
    }),

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
  }

  init();
})();
