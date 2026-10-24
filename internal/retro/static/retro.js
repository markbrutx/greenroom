(function () {
  'use strict';

  const S = JSON.parse(document.getElementById('retro-state').textContent);
  const me = S.me;
  const rid = S.id;
  const EMOJI = S.emojis;
  const THUMB = EMOJI[0];
  const EVERYONE = 'Everyone';
  const SORT_KEY = 'greenroom-retro-sort';

  const $ = (sel, root) => (root || document).querySelector(sel);
  const tplCard = $('#tpl-card').content.firstElementChild;
  const tplCol = $('#tpl-col').content.firstElementChild;
  const picker = $('#picker');
  const board = $('#board');

  const cards = new Map();
  const pending = new Map();
  const boundTmps = new Set();
  let cols = [];
  let hidden = false;
  let sortOn = readSort();
  let openEl = null;
  let people = (S.names || []).slice();
  let here = [];
  let toastTimer = 0;
  const fieldSizing = typeof CSS !== 'undefined' && CSS.supports && CSS.supports('field-sizing', 'content');

  function readSort() {
    try { return localStorage.getItem(SORT_KEY) === '1'; } catch (e) { return false; }
  }

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

  function ownerOptions(sel, current) {
    const list = peopleList();
    const key = list.join('\n');
    if (current && current !== EVERYONE && !list.includes(current)) list.push(current);
    sel.replaceChildren();
    const blank = document.createElement('option');
    blank.value = '';
    blank.textContent = 'Owner';
    sel.append(blank);
    list.forEach((n) => {
      const o = document.createElement('option');
      o.value = n;
      o.textContent = n;
      sel.append(o);
    });
    const all = document.createElement('option');
    all.value = EVERYONE;
    all.textContent = EVERYONE;
    sel.append(all);
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
    el.classList.add('col-' + c.col);
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
    ta.placeholder = c.col === 'w' ? 'Your answer' : c.col === 'a' ? 'What will we do?' : 'Write a card';
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
    renderVeil(el);
    renderReacts(el);
  }

  function renderLock(el) {
    const c = el._c;
    const ta = el._ta;
    const lockmsg = $('.lockmsg', el);
    clearTimeout(el._lockT);
    const other = c.lock && c.lock !== me;
    el.classList.toggle('locked', !!other);
    lockmsg.textContent = other ? c.lock + ' is typing' : '';
    ta.readOnly = !!other || el.classList.contains('veiled');
    if (other) {
      el._lockT = setTimeout(() => {
        c.lock = '';
        renderLock(el);
      }, c.lockMs || 5000);
    }
  }

  function renderVeil(el) {
    const veiled = hidden && isBoardCol(el._c.col) && el._c.author !== me;
    el.classList.toggle('veiled', veiled);
    el.inert = veiled;
    el._ta.readOnly = veiled || el.classList.contains('locked');
    if (veiled && openEl === el) openEl = null;
  }

  function renderReacts(el) {
    const box = $('.reacts', el);
    const add = $('.react-add', box);
    box.querySelectorAll('.chip').forEach((x) => x.remove());
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
      b.setAttribute('aria-label', e + ' ' + who.length + ': ' + who.join(', '));
      const em = document.createElement('span');
      em.className = 'em';
      em.textContent = e;
      const n = document.createElement('span');
      n.className = 'n';
      n.textContent = String(who.length);
      b.append(em, n);
      box.insertBefore(b, add);
    });
    add.disabled = !el._c.id;
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
    sortBox(el.parentElement);
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
      b.setAttribute('aria-label', (mine ? 'Remove ' : 'React with ') + b.dataset.emoji);
    });
    picker._el = el;
    picker._anchor = anchor;
    picker.hidden = false;
    const r = anchor.getBoundingClientRect();
    const pw = picker.offsetWidth;
    const ph = picker.offsetHeight;
    let left = Math.min(Math.max(8, r.left), window.innerWidth - pw - 8);
    let top = r.top - ph - 8;
    if (top < 68) top = r.bottom + 8;
    picker.style.left = left + 'px';
    picker.style.top = top + 'px';
    picker.classList.add('on');
    anchor.setAttribute('aria-expanded', 'true');
    const first = $('button', picker);
    if (first) first.focus({ preventScroll: true });
  }

  function closePicker(refocus) {
    if (picker.hidden) return;
    picker.hidden = true;
    picker.classList.remove('on');
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
      const n = (i + (e.key === 'ArrowRight' ? 1 : -1) + items.length) % items.length;
      items[n].focus();
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
    el.classList.add('open');
  }

  function leave(el) {
    if (openEl === el) openEl = null;
    el.classList.remove('open');
    const blank = !el._ta.value.trim() && !el._c.owner && !hasReactions(el._c);
    if (blank && (el._c.author === me || !el._c.id)) {
      discard(el, false);
      return;
    }
    if (el._typed || el._timer) sendText(el, true).catch(() => {});
    sortBox(el.parentElement);
  }

  function discard(el, explicit) {
    if (openEl === el) openEl = null;
    if (picker._el === el) closePicker(false);
    const box = el.parentElement;
    el.remove();
    clearTimeout(el._timer);
    el._timer = 0;
    el._discarded = true;
    if (el._c.id) {
      cards.delete(el._c.id);
      api({ op: 'del', id: el._c.id }).catch(() => { if (explicit) resync(); });
    }
    updateCounts();
    if (box) sortBox(box);
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
    updateCounts();
    el._ta.focus();
    api({ op: 'add', col, tmp })
      .then((res) => { if (res && res.c) upsert(res.c, res.tmp); })
      .catch(() => {
        if (pending.get(tmp) === el) {
          pending.delete(tmp);
          if (openEl === el) openEl = null;
          el.remove();
          updateCounts();
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
      sortBox(el.parentElement);
      return;
    }
    el = makeCard(c);
    el._c.lock = c.lock;
    el._c.lockMs = c.lockMs;
    cards.set(c.id, el);
    const box = boxFor(c.col);
    if (box) box.append(el);
    render(el);
    updateCounts();
    if (box) sortBox(box);
  }

  function drop(id) {
    const el = cards.get(id);
    if (!el) return;
    if (openEl === el) openEl = null;
    if (picker._el === el) closePicker(false);
    const box = el.parentElement;
    el.remove();
    cards.delete(id);
    updateCounts();
    if (box) sortBox(box);
  }

  function score(el) {
    const r = el._c.r || {};
    let total = 0;
    Object.keys(r).forEach((e) => { total += r[e].length; });
    return [(r[THUMB] || []).length, total];
  }

  function sortBox(box) {
    if (!box || !box.classList || !box.classList.contains('cards')) return;
    const list = [...box.children];
    if (!sortOn) {
      list.forEach((el) => { el.style.order = ''; });
      return;
    }
    if (openEl && box.contains(openEl)) return;
    list
      .map((el, i) => ({ el, i, s: score(el) }))
      .sort((a, b) => b.s[0] - a.s[0] || b.s[1] - a.s[1] || a.i - b.i)
      .forEach((x, rank) => { x.el.style.order = String(rank); });
  }

  function sortAll() {
    document.querySelectorAll('#board .cards').forEach(sortBox);
  }

  function updateCounts() {
    let boardCards = 0;
    document.querySelectorAll('#board .col').forEach((col) => {
      const n = $('.cards', col).childElementCount;
      if (col.id !== 'col-a') boardCards += n;
      const label = $('.col-count', col);
      if (label) label.textContent = n ? String(n) : '';
    });
    $('#set-pick').hidden = boardCards > 0;
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
      d.dataset.col = String(i);
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
    board.dataset.cols = String(next.length);
    cols = next.slice();
    for (const el of cards.values()) {
      if (isBoardCol(el._c.col)) {
        const box = boxFor(el._c.col);
        if (box) box.append(el);
      }
    }
    sortAll();
  }

  function renderSetPicker() {
    const sel = $('#set-select');
    if (sel.childElementCount) return;
    const first = document.createElement('option');
    first.value = '';
    first.textContent = 'Change column set';
    sel.append(first);
    (S.sets || []).forEach((n) => {
      const o = document.createElement('option');
      o.value = n;
      o.textContent = n;
      sel.append(o);
    });
  }

  function setHidden(on) {
    hidden = on;
    const b = $('#hide-toggle');
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
    b.classList.toggle('btn-on', on);
    $('.label', b).textContent = on ? 'Reveal cards' : 'Private writing';
    b.title = on ? 'Show everyone\'s cards to everyone' : 'Blur other people\'s cards while everyone writes';
    $('#private-note').hidden = !on;
    for (const el of cards.values()) renderVeil(el);
  }

  function setSort(on) {
    sortOn = on;
    try { localStorage.setItem(SORT_KEY, on ? '1' : '0'); } catch (e) {}
    const b = $('#sort-toggle');
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
    b.classList.toggle('btn-on', on);
    sortAll();
  }

  function meta(m) {
    const q = $('#warmup');
    if (document.activeElement !== q) {
      q.value = m.warmup;
      grow(q);
    }
    S.warmup = m.warmup;
    const t = $('#title');
    if (document.activeElement !== t) t.value = m.title || '';
    S.title = m.title || '';
    const crumb = document.querySelector('.crumbs [aria-current]');
    if (crumb && S.title) crumb.textContent = S.title;
    document.title = (S.title || 'Retro ' + S.date) + ' · ' + document.title.split(' · ').slice(-1)[0];
    buildCols(m.cols);
    if (hidden !== !!m.hidden) setHidden(!!m.hidden);
    updateCounts();
  }

  function renderHere(names) {
    here = names.slice();
    const h = $('#here');
    h.replaceChildren();
    const max = 5;
    names.slice(0, max).forEach((n) => {
      const a = document.createElement('span');
      a.className = 'avatar' + (n === me ? ' self' : '');
      a.title = n;
      a.setAttribute('role', 'img');
      a.setAttribute('aria-label', n);
      a.textContent = initials(n);
      h.append(a);
    });
    if (names.length > max) {
      const more = document.createElement('span');
      more.className = 'avatar more';
      more.title = names.slice(max).join(', ');
      more.textContent = '+' + (names.length - max);
      h.append(more);
    }
    for (const el of cards.values()) if (el._c.col === 'a') render(el);
  }

  function initials(s) {
    let x = s.trim();
    const at = x.indexOf('@');
    if (at > 0) x = x.slice(0, at);
    const parts = x.split(/[\s._-]+/).filter(Boolean);
    const out = parts.slice(0, 2).map((p) => Array.from(p)[0]).join('');
    return (out || '?').toUpperCase();
  }

  function renderCarry(list) {
    S.carry = list;
    $('#carry-block').hidden = !list.length;
    const box = $('#carry');
    box.replaceChildren();
    list.forEach((c) => {
      const li = document.createElement('li');
      li.id = 'cy-' + c.id;
      li.className = 'carry-item' + (c.done ? ' done' : '');
      const label = document.createElement('label');
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = c.done;
      const body = document.createElement('span');
      body.className = 'carry-body';
      body.textContent = c.body;
      const metaEl = document.createElement('span');
      metaEl.className = 'carry-meta';
      metaEl.textContent = (c.owner ? c.owner + ' · ' : '') + c.from;
      label.append(cb, body, metaEl);
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

  const timer = { on: false, end: 0, tick: 0 };
  function setTimer(on, ms) {
    timer.on = on;
    timer.end = performance.now() + ms;
    clearInterval(timer.tick);
    const pill = $('#timer-pill');
    pill.hidden = !on;
    pill.classList.remove('up');
    if (!on) return;
    const draw = () => {
      const left = Math.max(0, timer.end - performance.now());
      const label = $('#timer-left');
      if (left <= 0) {
        label.textContent = 'Time\u2019s up';
        pill.classList.add('up');
        pill.setAttribute('aria-label', 'Time is up');
        clearInterval(timer.tick);
        return;
      }
      const s = Math.ceil(left / 1000);
      label.textContent = Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
      pill.setAttribute('aria-label', 'Time left ' + label.textContent);
    };
    draw();
    timer.tick = setInterval(draw, 250);
  }

  function apply(s) {
    if (s.names) people = s.names.slice();
    renderHere(s.here || []);
    meta(s);
    setTimer(s.timerOn, s.timerMs);
    const seen = new Set();
    s.cards.forEach((c) => {
      seen.add(c.id);
      upsert(Object.assign({}, c, { r: c.r || {} }));
    });
    for (const id of [...cards.keys()]) if (!seen.has(id)) drop(id);
    renderCarry(s.carry || []);
    updateCounts();
    sortAll();
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

  window.addEventListener('resize', () => closePicker(false));
  window.addEventListener('scroll', () => closePicker(false), { passive: true });

  document.addEventListener('click', (e) => {
    const add = e.target.closest('[data-add]');
    if (add) addCard(add.dataset.add);
    const t = e.target.closest('[data-timer]');
    if (t) {
      const min = Number(t.dataset.timer);
      setTimer(min > 0, min * 60000);
      api({ op: 'timer', min }).catch(() => {});
    }
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

  const title = $('#title');
  title.addEventListener('change', () => api({ op: 'title', text: title.value }).catch(() => {}));
  title.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') title.blur();
    if (e.key === 'Escape') { title.value = S.title; title.blur(); }
  });

  $('#cols-dice').addEventListener('click', () => api({ op: 'cols' }).catch(() => {}));
  $('#set-select').addEventListener('change', (e) => {
    const v = e.target.value;
    e.target.value = '';
    if (v) api({ op: 'cols', set: v }).catch(() => {});
  });

  $('#hide-toggle').addEventListener('click', () => {
    const next = !hidden;
    setHidden(next);
    api({ op: 'hidden', on: next }).catch(() => setHidden(!next));
  });

  $('#sort-toggle').addEventListener('click', () => setSort(!sortOn));

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
        case 'card': upsert(m.c, m.tmp); break;
        case 'del': drop(m.id); break;
        case 'meta': meta(m); break;
        case 'here': renderHere(m.names); break;
        case 'timer':
          setTimer(m.on, m.ms);
          if (m.on && m.by && m.by !== me) toast(m.by + ' started a ' + Math.round(m.ms / 60000) + ' min timer.');
          break;
        case 'react': {
          const el = cards.get(m.id);
          if (!el) break;
          setReact(el._c, m.emoji, m.who, m.on);
          renderReacts(el);
          sortBox(el.parentElement);
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

  renderSetPicker();
  setSort(sortOn);
  apply(S);
  connect();
})();
