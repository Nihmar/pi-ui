/* pi-ui mockup — the small, dependency-free interactivity of the HTML mockup.
 *
 * Nothing here is application logic: it toggles the theme and the device frame,
 * switches tabs, runs the dialog countdown and keeps the per-screen preferences
 * in localStorage. An embedded thumbnail never touches the stored preferences,
 * so the gallery always shows every screen in its own default frame.
 */
(function () {
  'use strict';

  var root = document.documentElement;
  var embedded = window.self !== window.top;
  var THEME_KEY = 'piui-mock-theme';
  var DEVICE_KEY = 'piui-mock-device';

  function stored(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (error) {
      return null;
    }
  }

  function store(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (error) {
      /* A file:// page with storage disabled still works, without memory. */
    }
  }

  function applyTheme(theme) {
    root.setAttribute('data-theme', theme);
    document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
      button.textContent = theme === 'dark' ? 'Light' : 'Dark';
      button.setAttribute('aria-pressed', theme === 'light' ? 'true' : 'false');
    });
  }

  function applyDevice(device) {
    root.setAttribute('data-device', device);
    document.querySelectorAll('[data-device-toggle]').forEach(function (button) {
      var active = button.getAttribute('data-device-toggle') === device;
      button.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
  }

  if (!embedded) {
    applyTheme(stored(THEME_KEY) || root.getAttribute('data-theme') || 'dark');
    applyDevice(stored(DEVICE_KEY) || root.getAttribute('data-device') || 'mobile');
  } else {
    applyTheme(root.getAttribute('data-theme') || 'dark');
    applyDevice(root.getAttribute('data-device') || 'mobile');
  }

  document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
    button.addEventListener('click', function () {
      var next = root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      applyTheme(next);
      if (!embedded) {
        store(THEME_KEY, next);
      }
    });
  });

  document.querySelectorAll('[data-device-toggle]').forEach(function (button) {
    button.addEventListener('click', function () {
      var next = button.getAttribute('data-device-toggle');
      applyDevice(next);
      if (!embedded) {
        store(DEVICE_KEY, next);
      }
    });
  });

  /* Tabs: a button with data-tab="x" shows [data-panel="x"] of the same group. */
  document.querySelectorAll('[data-tab]').forEach(function (button) {
    button.addEventListener('click', function () {
      var group = button.getAttribute('data-group');
      var name = button.getAttribute('data-tab');
      document.querySelectorAll('[data-tab][data-group="' + group + '"]').forEach(function (tab) {
        tab.classList.toggle('active', tab === button);
      });
      document.querySelectorAll('[data-panel][data-group="' + group + '"]').forEach(function (panel) {
        panel.hidden = panel.getAttribute('data-panel') !== name;
      });
    });
  });

  /* Buttons that reveal a hidden element: data-toggle="#selector". */
  document.querySelectorAll('[data-toggle]').forEach(function (button) {
    button.addEventListener('click', function () {
      document.querySelectorAll(button.getAttribute('data-toggle')).forEach(function (target) {
        target.hidden = !target.hidden;
      });
    });
  });

  /* The dialog countdown: the server's deadline, ticked down once a second. */
  document.querySelectorAll('[data-countdown]').forEach(function (element) {
    var remaining = parseInt(element.getAttribute('data-countdown'), 10);
    var dialog = element.closest('.dialog');
    var bar = dialog ? dialog.querySelector('[data-countdown-bar]') : null;
    var total = remaining;
    function tick() {
      element.textContent = remaining + 's';
      if (bar) {
        bar.style.width = Math.max(0, Math.round((remaining / total) * 100)) + '%';
      }
      if (remaining <= 0) {
        if (dialog) {
          dialog.classList.add('expired');
        }
        element.textContent = 'cancelled';
        clearInterval(timer);
        return;
      }
      remaining -= 1;
    }
    tick();
    var timer = setInterval(tick, 1000);
  });
})();
