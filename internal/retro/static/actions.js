(function () {
  'use strict';
  const toast = (text) => {
    const t = document.getElementById('toast');
    t.textContent = text;
    t.classList.add('on');
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => t.classList.remove('on'), 3200);
  };
  document.addEventListener('change', async (e) => {
    const cb = e.target.closest('input[data-action]');
    if (!cb) return;
    const li = cb.closest('.oa');
    const done = cb.checked;
    li.classList.toggle('done', done);
    try {
      const r = await fetch('/retro/actions/done', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Greenroom': '1' },
        body: JSON.stringify({ id: Number(cb.dataset.action), done }),
      });
      if (r.status === 401) {
        location.href = '/login?next=' + encodeURIComponent(location.pathname);
        return;
      }
      if (!r.ok) throw new Error((await r.text()).trim());
      if (done) toast('Marked as done.');
    } catch (err) {
      cb.checked = !done;
      li.classList.toggle('done', !done);
      toast(err.message || 'No connection to the server. Nothing changed.');
    }
  });
})();
