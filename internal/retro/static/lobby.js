(function () {
  'use strict';

  const box = document.getElementById('lobby');
  const code = box.dataset.code;
  const base = '/' + code;
  const note = document.getElementById('host-note');
  let done = false;

  function go(url) {
    if (done) return;
    done = true;
    location.replace(url);
  }

  const es = new EventSource(base + '/events');
  es.onmessage = (e) => {
    const m = JSON.parse(e.data);
    switch (m.t) {
      case 'host':
        if (note) note.hidden = !!m.online;
        break;
      case 'admitted':
        es.close();
        go(base);
        break;
      case 'denied':
        es.close();
        go(base);
        break;
      case 'deleted':
        es.close();
        go('/?n=' + (m.why === 'expired' ? 'expired' : 'deleted'));
        break;
    }
  };
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) setTimeout(() => go(base), 1500);
  };
})();
