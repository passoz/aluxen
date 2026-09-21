// Aluxen — JS do painel (vanilla, sem framework).
//
// Responsabilidades: tema claro/escuro, toasts, modais, confirmações,
// formulários de mutação por fetch e fila offline das mutações.
// O envio é sempre para as mesmas rotas do servidor (`POST /dashboard` com o
// campo `intent`, `POST /settings`), com o token CSRF do formulário.

(function () {
  'use strict';

  var THEME_KEY = 'aluxen.theme';
  var QUEUE_KEY = 'aluxen.offline.queue';
  var CACHE_KEY = 'aluxen.cache';

  function root() {
    return document.documentElement;
  }

  function currentTheme() {
    return root().getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
  }

  function applyTheme(theme) {
    root().setAttribute('data-theme', theme);
    try {
      localStorage.setItem(THEME_KEY, theme);
    } catch (error) {}
  }

  function toast(message, tone) {
    var host = document.getElementById('toast-host');
    if (!host) {
      host = document.createElement('div');
      host.id = 'toast-host';
      host.className = 'toast-host';
      document.body.appendChild(host);
    }
    var element = document.createElement('div');
    element.className = 'toast' + (tone ? ' toast-' + tone : '');
    element.textContent = message;
    host.appendChild(element);
    window.setTimeout(function () {
      element.remove();
    }, 4000);
  }

  function readQueue() {
    try {
      var raw = localStorage.getItem(QUEUE_KEY);
      if (!raw) return [];
      var parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      return [];
    }
  }

  function writeQueue(items) {
    try {
      localStorage.setItem(QUEUE_KEY, JSON.stringify(items));
    } catch (error) {}
    updateSyncBanner(items.length);
  }

  function updateSyncBanner(pending) {
    var banner = document.getElementById('sync-banner');
    if (!banner) return;
    if (!pending) {
      banner.hidden = true;
      banner.textContent = '';
      return;
    }
    banner.hidden = false;
    banner.textContent = pending === 1
      ? '1 alteração pendente de envio.'
      : pending + ' alterações pendentes de envio.';
  }

  function enqueue(entry) {
    var items = readQueue();
    items.push(entry);
    writeQueue(items);
  }

  function formPayload(form) {
    var payload = {};
    new FormData(form).forEach(function (value, key) {
      payload[key] = typeof value === 'string' ? value : '';
    });
    return payload;
  }

  function submitMutation(form) {
    var payload = formPayload(form);
    var action = form.getAttribute('action') || '/dashboard';
    return fetch(action, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        Accept: 'application/json'
      },
      body: new URLSearchParams(payload).toString(),
      credentials: 'same-origin'
    }).then(function (response) {
      return response.json().catch(function () {
        return { ok: false, error: 'Resposta inesperada do servidor.' };
      }).then(function (body) {
        return { status: response.status, body: body };
      });
    });
  }

  function flushQueue() {
    var items = readQueue();
    if (!items.length) return Promise.resolve();
    var remaining = [];
    var chain = Promise.resolve();
    items.forEach(function (item) {
      chain = chain.then(function () {
        return submitMutationPayload(item.action, item.payload).then(function (result) {
          if (!result.ok) remaining.push(item);
        });
      });
    });
    return chain.then(function () {
      writeQueue(remaining);
      if (!remaining.length) {
        toast('Alterações pendentes enviadas.', 'success');
        window.setTimeout(function () {
          window.location.reload();
        }, 800);
      }
    });
  }

  function submitMutationPayload(action, payload) {
    return fetch(action, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        Accept: 'application/json'
      },
      body: new URLSearchParams(payload).toString(),
      credentials: 'same-origin'
    }).then(function (response) {
      return response.json().then(function (body) {
        return { ok: response.ok && body && body.ok === true, status: response.status, body: body };
      }).catch(function () {
        return { ok: false, status: response.status, body: {} };
      });
    }).catch(function () {
      return { ok: false, status: 0, body: {} };
    });
  }

  function openModal(id) {
    var modal = document.getElementById(id);
    if (modal) modal.hidden = false;
  }

  function closeModal(modal) {
    if (modal) modal.hidden = true;
  }

  document.addEventListener('click', function (event) {
    var target = event.target;
    if (!(target instanceof Element)) return;

    var toggle = target.closest('[data-theme-toggle]');
    if (toggle) {
      applyTheme(currentTheme() === 'dark' ? 'light' : 'dark');
      return;
    }

    var opener = target.closest('[data-modal-open]');
    if (opener) {
      event.preventDefault();
      openModal(opener.getAttribute('data-modal-open'));
      return;
    }

    var closer = target.closest('[data-modal-close]');
    if (closer) {
      event.preventDefault();
      closeModal(closer.closest('.modal-backdrop'));
      return;
    }

    if (target.classList.contains('modal-backdrop')) {
      closeModal(target);
    }
  });

  document.addEventListener('submit', function (event) {
    var form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.hasAttribute('data-native')) return;

    event.preventDefault();

    if (form.hasAttribute('data-confirm') && !window.confirm(form.getAttribute('data-confirm'))) {
      return;
    }

    if (!navigator.onLine && form.hasAttribute('data-offline')) {
      enqueue({ action: form.getAttribute('action') || '/dashboard', payload: formPayload(form) });
      toast('Sem conexão: alteração guardada para envio automático.', 'warning');
      closeModal(form.closest('.modal-backdrop'));
      return;
    }

    submitMutation(form).then(function (result) {
      if (result.body && result.body.ok) {
        toast('Alteração salva.', 'success');
        closeModal(form.closest('.modal-backdrop'));
        window.setTimeout(function () {
          window.location.reload();
        }, 400);
        return;
      }
      var message = (result.body && result.body.error) || 'Não foi possível salvar.';
      toast(message, 'error');
    });
  });

  window.addEventListener('online', function () {
    flushQueue();
  });

  document.addEventListener('DOMContentLoaded', function () {
    updateSyncBanner(readQueue().length);
    if (navigator.onLine) flushQueue();
  });

  window.aluxen = {
    toast: toast,
    applyTheme: applyTheme,
    flushQueue: flushQueue,
    readQueue: readQueue,
    writeQueue: writeQueue
  };
})();
