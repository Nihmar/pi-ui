/* pi-ui — the site's own JavaScript: no framework, no bundler, no dependency.
 *
 * Every behaviour here is a progressive enhancement: with the script blocked the page is
 * still readable, the first mockup↔app pair is still shown and the code blocks are still
 * selectable by hand.
 */
(() => {
  'use strict';

  /* ------------------------------------------------------------------ theme */

  const THEME_KEY = 'piui-site-theme';
  const themeButton = document.getElementById('theme');

  themeButton?.addEventListener('click', () => {
    const next = document.documentElement.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem(THEME_KEY, next);
  });

  /* ------------------------------------------------------------- mobile nav */

  const menuButton = document.getElementById('menu');
  const nav = document.getElementById('main-nav');

  const setMenu = (open) => {
    if (!nav || !menuButton) return;
    nav.hidden = !open;
    menuButton.setAttribute('aria-expanded', String(open));
  };

  menuButton?.addEventListener('click', () => setMenu(!!nav?.hidden));
  nav?.addEventListener('click', (event) => {
    if (event.target instanceof HTMLAnchorElement) setMenu(false);
  });

  /* ----------------------------------------------------------- active link */

  const links = [...(nav?.querySelectorAll('a') ?? [])];
  const sections = links
    .map((link) => document.querySelector(link.getAttribute('href')))
    .filter((section) => section !== null);

  if ('IntersectionObserver' in window && sections.length > 0) {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          for (const link of links) {
            const current = link.getAttribute('href') === `#${entry.target.id}`;
            if (current) link.setAttribute('aria-current', 'true');
            else link.removeAttribute('aria-current');
          }
        }
      },
      { rootMargin: '-45% 0px -50% 0px' },
    );
    for (const section of sections) observer.observe(section);
  }

  /* ------------------------------------------------------------ copy button */

  for (const block of document.querySelectorAll('.code')) {
    const pre = block.querySelector('pre');
    if (!pre) continue;
    const button = document.createElement('button');
    button.className = 'copy';
    button.type = 'button';
    button.textContent = 'Copy';
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(pre.innerText);
        button.textContent = 'Copied';
      } catch {
        button.textContent = 'Select it by hand';
      }
      setTimeout(() => {
        button.textContent = 'Copy';
      }, 1400);
    });
    block.append(button);
  }

  /* ------------------------------------------------- mockup → app comparison */

  const pairSelect = document.getElementById('pair');
  const pairNote = document.getElementById('pair-note');
  const pairs = [...document.querySelectorAll('.pair-set')];

  const showPair = (id) => {
    for (const pair of pairs) pair.hidden = pair.dataset.pair !== id;
    const shown = pairs.find((pair) => pair.dataset.pair === id);
    if (pairNote && shown) pairNote.textContent = shown.dataset.note ?? '';
  };

  if (pairs.length > 0) {
    pairSelect?.addEventListener('change', () => showPair(pairSelect.value));
    showPair(pairSelect?.value ?? pairs[0].dataset.pair);
  }

  /* ---------------------------------------------------------------- gallery */

  const gallery = document.getElementById('gallery-grid');
  const tiles = gallery ? [...gallery.querySelectorAll('.tile')] : [];
  const groupChips = [...document.querySelectorAll('[data-filter="group"]')];
  const deviceChips = [...document.querySelectorAll('[data-filter="device"]')];
  const sourceChips = [...document.querySelectorAll('[data-filter="source"]')];
  const search = document.getElementById('gallery-search');
  const count = document.getElementById('gallery-count');
  const emptyNote = document.getElementById('gallery-empty');
  const resetChips = document.getElementById('gallery-reset');

  if (tiles.length > 0) {
    const summary = count?.textContent ?? '';
    const state = { group: 'all', device: null, source: null, query: '' };

    const matches = (tile) => {
      if (state.group !== 'all' && tile.dataset.group !== state.group) return false;
      if (state.device && tile.dataset.device !== state.device) return false;
      if (state.source && tile.dataset.source !== state.source) return false;
      if (state.query) {
        const haystack = `${tile.dataset.title} ${tile.dataset.slug} ${tile.dataset.group}`.toLowerCase();
        if (!haystack.includes(state.query)) return false;
      }
      return true;
    };

    const visibleTiles = () => tiles.filter((tile) => !tile.hidden);

    const applyFilters = () => {
      let shown = 0;
      for (const tile of tiles) {
        const match = matches(tile);
        tile.hidden = !match;
        if (match) shown += 1;
      }
      for (const chip of [...groupChips, ...deviceChips, ...sourceChips]) {
        const value = chip.dataset.value;
        const pressed =
          (chip.dataset.filter === 'group' && state.group === value) ||
          (chip.dataset.filter === 'device' && state.device === value) ||
          (chip.dataset.filter === 'source' && state.source === value);
        chip.setAttribute('aria-pressed', String(pressed));
      }
      if (count) {
        count.textContent = shown === tiles.length ? summary : `${shown} of ${tiles.length} screens match.`;
      }
      emptyNote?.toggleAttribute('hidden', shown > 0);
    };

    const toggleChip = (chips, key, value) => {
      const pressed = chips.find((chip) => chip.dataset.value === value)?.getAttribute('aria-pressed') === 'true';
      state[key] = pressed ? null : value;
      if (key === 'group' && state.group === null) state.group = 'all';
      applyFilters();
    };

    for (const chip of groupChips) chip.addEventListener('click', () => toggleChip(groupChips, 'group', chip.dataset.value));
    for (const chip of deviceChips) chip.addEventListener('click', () => toggleChip(deviceChips, 'device', chip.dataset.value));
    for (const chip of sourceChips) chip.addEventListener('click', () => toggleChip(sourceChips, 'source', chip.dataset.value));

    search?.addEventListener('input', () => {
      state.query = search.value.trim().toLowerCase();
      applyFilters();
    });

    const clearFilters = () => {
      state.group = 'all';
      state.device = null;
      state.source = null;
      state.query = '';
      if (search) search.value = '';
      applyFilters();
    };

    resetChips?.addEventListener('click', clearFilters);

    /* -------------------------------------------------------------- lightbox */

    const lightbox = document.getElementById('lightbox');
    const lightboxImage = document.getElementById('lightbox-image');
    const lightboxTitle = document.getElementById('lightbox-title');
    const lightboxCounter = document.getElementById('lightbox-counter');
    const lightboxMeta = document.getElementById('lightbox-meta');
    const lightboxLive = document.getElementById('lightbox-live');
    const lightboxFull = document.getElementById('lightbox-full');
    const themeName = () => (document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark');
    const imageOf = (tile, theme) => (theme === 'light' ? tile.dataset.light : tile.dataset.dark);
    const thumbOf = (tile, theme) => (theme === 'light' ? tile.dataset.thumbLight : tile.dataset.thumbDark);
    let current = null;
    let opener = null;

    /** The renders exist in both themes: whatever the page shows follows the theme toggle. */
    const syncTheme = () => {
      const theme = themeName();
      for (const tile of tiles) {
        const image = tile.querySelector('img');
        const source = thumbOf(tile, theme) || imageOf(tile, theme);
        if (image && source && image.getAttribute('src') !== source) image.setAttribute('src', source);
      }
      if (current) {
        const source = imageOf(current, theme);
        if (source && lightboxImage) {
          lightboxImage.setAttribute('src', source);
          lightboxImage.alt = `${current.dataset.title} (${theme} theme)`;
        }
        if (source && lightboxFull) lightboxFull.setAttribute('href', source);
      }
    };

    const show = (tile) => {
      if (!tile) return;
      current = tile;
      const theme = themeName();
      const group = tile.querySelector('.meta span:last-child')?.textContent ?? '';
      const device = tile.dataset.device === 'desktop' ? 'desktop' : 'phone';
      const source = tile.dataset.source === 'app' ? 'rendered from the Flutter client' : 'mockup screen';
      if (lightbox) lightbox.dataset.device = tile.dataset.device;
      if (lightboxTitle) lightboxTitle.textContent = tile.dataset.title;
      if (lightboxMeta) lightboxMeta.textContent = `${group} · ${device} · ${source}`;
      if (lightboxCounter) {
        const order = visibleTiles();
        lightboxCounter.textContent = `${order.indexOf(tile) + 1} / ${order.length}`;
      }
      if (lightboxLive) {
        lightboxLive.hidden = !tile.dataset.live;
        if (tile.dataset.live) lightboxLive.setAttribute('href', tile.dataset.live);
      }
      syncTheme();
    };

    const openTile = (tile, trigger) => {
      if (!lightbox) return;
      opener = trigger ?? null;
      show(tile);
      lightbox.hidden = false;
      document.body.style.overflow = 'hidden';
      document.getElementById('lightbox-close')?.focus();
    };

    const closeLightbox = () => {
      if (!lightbox) return;
      lightbox.hidden = true;
      document.body.style.overflow = '';
      current = null;
      opener?.focus();
    };

    const step = (delta) => {
      const order = visibleTiles();
      if (order.length === 0 || !current) return;
      const index = order.indexOf(current);
      show(order[(index + delta + order.length) % order.length]);
    };

    for (const tile of tiles) {
      const button = tile.querySelector('button.open');
      button?.addEventListener('click', () => openTile(tile, button));
      tile.querySelector('img')?.addEventListener('dblclick', () => {
        if (tile.dataset.live) window.location.href = tile.dataset.live;
      });
    }

    document.getElementById('lightbox-close')?.addEventListener('click', closeLightbox);
    document.getElementById('lightbox-prev')?.addEventListener('click', () => step(-1));
    document.getElementById('lightbox-next')?.addEventListener('click', () => step(1));
    lightbox?.addEventListener('click', (event) => {
      if (event.target === lightbox || event.target?.classList.contains('stage')) closeLightbox();
    });

    document.addEventListener('keydown', (event) => {
      const typing = event.target instanceof HTMLInputElement;
      if (lightbox && !lightbox.hidden) {
        if (event.key === 'Escape') closeLightbox();
        else if (event.key === 'ArrowRight') step(1);
        else if (event.key === 'ArrowLeft') step(-1);
        return;
      }
      if (event.key === '/' && !typing && search) {
        event.preventDefault();
        search.focus();
      }
      if (event.key === 'Escape' && typing && search?.value) clearFilters();
    });

    // The theme button above swaps the theme; the captures have to follow it.
    themeButton?.addEventListener('click', syncTheme);

    applyFilters();
    syncTheme();
  }
})();
