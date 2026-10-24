(function () {
  'use strict';

  const S = JSON.parse(document.getElementById('retro-state').textContent);
  const me = S.me;
  const rid = S.id;
  const EMOJI = S.emojis;
  const EVERYONE = 'Everyone';

  const $ = (sel, root) => (root || document).querySelector(sel);
  const tplCard = $('#tpl-card').content.firstElementChild;
  const tplCol = $('#tpl-col').content.firstElementChild;
  const picker = $('#picker');
  const board = $('#board');

  const cards = new Map();
  const pending = new Map();
  const boundTmps = new Set();
  let cols = [];
  let openEl = null;
  let people = (S.names || []).slice();
  let here = [];
  let toastTimer = 0;
  const fieldSizing = typeof CSS !== 'undefined' && CSS.supports && CSS.supports('field-sizing', 'content');

  function toast(text) {
    const t = $('#toast');
    t.textContent = text;
    t.classList.add('on');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove('on'), 3200);
  }

  async function api(body) {
    let r;
    try {
      r = await fetch('/retro/' + rid + '/do', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Greenroom': '1' },
        body: JSON.stringify(body),
      });
    } catch (e) {
      toast('No connection to the server. Your last change was not saved.');
      throw e;
    }
    if (r.status === 401) {
      location.href = '/login?next=' + encodeURIComponent(location.pathname);
      throw new Error('signed out');
    }
    if (!r.ok) {
      const msg = (await r.text()).trim();
      toast(msg || 'The server refused the change (' + r.status + ').');
      throw new Error(msg);
    }
    return r.json().catch(() => ({}));
  }

  function grow(ta) {
    if (fieldSizing) return;
    ta.style.height = 'auto';
    ta.style.height = ta.scrollHeight + 'px';
  }

  function isBoardCol(col) { return col !== 'a' && col !== 'w'; }

  function boxFor(col) {
    const c = document.getElementById('col-' + col);
    return c ? $('.cards', c) : null;
  }

  function hasReactions(c) {
    return Object.keys(c.r || {}).some((e) => c.r[e].length > 0);
  }

  function peopleList() {
    const list = people.slice();
    here.forEach((n) => { if (!list.includes(n)) list.push(n); });
    return list.sort((a, b) => a.localeCompare(b));
  }

  function option(value, text) {
    const o = document.createElement('option');
    o.value = value;
    o.textContent = text;
    return o;
  }

  function ownerOptions(sel, current) {
    const list = peopleList();
    const key = list.join('\n');
    if (current && current !== EVERYONE && !list.includes(current)) list.push(current);
    sel.replaceChildren(option('', 'Who?'));
    list.forEach((n) => sel.append(option(n, n)));
    sel.append(option(EVERYONE, EVERYONE));
    sel.value = current || '';
    sel.dataset.people = key;
  }

  function makeCard(c) {
    const el = tplCard.cloneNode(true);
    const ta = $('textarea', el);
    const sel = $('select.owner', el);
    el._c = { id: c.id, col: c.col, author: c.author, body: c.body || '', owner: c.owner || '', ver: c.ver, r: c.r || {} };
    el._ta = ta;
    el._seq = 0;
    el._typed = false;
    el._timer = 0;
    el._inflight = 0;
    ta.value = el._c.body;
    if (c.col === 'a') {
      sel.hidden = false;
      ownerOptions(sel, el._c.owner);
      sel.addEventListener('focus', () => enter(el));
      sel.addEventListener('change', () => {
        enter(el);
        el._c.owner = sel.value;
        if (el._c.id) api({ op: 'owner', id: el._c.id, text: sel.value }).catch(() => {});
      });
    }
    ta.addEventListener('focus', () => enter(el));
    ta.addEventListener('input', () => {
      grow(ta);
      el._typed = true;
      queueText(el);
    });
    ta.addEventListener('blur', () => {
      if (el._timer) sendText(el, false).catch(() => {});
    });
    ta.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        const col = el._c.col;
        ta.blur();
        leave(el);
        if (e.metaKey || e.ctrlKey) addCard(col);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        ta.blur();
        leave(el);
      }
    });
    $('.del', el).addEventListener('click', () => discard(el, true));
    $('.react-add', el).addEventListener('click', (e) => openPicker(el, e.currentTarget));
    $('.reacts', el).addEventListener('click', (e) => {
      const chip = e.target.closest('.chip');
      if (!chip) return;
      const who = el._c.r[chip.dataset.emoji] || [];
      react(el, chip.dataset.emoji, !who.includes(me));
    });
    return el;
  }

  function render(el) {
    const c = el._c;
    const ta = el._ta;
    const editing = document.activeElement === ta || el._timer || el._inflight > 0;
    if (!editing && ta.value !== c.body) ta.value = c.body;
    grow(ta);
    el.classList.toggle('mine', c.author === me);
    el.dataset.id = c.id || '';
    $('.who', el).textContent = c.author;
    const sel = $('select.owner', el);
    if (c.col === 'a' && document.activeElement !== sel) {
      if (sel.dataset.people !== peopleList().join('\n') || sel.value !== c.owner) ownerOptions(sel, c.owner);
    }
    renderLock(el);
    renderReacts(el);
  }

  function renderLock(el) {
    const c = el._c;
    clearTimeout(el._lockT);
    const other = c.lock && c.lock !== me;
    el.classList.toggle('locked', !!other);
    $('.lockmsg', el).textContent = other ? c.lock + ' is typing' : '';
    el._ta.readOnly = !!other;
    if (other) {
      el._lockT = setTimeout(() => {
        c.lock = '';
        renderLock(el);
      }, c.lockMs || 5000);
    }
  }

  function renderReacts(el) {
    const box = $('.reacts', el);
    box.replaceChildren();
    const r = el._c.r || {};
    EMOJI.forEach((e) => {
      const who = r[e] || [];
      if (!who.length) return;
      const mine = who.includes(me);
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'chip' + (mine ? ' mine' : '');
      b.dataset.emoji = e;
      b.setAttribute('aria-pressed', mine ? 'true' : 'false');
      b.title = who.join(', ');
      b.textContent = e + ' ' + who.length;
      box.append(b);
    });
    box.hidden = !box.childElementCount;
    $('.react-add', el).disabled = !el._c.id;
  }

  function setReact(c, emoji, who, on) {
    c.r = c.r || {};
    const list = c.r[emoji] || [];
    const has = list.includes(who);
    if (on && !has) c.r[emoji] = list.concat(who);
    if (!on && has) c.r[emoji] = list.filter((x) => x !== who);
    if (c.r[emoji] && !c.r[emoji].length) delete c.r[emoji];
  }

  function react(el, emoji, on) {
    if (!el._c.id) return;
    setReact(el._c, emoji, me, on);
    renderReacts(el);
    const id = el._c.id;
    el._chain = (el._chain || Promise.resolve())
      .then(() => api({ op: 'react', id, emoji, on }))
      .catch(() => {
        setReact(el._c, emoji, me, !on);
        renderReacts(el);
      });
  }

  function openPicker(el, anchor) {
    if (!el._c.id) return;
    if (!picker.hidden && picker._anchor === anchor) {
      closePicker(true);
      return;
    }
    if (!picker.childElementCount) {
      EMOJI.forEach((e) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.setAttribute('role', 'menuitemcheckbox');
        b.dataset.emoji = e;
        b.textContent = e;
        picker.append(b);
      });
    }
    const who = el._c.r || {};
    picker.querySelectorAll('button').forEach((b) => {
      const mine = (who[b.dataset.emoji] || []).includes(me);
      b.classList.toggle('mine', mine);
      b.setAttribute('aria-checked', mine ? 'true' : 'false');
    });
    picker._el = el;
    picker._anchor = anchor;
    picker.hidden = false;
    placePicker();
    anchor.setAttribute('aria-expanded', 'true');
    const first = $('button', picker);
    if (first) first.focus({ preventScroll: true });
  }

  function placePicker() {
    const anchor = picker._anchor;
    if (picker.hidden || !anchor) return;
    const r = anchor.getBoundingClientRect();
    if (r.bottom < 0 || r.top > window.innerHeight) {
      closePicker(false);
      return;
    }
    const pw = picker.offsetWidth;
    const ph = picker.offsetHeight;
    const left = Math.min(Math.max(8, r.right - pw), window.innerWidth - pw - 8);
    let top = r.top - ph - 8;
    if (top < 64) top = r.bottom + 8;
    picker.style.left = left + 'px';
    picker.style.top = top + 'px';
  }

  function closePicker(refocus) {
    if (picker.hidden) return;
    picker.hidden = true;
    const a = picker._anchor;
    picker._el = null;
    picker._anchor = null;
    if (a) {
      a.setAttribute('aria-expanded', 'false');
      if (refocus && document.contains(a)) a.focus({ preventScroll: true });
    }
  }

  picker.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b || !picker._el) return;
    const el = picker._el;
    const who = el._c.r[b.dataset.emoji] || [];
    react(el, b.dataset.emoji, !who.includes(me));
    closePicker(true);
  });

  picker.addEventListener('keydown', (e) => {
    const items = [...picker.querySelectorAll('button')];
    const i = items.indexOf(document.activeElement);
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault();
      items[(i + (e.key === 'ArrowRight' ? 1 : -1) + items.length) % items.length].focus();
    }
  });

  function queueText(el) {
    if (!el._c.id) return;
    clearTimeout(el._timer);
    el._timer = setTimeout(() => {
      el._timer = 0;
      sendText(el, false).catch(() => {});
    }, 250);
  }

  function sendText(el, final) {
    clearTimeout(el._timer);
    el._timer = 0;
    if (!el._c.id) return Promise.resolve();
    el._seq += 1;
    if (final) el._typed = false;
    el._inflight += 1;
    const done = () => { el._inflight -= 1; };
    const p = api({ op: 'text', id: el._c.id, text: el._ta.value, seq: el._seq, final });
    p.then(done, done);
    return p;
  }

  function enter(el) {
    if (openEl === el) return;
    if (openEl) leave(openEl);
    openEl = el;
  }

  function leave(el) {
    if (openEl === el) openEl = null;
    const blank = !el._ta.value.trim() && !el._c.owner && !hasReactions(el._c);
    if (blank && (el._c.author === me || !el._c.id)) {
      discard(el, false);
      return;
    }
    if (el._typed || el._timer) sendText(el, true).catch(() => {});
  }

  function discard(el, explicit) {
    if (openEl === el) openEl = null;
    if (picker._el === el) closePicker(false);
    el.remove();
    clearTimeout(el._timer);
    el._timer = 0;
    el._discarded = true;
    if (el._c.id) {
      cards.delete(el._c.id);
      api({ op: 'del', id: el._c.id }).catch(() => { if (explicit) resync(); });
    }
    diceState();
  }

  function addCard(col) {
    const box = boxFor(col);
    if (!box) return;
    const tmp = 't' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
    const el = makeCard({ id: 0, col, author: me, body: '', owner: '', ver: -1, r: {} });
    el._tmp = tmp;
    pending.set(tmp, el);
    box.append(el);
    render(el);
    diceState();
    el._ta.focus();
    api({ op: 'add', col, tmp })
      .then((res) => { if (res && res.c) upsert(res.c, res.tmp); })
      .catch(() => {
        if (pending.get(tmp) === el) {
          pending.delete(tmp);
          if (openEl === el) openEl = null;
          el.remove();
          diceState();
        }
      });
  }

  function bind(el, c, tmp) {
    pending.delete(tmp);
    boundTmps.add(tmp);
    el._tmp = null;
    el._c.id = c.id;
    el._c.ver = c.ver;
    el._c.author = c.author;
    el.dataset.id = c.id;
    if (el._discarded) {
      api({ op: 'del', id: c.id }).catch(() => {});
      return;
    }
    cards.set(c.id, el);
    if (el._c.owner) api({ op: 'owner', id: c.id, text: el._c.owner }).catch(() => {});
    el._typed = true;
    if (el._ta.value || openEl !== el) sendText(el, openEl !== el).catch(() => {});
    renderReacts(el);
  }

  function upsert(c, tmp) {
    if (tmp && boundTmps.has(tmp)) return;
    if (tmp && pending.has(tmp)) {
      bind(pending.get(tmp), c, tmp);
      return;
    }
    let el = cards.get(c.id);
    if (el) {
      if (c.ver < el._c.ver) return;
      const r = c.r || el._c.r;
      Object.assign(el._c, { col: c.col, author: c.author, body: c.body, owner: c.owner, ver: c.ver, lock: c.lock, lockMs: c.lockMs, r });
      render(el);
      return;
    }
    el = makeCard(c);
    el._c.lock = c.lock;
    el._c.lockMs = c.lockMs;
    cards.set(c.id, el);
    const box = boxFor(c.col);
    if (box) box.append(el);
    render(el);
  }

  function drop(id) {
    const el = cards.get(id);
    if (!el) return;
    if (openEl === el) openEl = null;
    if (picker._el === el) closePicker(false);
    el.remove();
    cards.delete(id);
  }

  function buildCols(next) {
    const same = cols.length === next.length && board.querySelectorAll('.col.board-col').length === next.length;
    if (same) {
      next.forEach((t, i) => {
        const inp = $('#col-' + i + ' .col-title');
        if (inp && document.activeElement !== inp) inp.value = t;
      });
      cols = next.slice();
      return;
    }
    board.querySelectorAll('.col.board-col').forEach((e) => e.remove());
    const actions = $('#col-a');
    next.forEach((t, i) => {
      const d = tplCol.cloneNode(true);
      d.classList.add('board-col');
      d.id = 'col-' + i;
      const inp = $('.col-title', d);
      inp.value = t;
      inp.addEventListener('change', () => {
        if (inp.value.trim()) api({ op: 'col', i, text: inp.value }).catch(() => { inp.value = cols[i]; });
        else inp.value = cols[i];
      });
      inp.addEventListener('keydown', (e) => { if (e.key === 'Enter') inp.blur(); });
      $('.add', d).dataset.add = String(i);
      board.insertBefore(d, actions);
    });
    cols = next.slice();
    for (const el of cards.values()) {
      if (isBoardCol(el._c.col)) {
        const box = boxFor(el._c.col);
        if (box) box.append(el);
      }
    }
  }

  function diceState() {
    let n = 0;
    for (const el of cards.values()) if (isBoardCol(el._c.col)) n += 1;
    board.querySelectorAll('.board-col .cards').forEach((box) => { n += [...box.children].filter((el) => !el._c.id).length; });
    $('#cols-dice').disabled = n > 0;
  }

  function meta(m) {
    const q = $('#warmup');
    if (document.activeElement !== q) {
      q.value = m.warmup;
      grow(q);
    }
    S.warmup = m.warmup;
    buildCols(m.cols);
  }

  function renderHere(names) {
    here = names.slice();
    $('#here').replaceChildren(...names.map((n) => {
      const s = document.createElement('span');
      s.textContent = n;
      return s;
    }));
    for (const el of cards.values()) if (el._c.col === 'a') render(el);
  }

  function renderCarry(list) {
    S.carry = list;
    $('#carry-block').hidden = !list.length;
    const box = $('#carry');
    box.replaceChildren();
    list.forEach((c) => {
      const li = document.createElement('li');
      li.id = 'cy-' + c.id;
      li.className = c.done ? 'done' : '';
      const label = document.createElement('label');
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = c.done;
      const body = document.createElement('span');
      body.className = 'carry-body';
      body.textContent = c.body;
      const info = document.createElement('span');
      info.className = 'carry-meta';
      info.textContent = (c.owner ? c.owner + ' · ' : '') + c.from;
      label.append(cb, body, info);
      li.append(label);
      cb.addEventListener('change', () => {
        const done = cb.checked;
        li.classList.toggle('done', done);
        c.done = done;
        api({ op: 'done', id: c.id, done }).catch(() => {
          cb.checked = !done;
          c.done = !done;
          li.classList.toggle('done', !done);
        });
      });
      box.append(li);
    });
  }

  function apply(s) {
    if (s.names) people = s.names.slice();
    renderHere(s.here || []);
    meta(s);
    const seen = new Set();
    s.cards.forEach((c) => {
      seen.add(c.id);
      upsert(Object.assign({}, c, { r: c.r || {} }));
    });
    for (const id of [...cards.keys()]) if (!seen.has(id)) drop(id);
    renderCarry(s.carry || []);
    diceState();
  }

  async function resync() {
    try {
      const r = await fetch('/retro/' + rid + '/state', { headers: { 'X-Greenroom': '1' } });
      if (r.status === 401) {
        location.href = '/login?next=' + encodeURIComponent(location.pathname);
        return;
      }
      if (r.ok) apply(await r.json());
    } catch (e) {}
  }

  document.addEventListener('pointerdown', (e) => {
    if (!picker.hidden && !picker.contains(e.target) && !(picker._anchor && picker._anchor.contains(e.target))) closePicker(false);
    if (openEl && !openEl.contains(e.target) && !picker.contains(e.target)) leave(openEl);
  }, true);

  document.addEventListener('focusin', (e) => {
    if (openEl && !openEl.contains(e.target) && !picker.contains(e.target)) leave(openEl);
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !picker.hidden) {
      e.preventDefault();
      closePicker(true);
    }
  });

  window.addEventListener('resize', placePicker);
  window.addEventListener('scroll', placePicker, { passive: true });

  document.addEventListener('click', (e) => {
    const add = e.target.closest('[data-add]');
    if (add) addCard(add.dataset.add);
  });

  const warm = $('#warmup');
  warm.addEventListener('input', () => grow(warm));
  warm.addEventListener('change', () => {
    if (warm.value.trim()) api({ op: 'warmuptext', text: warm.value }).catch(() => {});
    else { warm.value = S.warmup; grow(warm); }
  });
  warm.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); warm.blur(); }
    if (e.key === 'Escape') { warm.value = S.warmup; grow(warm); warm.blur(); }
  });
  $('#warmup-dice').addEventListener('click', () => api({ op: 'warmup' }).catch(() => {}));
  $('#cols-dice').addEventListener('click', () => api({ op: 'cols' }).catch(() => {}));

  $('#copy').addEventListener('click', async () => {
    const url = '/retro/' + rid + '/export.md';
    const load = () => fetch(url, { headers: { 'X-Greenroom': '1' } }).then((r) => {
      if (!r.ok) throw new Error('export failed');
      return r.text();
    });
    try {
      if (window.ClipboardItem && navigator.clipboard && navigator.clipboard.write) {
        const blob = load().then((t) => new Blob([t], { type: 'text/plain' }));
        await navigator.clipboard.write([new ClipboardItem({ 'text/plain': blob })]);
      } else {
        await navigator.clipboard.writeText(await load());
      }
      toast('Summary copied as Markdown.');
    } catch (e) {
      location.href = url + '?download=1';
    }
  });

  let opened = false;
  function connect() {
    const es = new EventSource('/retro/' + rid + '/events');
    es.onopen = () => {
      if (opened) resync();
      opened = true;
    };
    es.onmessage = (e) => {
      const m = JSON.parse(e.data);
      switch (m.t) {
        case 'card': upsert(m.c, m.tmp); diceState(); break;
        case 'del': drop(m.id); diceState(); break;
        case 'meta': meta(m); break;
        case 'here': renderHere(m.names); break;
        case 'react': {
          const el = cards.get(m.id);
          if (!el) break;
          setReact(el._c, m.emoji, m.who, m.on);
          renderReacts(el);
          break;
        }
        case 'carry': {
          const li = document.getElementById('cy-' + m.id);
          if (!li) break;
          li.classList.toggle('done', m.done);
          $('input', li).checked = m.done;
          const c = (S.carry || []).find((x) => x.id === m.id);
          if (c) c.done = m.done;
          break;
        }
      }
    };
  }

  apply(S);
  connect();
})();
