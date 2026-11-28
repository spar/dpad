// Dark is the default. A visitor's light or dark choice is kept in this
// browser only. Without JavaScript the page shows dark mode and the Homebrew tab.
(function () {
  var root = document.documentElement;
  // website/pages/_boot.js sets the js class and a saved light choice before
  // the page paints; this file loads deferred.
  var key = 'dpad-theme';

  function isLight() {
    return root.getAttribute('data-theme') === 'light';
  }

  // Screenshots have a light copy in shots-light/, written by `make website`.
  function swapShots() {
    var dir = isLight() ? '/assets/shots-light/' : '/assets/shots/';
    document.querySelectorAll('img[src*="/assets/shots"], source[srcset*="/assets/shots"]').forEach(function (el) {
      var attr = el.tagName === 'IMG' ? 'src' : 'srcset';
      var src = el.getAttribute(attr).replace(/\/assets\/shots(-light)?\//, dir);
      if (src !== el.getAttribute(attr)) el.setAttribute(attr, src);
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

    var dialog = document.querySelector('.zoom');
    if (dialog) {
      var links = Array.prototype.slice.call(document.querySelectorAll('[data-zoom]'));
      var big = dialog.querySelector('img');
      var name = dialog.querySelector('figcaption span');
      var current = 0;
      var show = function (i) {
        current = (i + links.length) % links.length;
        var label = links[current].parentNode.querySelector('figcaption').textContent;
        big.setAttribute('src', links[current].getAttribute('href'));
        big.setAttribute('alt', 'dpad with the ' + label + ' theme');
        name.textContent = label + '  ' + (current + 1) + ' / ' + links.length;
      };
      links.forEach(function (link, i) {
        link.addEventListener('click', function (e) {
          e.preventDefault();
          show(i);
          dialog.showModal();
        });
      });
      dialog.querySelector('.prev').addEventListener('click', function () { show(current - 1); });
      dialog.querySelector('.next').addEventListener('click', function () { show(current + 1); });
      dialog.querySelector('.close').addEventListener('click', function () { dialog.close(); });
      dialog.addEventListener('keydown', function (e) {
        if (e.key === 'ArrowLeft') show(current - 1);
        if (e.key === 'ArrowRight') show(current + 1);
      });
      // A click on the backdrop lands on the dialog itself.
      dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
      dialog.addEventListener('close', function () { links[current].focus(); });
    }

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
