(function () {
  var root = document.documentElement;
  root.classList.add('js');
  try {
    if (localStorage.getItem('dpad-theme') === 'light') root.setAttribute('data-theme', 'light');
  } catch (e) {}
})();
