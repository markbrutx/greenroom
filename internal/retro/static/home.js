(function () {
  'use strict';

  const ROOMS = 'moored:rooms';
  const box = document.getElementById('my-rooms');
  const list = document.getElementById('my-rooms-list');
  const tpl = document.getElementById('tpl-room');
  if (!box || !list || !tpl) return;

  function load() {
    try { return JSON.parse(localStorage.getItem(ROOMS) || '[]'); } catch (e) { return []; }
  }
  function save(rooms) {
    try { localStorage.setItem(ROOMS, JSON.stringify(rooms)); } catch (e) {}
  }
  function drop(code) {
    save(load().filter((r) => r.code !== code));
    render();
  }
  function ago(ms) {
    const m = Math.round((Date.now() - ms) / 60000);
    if (m < 1) return 'just now';
    if (m < 60) return m + ' min ago';
    const h = Math.round(m / 60);
    if (h < 24) return h + ' h ago';
    const d = Math.round(h / 24);
    return d === 1 ? 'yesterday' : d + ' days ago';
  }

  function render() {
    const rooms = load().filter((r) => r && /^[a-z]{3}-[a-z]{4}-[a-z]{3}$/.test(r.code));
    list.replaceChildren(...rooms.map((r) => {
      const el = tpl.content.firstElementChild.cloneNode(true);
      const a = el.querySelector('a');
      a.href = '/' + r.code;
      el.querySelector('.room-title').textContent = r.warmup || 'Retro';
      el.querySelector('.room-meta').textContent = [r.code, r.date, ago(r.seen || 0)].filter(Boolean).join(' · ');
      el.querySelector('.room-host').hidden = !r.host;
      const x = el.querySelector('.room-forget');
      x.addEventListener('click', () => drop(r.code));
      x.setAttribute('aria-label', 'Remove ' + r.code + ' from this list');
      return el;
    }));
    box.hidden = rooms.length === 0;
    document.body.classList.toggle('has-rooms', rooms.length > 0);
  }

  render();

  load().slice(0, 12).forEach((r) => {
    fetch('/' + r.code + '/state', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then((res) => { if (res.status === 404 || res.status === 403) drop(r.code); })
      .catch(() => {});
  });
})();
