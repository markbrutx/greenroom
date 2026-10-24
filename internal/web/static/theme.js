(function () {
  var key = 'greenroom-theme';
  var root = document.documentElement;
  var media = window.matchMedia('(prefers-color-scheme: dark)');
  function stored() {
    try { return localStorage.getItem(key); } catch (e) { return null; }
  }
  function apply() {
    var s = stored();
    root.dataset.theme = s === 'dark' || s === 'light' ? s : (media.matches ? 'dark' : 'light');
  }
  apply();
  if (media.addEventListener) media.addEventListener('change', apply);
  document.addEventListener('click', function (e) {
    var b = e.target.closest && e.target.closest('[data-theme-toggle]');
    if (!b) return;
    var next = root.dataset.theme === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem(key, next); } catch (err) {}
    apply();
  });
})();
