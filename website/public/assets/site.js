// Dark is the default. A visitor's light or dark choice is kept in this
// browser only. Without JavaScript the page shows dark mode and the Homebrew tab.
(function () {
  var root = document.documentElement;
  var key = 'dpad-theme';
  root.classList.add('js');

  try {
    if (localStorage.getItem(key) === 'light') root.setAttribute('data-theme', 'light');
  } catch (e) {}

  function isLight() {
    return root.getAttribute('data-theme') === 'light';
  }

  // Screenshots have a light copy in shots-light/, made by `make website`.
  function swapShots() {
    var dir = isLight() ? '/assets/shots-light/' : '/assets/shots/';
    document.querySelectorAll('img[src*="/assets/shots"]').forEach(function (img) {
      var src = img.getAttribute('src').replace(/\/assets\/shots(-light)?\//, dir);
      if (src !== img.getAttribute('src')) img.setAttribute('src', src);
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    swapShots();

    document.querySelector('.mode').addEventListener('click', function () {
      if (isLight()) root.removeAttribute('data-theme');
      else root.setAttribute('data-theme', 'light');
      try { localStorage.setItem(key, isLight() ? 'light' : 'dark'); } catch (e) {}
      swapShots();
    });

    document.querySelectorAll('[data-tabs]').forEach(function (box) {
      box.querySelectorAll('[data-tab]').forEach(function (tab) {
        tab.addEventListener('click', function () {
          box.querySelectorAll('[data-tab]').forEach(function (t) {
            var on = t === tab;
            t.classList.toggle('on', on);
            t.setAttribute('aria-selected', on ? 'true' : 'false');
          });
          box.querySelectorAll('[data-panel]').forEach(function (panel) {
            panel.hidden = panel.getAttribute('data-panel') !== tab.getAttribute('data-tab');
          });
        });
      });
    });

    document.querySelectorAll('.copy').forEach(function (button) {
      button.addEventListener('click', function () {
        var code = button.parentNode.querySelector('code').cloneNode(true);
        var prompt = code.querySelector('.p');
        if (prompt) prompt.remove();
        navigator.clipboard.writeText(code.textContent.trim()).then(function () {
          button.textContent = 'copied';
          button.classList.add('done');
          setTimeout(function () {
            button.textContent = 'copy';
            button.classList.remove('done');
          }, 1500);
        });
      });
    });
  });
})();
