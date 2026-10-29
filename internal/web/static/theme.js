(function () {
  'use strict';
  var KEY = 'moored:theme';
  var root = document.documentElement;
  var media = window.matchMedia('(prefers-color-scheme: dark)');
  function stored() {
    try { return localStorage.getItem(KEY); } catch (e) { return null; }
  }
  function apply(t) {
    root.dataset.theme = t;
    root.style.colorScheme = t;
  }
  try {
    ['theme', 'rooms'].forEach(function (k) {
      var old = localStorage.getItem('greenroom:' + k);
      if (old === null) return;
      if (localStorage.getItem('moored:' + k) === null) localStorage.setItem('moored:' + k, old);
      localStorage.removeItem('greenroom:' + k);
    });
  } catch (e) {}
  var saved = stored();
  apply(saved === 'light' || saved === 'dark' ? saved : (media.matches ? 'dark' : 'light'));
  media.addEventListener('change', function (e) {
    var s = stored();
    if (s !== 'light' && s !== 'dark') apply(e.matches ? 'dark' : 'light');
  });
  document.addEventListener('DOMContentLoaded', function () {
    var b = document.getElementById('theme');
    if (!b) return;
    function label() {
      var dark = root.dataset.theme === 'dark';
      b.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');
      b.title = b.getAttribute('aria-label');
    }
    label();
    b.addEventListener('click', function () {
      var next = root.dataset.theme === 'dark' ? 'light' : 'dark';
      apply(next);
      try { localStorage.setItem(KEY, next); } catch (e) {}
      label();
    });
  });
})();
