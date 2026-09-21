// Aluxen — JS do painel (vanilla, sem framework, sem dependência externa).
//
// Camada de interação do painel server-side (html/template):
//   1. tema claro/escuro persistido e toasts (`window.aluxen.toast`);
//   2. formulários de mutação por fetch (urlencoded + Accept: application/json),
//      com `data-confirm` e devolução ao submit nativo quando a resposta não é JSON;
//   3. fila offline (`aluxen.offline.queue`) e cache de dados por aba (`aluxen.cache`);
//   4. máscaras (`data-mask`), dinheiro normalizado antes do envio (`data-money-input`);
//   5. datepicker próprio (`data-datepicker`, `data-datetime`);
//   6. tabelas (`data-table`): ordenação, busca local, filtros, paginação e lote;
//   7. modais (`data-modal-open`/`data-modal-close`/`data-modal-fill`);
//   8. envio por WhatsApp (`data-whatsapp-send`) e PWA (service worker + manifest).
//
// Os eventos são delegados em `document` (click, submit, input, change, keydown),
// então conteúdo trocado depois — cache offline, modais e linhas de paginação
// criados em runtime — continua funcionando. O único contrato público é
// `window.aluxen`.

(function () {
  'use strict';

  /* ── Constantes ───────────────────────────────────────────────────────── */

  var THEME_KEY = 'aluxen.theme';
  var QUEUE_KEY = 'aluxen.offline.queue';
  var CACHE_KEY = 'aluxen.cache';
  var PAGESIZE_KEY = 'aluxen.pagesize.';
  var PAGESIZE_OPTIONS = [10, 25, 50, 100];
  var CACHE_LIMIT = 8;

  var OFFLINE_NOTICE = 'Modo offline ativo. As alterações serão sincronizadas quando a conexão voltar.';
  var SYNC_FAILURE = 'Falha ao sincronizar dados pendentes.';
  var CONNECTION_ERROR = 'Erro de conexão.';
  var CONNECTION_SAVE_ERROR = 'Não foi possível salvar.';

  var MONTH_NAMES = [
    'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
    'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'
  ];
  var WEEKDAY_NAMES = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb'];

  var WHATSAPP_STEPS = [
    { stage: 'preparing', label: 'Preparando o envio' },
    { stage: 'connecting', label: 'Conectando ao WhatsApp' },
    { stage: 'opening_chat', label: 'Abrindo a conversa' },
    { stage: 'sending', label: 'Enviando a mensagem' },
    { stage: 'confirming', label: 'Confirmando a entrega' }
  ];
  var WHATSAPP_SEND_TIMEOUT = 4 * 60 * 1000;
  var WHATSAPP_POLL_INTERVAL = 1200;

  var booted = false;
  var flushInFlight = null;

  /* ── Utilidades ───────────────────────────────────────────────────────── */

  function $(selector, scope) {
    return (scope || document).querySelector(selector);
  }

  function $$(selector, scope) {
    return Array.prototype.slice.call((scope || document).querySelectorAll(selector));
  }

  function each(list, fn) {
    Array.prototype.forEach.call(list, fn);
  }

  function element(target) {
    return target instanceof Element ? target : null;
  }

  function storageGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (error) {
      return null;
    }
  }

  function storageSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (error) {
      /* quota/privacidade: o painel segue funcionando sem persistir */
    }
  }

  function readJSON(key, fallback) {
    var raw = storageGet(key);
    if (!raw) return fallback;
    try {
      var parsed = JSON.parse(raw);
      return parsed === null || parsed === undefined ? fallback : parsed;
    } catch (error) {
      return fallback;
    }
  }

  function writeJSON(key, value) {
    storageSet(key, JSON.stringify(value));
  }

  /** Texto normalizado para busca/ordenação: sem acento, minúsculo, sem espaços nas pontas. */
  function normalizeText(value) {
    var text = value === null || value === undefined ? '' : String(value);
    if (text.normalize) {
      text = text.normalize('NFD').replace(/[\u0300-\u036f]/g, '');
    }
    return text.toLowerCase().replace(/\s+/g, ' ').trim();
  }

  function debounce(fn, wait) {
    var timer = 0;
    return function () {
      var args = arguments;
      var self = this;
      window.clearTimeout(timer);
      timer = window.setTimeout(function () {
        fn.apply(self, args);
      }, wait);
    };
  }

  function delay(ms) {
    return new Promise(function (resolve) {
      window.setTimeout(resolve, ms);
    });
  }

  function csrfToken() {
    var field = $('input[name="csrf_token"]');
    if (field && field.value) return field.value;
    var meta = $('meta[name="csrf-token"]');
    if (meta && meta.getAttribute('content')) return meta.getAttribute('content');
    var match = document.cookie.match(/(?:^|;\s*)(?:aluxen_)?csrf[^=]*=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : '';
  }

  /* ── Tema ─────────────────────────────────────────────────────────────── */

  function root() {
    return document.documentElement;
  }

  function currentTheme() {
    return root().getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
  }

  function applyTheme(theme) {
    var next = theme === 'dark' ? 'dark' : 'light';
    root().setAttribute('data-theme', next);
    storageSet(THEME_KEY, next);
  }

  function toggleTheme() {
    applyTheme(currentTheme() === 'dark' ? 'light' : 'dark');
  }

  /* ── Toasts ───────────────────────────────────────────────────────────── */

  function toast(message, tone) {
    if (!message) return;
    var host = document.getElementById('toast-host');
    if (!host) {
      host = document.createElement('div');
      host.id = 'toast-host';
      host.className = 'toast-host';
      document.body.appendChild(host);
    }
    var node = document.createElement('div');
    node.className = 'toast' + (tone ? ' toast-' + tone : '');
    node.setAttribute('role', tone === 'error' ? 'alert' : 'status');
    node.textContent = message;
    host.appendChild(node);
    window.setTimeout(function () {
      node.remove();
    }, 4200);
  }

  /* ── Fila offline ─────────────────────────────────────────────────────── */

  function readQueue() {
    var parsed = readJSON(QUEUE_KEY, []);
    return Array.isArray(parsed) ? parsed : [];
  }

  function writeQueue(items) {
    writeJSON(QUEUE_KEY, items);
    updateSyncBanner(items.length);
  }

  function enqueue(entry) {
    var items = readQueue();
    items.push(entry);
    writeQueue(items);
  }

  function updateSyncBanner(pending) {
    var banner = document.getElementById('sync-banner');
    if (!banner) return;
    var online = window.navigator.onLine !== false;
    if (online && !pending) {
      banner.hidden = true;
      banner.className = 'sync-banner';
      banner.textContent = '';
      return;
    }
    banner.hidden = false;
    banner.className = 'sync-banner ' + (online ? 'pending' : 'offline');
    banner.textContent = online
      ? pending + ' alteração(ões) pendente(s) de sincronização.'
      : OFFLINE_NOTICE;
  }

  /** Mensagem de erro de sincronização no mesmo tom do app atual. */
  function syncErrorMessage(body) {
    var raw = (body && (body.error || body.message)) || '';
    var lower = raw.toLowerCase();
    if (lower.indexOf('credenciais inv') >= 0 || lower.indexOf('não autorizado') >= 0) {
      return 'Sessão expirada. Entre novamente para sincronizar os dados pendentes.';
    }
    if (raw) return raw;
    return SYNC_FAILURE;
  }

  function postPayload(action, payload) {
    return fetch(action, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        Accept: 'application/json'
      },
      body: new URLSearchParams(payload).toString(),
      credentials: 'same-origin'
    }).then(function (response) {
      var contentType = response.headers.get('content-type') || '';
      if (contentType.indexOf('json') === -1) {
        return { ok: response.ok, status: response.status, body: {}, native: true };
      }
      return response.json().catch(function () {
        return {};
      }).then(function (body) {
        return { ok: response.ok && body && body.ok === true, status: response.status, body: body };
      });
    }).catch(function () {
      return { ok: false, status: 0, body: {}, offline: true };
    });
  }

  function flushQueue() {
    if (flushInFlight) return flushInFlight;
    if (window.navigator.onLine === false) return Promise.resolve();

    var items = readQueue();
    if (!items.length) {
      updateSyncBanner(0);
      return Promise.resolve();
    }

    var remaining = items.slice();
    var chain = Promise.resolve();
    items.forEach(function (item) {
      chain = chain.then(function () {
        if (remaining.indexOf(item) === -1) return null;
        return postPayload(item.action || '/dashboard', item.payload || {}).then(function (result) {
          if (!result.ok) {
            toast(syncErrorMessage(result.body), 'error');
            return Promise.reject(new Error('sync'));
          }
          remaining = remaining.filter(function (candidate) {
            return candidate !== item;
          });
          writeQueue(remaining);
          return null;
        });
      });
    });

    flushInFlight = chain.then(function () {
      if (!remaining.length) {
        toast('Sincronização concluída com o servidor.', 'success');
        window.setTimeout(function () {
          window.location.reload();
        }, 900);
      }
    }).catch(function () {
      /* erro já reportado; a fila permanece para a próxima tentativa */
    }).then(function () {
      flushInFlight = null;
    });

    return flushInFlight;
  }

  /* ── Cache de dados por aba ───────────────────────────────────────────── */

  function currentTab() {
    var params = new URLSearchParams(window.location.search);
    return params.get('tab') || 'dashboard';
  }

  function pageHost() {
    return $('main.page') || $('main');
  }

  function readCache() {
    var cache = readJSON(CACHE_KEY, {});
    return cache && typeof cache === 'object' ? cache : {};
  }

  function saveTabCache() {
    var host = pageHost();
    if (!host) return;
    var cache = readCache();
    cache[currentTab()] = {
      html: host.innerHTML,
      url: window.location.pathname + window.location.search,
      savedAt: Date.now()
    };
    var keys = Object.keys(cache);
    if (keys.length > CACHE_LIMIT) {
      keys.sort(function (a, b) {
        return (cache[a].savedAt || 0) - (cache[b].savedAt || 0);
      });
      keys.slice(0, keys.length - CACHE_LIMIT).forEach(function (key) {
        delete cache[key];
      });
    }
    writeJSON(CACHE_KEY, cache);
  }

  function restoreTabCache(tab) {
    var host = pageHost();
    var cached = readCache()[tab];
    if (!host || !cached || !cached.html) return false;
    host.innerHTML = cached.html;
    refresh();
    updateSyncBanner(readQueue().length);
    toast('Modo offline: exibindo os dados salvos desta aba.', 'warning');
    return true;
  }

  /* ── Máscaras e dinheiro ──────────────────────────────────────────────── */

  function maskCpf(value) {
    var digits = String(value || '').replace(/\D/g, '').slice(0, 11);
    if (!digits) return '';
    if (digits.length <= 3) return digits;
    if (digits.length <= 6) return digits.slice(0, 3) + '.' + digits.slice(3);
    if (digits.length <= 9) return digits.slice(0, 3) + '.' + digits.slice(3, 6) + '.' + digits.slice(6);
    return digits.slice(0, 3) + '.' + digits.slice(3, 6) + '.' + digits.slice(6, 9) + '-' + digits.slice(9);
  }

  function maskCep(value) {
    var digits = String(value || '').replace(/\D/g, '').slice(0, 8);
    if (!digits) return '';
    if (digits.length <= 5) return digits;
    return digits.slice(0, 5) + '-' + digits.slice(5);
  }

  function maskPhone(value) {
    var digits = String(value || '').replace(/\D/g, '').slice(0, 11);
    if (!digits) return '';
    if (digits.length <= 2) return '(' + digits;
    if (digits.length <= 6) return '(' + digits.slice(0, 2) + ') ' + digits.slice(2);
    if (digits.length <= 10) return '(' + digits.slice(0, 2) + ') ' + digits.slice(2, 6) + '-' + digits.slice(6);
    return '(' + digits.slice(0, 2) + ') ' + digits.slice(2, 7) + '-' + digits.slice(7);
  }

  function groupThousands(digits) {
    return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  }

  /** Digitação em centavos → `R$ 0,00` (mesma máscara do app atual). */
  function maskMoney(value) {
    var digits = String(value || '').replace(/\D/g, '');
    if (!digits) return '';
    digits = digits.replace(/^0+(?=\d)/, '');
    if (digits.length === 1) digits = '0' + digits;
    var cents = digits.slice(-2);
    var units = digits.slice(0, -2);
    return 'R$ ' + groupThousands(units) + ',' + cents;
  }

  /**
   * Texto de campo de dinheiro → decimal (`2500.00`), formato que o servidor
   * aceita. Entende `R$ 2.500,00` (máscara), `2500.00`/`2500`/`2500.5`
   * (decimal) e `2.500` (milhar). Texto não numérico devolve ''.
   */
  function normalizeMoneyText(value) {
    var text = String(value === null || value === undefined ? '' : value).trim();
    if (!text) return '';
    if (/^R\$/.test(text)) {
      var digits = text.replace(/\D/g, '');
      return digits ? (Number(digits) / 100).toFixed(2) : '';
    }
    var normalized;
    if (text.indexOf(',') >= 0) {
      normalized = text.replace(/\./g, '').replace(',', '.');
    } else if (/^\d{1,3}(\.\d{3})+$/.test(text)) {
      normalized = text.replace(/\./g, '');
    } else {
      normalized = text;
    }
    var number = Number(normalized);
    if (!isFinite(number)) return '';
    return number.toFixed(2);
  }

  /** Valor enviado ao servidor: sempre ponto decimal com 2 casas. */
  function moneyValue(value) {
    return normalizeMoneyText(value);
  }

  /** Valor do servidor → texto mascarado (`R$ 2.500,00`). */
  function moneyFromDecimal(value) {
    var text = String(value === null || value === undefined ? '' : value).trim();
    if (!text) return '';
    var decimal = normalizeMoneyText(text);
    return decimal ? maskMoney(decimal) : text;
  }

  function maskFor(kind, value) {
    switch (kind) {
      case 'cpf': return maskCpf(value);
      case 'cep': return maskCep(value);
      case 'phone': return maskPhone(value);
      case 'money': return maskMoney(value);
      default: return value;
    }
  }

  function countDigits(value) {
    return String(value || '').replace(/\D/g, '').length;
  }

  function maskField(field, keepCaret) {
    var kind = field.getAttribute('data-mask');
    if (!kind) return;
    var before = field.value;
    var masked = maskFor(kind, before);
    if (masked === before) return;
    var caret = null;
    if (keepCaret && typeof field.selectionStart === 'number') {
      var digitsBefore = countDigits(before.slice(0, field.selectionStart));
      caret = 0;
      var seen = 0;
      while (caret < masked.length && seen < digitsBefore) {
        if (/\d/.test(masked.charAt(caret))) seen += 1;
        caret += 1;
      }
      if (seen < digitsBefore) caret = masked.length;
    }
    field.value = masked;
    if (caret !== null && field.setSelectionRange) {
      try {
        field.setSelectionRange(caret, caret);
      } catch (error) {
        /* input type=number/hidden não suporta seleção: ignora */
      }
    }
  }

  function initMasks() {
    $$('input[data-mask]').forEach(function (field) {
      var kind = field.getAttribute('data-mask');
      if (kind === 'money') {
        field.setAttribute('inputmode', 'numeric');
        field.value = moneyFromDecimal(field.value);
        return;
      }
      if (!field.value) return;
      field.value = maskFor(kind, field.value);
    });
  }

  /* ── Datepicker próprio ───────────────────────────────────────────────── */

  var activePicker = null;

  function parseDateValue(value, withTime) {
    var text = String(value || '').trim();
    var match = text.match(/^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}))?/);
    if (match) {
      return new Date(
        Number(match[1]),
        Number(match[2]) - 1,
        Number(match[3]),
        withTime ? Number(match[4] || 0) : 0,
        withTime ? Number(match[5] || 0) : 0
      );
    }
    var br = text.match(/^(\d{2})\/(\d{2})\/(\d{4})(?:[ T](\d{2}):(\d{2}))?/);
    if (br) {
      return new Date(
        Number(br[3]),
        Number(br[2]) - 1,
        Number(br[1]),
        withTime ? Number(br[4] || 0) : 0,
        withTime ? Number(br[5] || 0) : 0
      );
    }
    return null;
  }

  function pad2(value) {
    return (value < 10 ? '0' : '') + value;
  }

  function formatIsoDate(date, withTime) {
    var text = date.getFullYear() + '-' + pad2(date.getMonth() + 1) + '-' + pad2(date.getDate());
    if (withTime) text += 'T' + pad2(date.getHours()) + ':' + pad2(date.getMinutes());
    return text;
  }

  function formatDisplayDate(date, withTime) {
    var text = pad2(date.getDate()) + '/' + pad2(date.getMonth() + 1) + '/' + date.getFullYear();
    if (withTime) text += ' ' + pad2(date.getHours()) + ':' + pad2(date.getMinutes());
    return text;
  }

  function positionPopover(popover, trigger) {
    var rect = trigger.getBoundingClientRect();
    var width = popover.offsetWidth || 300;
    var height = popover.offsetHeight || 320;
    var margin = 8;
    var viewportWidth = window.innerWidth;
    var viewportHeight = window.innerHeight;
    var left = Math.min(Math.max(margin, rect.left), Math.max(margin, viewportWidth - width - margin));
    var top;
    if (rect.bottom + height + margin <= viewportHeight) {
      top = rect.bottom + 4;
    } else if (rect.top - height - margin >= 0) {
      top = rect.top - height - 4;
    } else {
      top = Math.min(Math.max(margin, rect.top), Math.max(margin, viewportHeight - height - margin));
      left = Math.max(margin, rect.right - width);
    }
    popover.style.left = Math.round(left) + 'px';
    popover.style.top = Math.round(top) + 'px';
  }

  function initDatepickers() {
    $$('input[data-datepicker], input[data-datetime]').forEach(function (field) {
      if (field.getAttribute('data-datepicker-ready') === 'true') return;
      field.setAttribute('data-datepicker-ready', 'true');
      field.setAttribute('type', 'hidden');
      field.setAttribute('autocomplete', 'off');

      var wrapper = field.parentNode;
      if (!wrapper || !wrapper.classList.contains('custom-date-picker')) {
        wrapper = document.createElement('div');
        wrapper.className = 'custom-date-picker';
        field.parentNode.insertBefore(wrapper, field);
        wrapper.appendChild(field);
      }

      var display = document.createElement('button');
      display.type = 'button';
      display.className = 'date-picker-display';
      display.setAttribute('aria-haspopup', 'dialog');
      var label = document.querySelector('label[for="' + field.id + '"]');
      if (label) {
        display.setAttribute('aria-label', label.textContent.trim());
      } else if (field.getAttribute('placeholder')) {
        display.setAttribute('aria-label', field.getAttribute('placeholder'));
      }
      display.innerHTML = '<span></span><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">'
        + '<rect x="3" y="4" width="18" height="18" rx="2"/><path d="M16 2V6"/><path d="M8 2V6"/><path d="M3 10H21"/></svg>';
      wrapper.appendChild(display);
      field.__aluxenDisplay = display;
      syncDateDisplay(field);
    });
  }

  function syncDateDisplay(field) {
    var display = field.__aluxenDisplay;
    if (!display) return;
    var withTime = field.hasAttribute('data-datetime');
    var parsed = parseDateValue(field.value, withTime);
    var text = display.firstChild;
    if (text) text.textContent = parsed ? formatDisplayDate(parsed, withTime) : 'Selecione...';
    display.classList.toggle('filled', Boolean(parsed));
  }

  function setDateValue(field, value) {
    field.value = value;
    syncDateDisplay(field);
    field.dispatchEvent(new Event('change', { bubbles: true }));
  }

  function buildDatePicker(field) {
    var withTime = field.hasAttribute('data-datetime');
    var picker = document.createElement('div');
    picker.className = 'date-picker-dropdown';
    picker.setAttribute('role', 'dialog');

    var state = {
      field: field,
      withTime: withTime,
      view: null,
      picker: picker
    };
    var selected = parseDateValue(field.value, withTime);
    state.view = selected ? new Date(selected.getFullYear(), selected.getMonth(), 1) : new Date();

    var header = document.createElement('div');
    header.className = 'date-picker-header';

    var prev = document.createElement('button');
    prev.type = 'button';
    prev.className = 'date-picker-nav';
    prev.setAttribute('aria-label', 'Mês anterior');
    prev.title = 'Mês anterior';
    prev.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M15 18L9 12L15 6"/></svg>';
    prev.addEventListener('click', function () {
      state.view = new Date(state.view.getFullYear(), state.view.getMonth() - 1, 1);
      renderPicker(state);
    });

    var next = document.createElement('button');
    next.type = 'button';
    next.className = 'date-picker-nav';
    next.setAttribute('aria-label', 'Próximo mês');
    next.title = 'Próximo mês';
    next.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18L15 12L9 6"/></svg>';
    next.addEventListener('click', function () {
      state.view = new Date(state.view.getFullYear(), state.view.getMonth() + 1, 1);
      renderPicker(state);
    });

    var selectors = document.createElement('div');
    selectors.className = 'date-picker-selectors';

    var monthSelect = document.createElement('select');
    monthSelect.className = 'date-picker-select';
    monthSelect.setAttribute('aria-label', 'Selecionar mês');
    MONTH_NAMES.forEach(function (name, index) {
      var option = document.createElement('option');
      option.value = String(index);
      option.textContent = name;
      monthSelect.appendChild(option);
    });
    monthSelect.addEventListener('change', function () {
      state.view = new Date(state.view.getFullYear(), Number(monthSelect.value), 1);
      renderPicker(state);
    });

    var yearSelect = document.createElement('select');
    yearSelect.className = 'date-picker-select date-picker-select-year';
    yearSelect.setAttribute('aria-label', 'Selecionar ano');
    yearSelect.addEventListener('change', function () {
      state.view = new Date(Number(yearSelect.value), state.view.getMonth(), 1);
      renderPicker(state);
    });

    selectors.appendChild(monthSelect);
    selectors.appendChild(yearSelect);
    header.appendChild(prev);
    header.appendChild(selectors);
    header.appendChild(next);

    var weekdays = document.createElement('div');
    weekdays.className = 'date-picker-weekdays';
    WEEKDAY_NAMES.forEach(function (name) {
      var span = document.createElement('span');
      span.textContent = name;
      weekdays.appendChild(span);
    });

    var days = document.createElement('div');
    days.className = 'date-picker-days';

    var timeRow = null;
    var hourSelect = null;
    var minuteSelect = null;
    if (withTime) {
      timeRow = document.createElement('div');
      timeRow.className = 'date-picker-time';
      var timeLabel = document.createElement('span');
      timeLabel.className = 'date-picker-time-label';
      timeLabel.textContent = 'Hora';
      hourSelect = document.createElement('select');
      hourSelect.className = 'date-picker-select';
      hourSelect.setAttribute('aria-label', 'Hora');
      for (var hour = 0; hour < 24; hour += 1) {
        var hourOption = document.createElement('option');
        hourOption.value = String(hour);
        hourOption.textContent = pad2(hour);
        hourSelect.appendChild(hourOption);
      }
      minuteSelect = document.createElement('select');
      minuteSelect.className = 'date-picker-select';
      minuteSelect.setAttribute('aria-label', 'Minuto');
      for (var minute = 0; minute < 60; minute += 1) {
        var minuteOption = document.createElement('option');
        minuteOption.value = String(minute);
        minuteOption.textContent = pad2(minute);
        minuteSelect.appendChild(minuteOption);
      }
      var applyTime = function () {
        var current = parseDateValue(field.value, true) || new Date();
        var updated = new Date(
          current.getFullYear(),
          current.getMonth(),
          current.getDate(),
          Number(hourSelect.value),
          Number(minuteSelect.value)
        );
        setDateValue(field, formatIsoDate(updated, true));
        renderPicker(state);
      };
      hourSelect.addEventListener('change', applyTime);
      minuteSelect.addEventListener('change', applyTime);
      timeRow.appendChild(timeLabel);
      timeRow.appendChild(hourSelect);
      timeRow.appendChild(minuteSelect);
    }

    var footer = document.createElement('div');
    footer.className = 'date-picker-footer';
    var today = document.createElement('button');
    today.type = 'button';
    today.className = 'btn-text-sm';
    today.textContent = 'Hoje';
    today.addEventListener('click', function () {
      var now = new Date();
      state.view = new Date(now.getFullYear(), now.getMonth(), 1);
      setDateValue(field, formatIsoDate(now, withTime));
      renderPicker(state);
    });
    var clear = document.createElement('button');
    clear.type = 'button';
    clear.className = 'btn-text-sm text-muted';
    clear.textContent = 'Limpar';
    clear.addEventListener('click', function () {
      setDateValue(field, '');
      renderPicker(state);
    });
    footer.appendChild(today);
    footer.appendChild(clear);

    picker.appendChild(header);
    picker.appendChild(weekdays);
    picker.appendChild(days);
    if (timeRow) picker.appendChild(timeRow);
    picker.appendChild(footer);

    state.days = days;
    state.monthSelect = monthSelect;
    state.yearSelect = yearSelect;
    state.hourSelect = hourSelect;
    state.minuteSelect = minuteSelect;
    return state;
  }

  function renderPicker(state) {
    var field = state.field;
    var withTime = state.withTime;
    var view = state.view;
    var year = view.getFullYear();
    var month = view.getMonth();
    var selected = parseDateValue(field.value, withTime);
    var now = new Date();

    state.monthSelect.value = String(month);
    var startYear = Math.min(year, now.getFullYear() - 30);
    var endYear = Math.max(year, now.getFullYear() + 20);
    if (state.yearSelect.options.length === 0) {
      for (var candidate = startYear; candidate <= endYear; candidate += 1) {
        var option = document.createElement('option');
        option.value = String(candidate);
        option.textContent = String(candidate);
        state.yearSelect.appendChild(option);
      }
    }
    if (Number(state.yearSelect.options[0].value) > year) {
      state.yearSelect.innerHTML = '';
      for (var earlier = startYear; earlier <= endYear; earlier += 1) {
        var earlierOption = document.createElement('option');
        earlierOption.value = String(earlier);
        earlierOption.textContent = String(earlier);
        state.yearSelect.appendChild(earlierOption);
      }
    }
    state.yearSelect.value = String(year);

    if (state.hourSelect) {
      state.hourSelect.value = String(selected ? selected.getHours() : now.getHours());
      state.minuteSelect.value = String(selected ? selected.getMinutes() : now.getMinutes());
    }

    var daysHost = state.days;
    daysHost.innerHTML = '';
    var firstWeekday = new Date(year, month, 1).getDay();
    var daysInMonth = new Date(year, month + 1, 0).getDate();
    for (var blank = 0; blank < firstWeekday; blank += 1) {
      var spacer = document.createElement('span');
      spacer.className = 'blank';
      daysHost.appendChild(spacer);
    }
    for (var day = 1; day <= daysInMonth; day += 1) {
      var button = document.createElement('button');
      button.type = 'button';
      button.textContent = String(day);
      var isSelected = selected
        && selected.getFullYear() === year
        && selected.getMonth() === month
        && selected.getDate() === day;
      var isToday = now.getFullYear() === year && now.getMonth() === month && now.getDate() === day;
      if (isSelected) button.className = 'selected';
      else if (isToday) button.className = 'today';
      button.setAttribute('data-day', String(day));
      daysHost.appendChild(button);
    }
  }

  function openPicker(field) {
    closePicker();
    var state = buildDatePicker(field);
    document.body.appendChild(state.picker);
    renderPicker(state);
    state.picker.style.visibility = 'hidden';
    positionPopover(state.picker, field.__aluxenDisplay || field);
    state.picker.style.visibility = '';
    activePicker = state;
    field.__aluxenDisplay.setAttribute('aria-expanded', 'true');
  }

  function closePicker() {
    if (!activePicker) return;
    var state = activePicker;
    activePicker = null;
    state.picker.remove();
    if (state.field.__aluxenDisplay) {
      state.field.__aluxenDisplay.setAttribute('aria-expanded', 'false');
    }
  }

  /* ── Formulários de mutação ───────────────────────────────────────────── */

  function formParams(form) {
    var params = new URLSearchParams();
    new FormData(form).forEach(function (value, key) {
      if (typeof value !== 'string') return;
      params.append(key, value);
    });
    $$('[data-mask="money"]', form).forEach(function (field) {
      if (!field.name) return;
      params.delete(field.name);
      params.append(field.name, moneyValue(field.value));
    });
    return params;
  }

  function payloadOf(form) {
    var payload = {};
    formParams(form).forEach(function (value, key) {
      payload[key] = value;
    });
    return payload;
  }

  function setBusy(form, busy) {
    $$('button[type="submit"], button:not([type])', form).forEach(function (button) {
      if (busy) {
        if (button.hasAttribute('data-busy-original')) return;
        button.setAttribute('data-busy-original', button.disabled ? '1' : '0');
        button.disabled = true;
      } else if (button.hasAttribute('data-busy-original')) {
        button.disabled = button.getAttribute('data-busy-original') === '1';
        button.removeAttribute('data-busy-original');
      }
    });
    if (busy) form.setAttribute('aria-busy', 'true');
    else form.removeAttribute('aria-busy');
  }

  function reloadSoon() {
    window.setTimeout(function () {
      window.location.reload();
    }, 400);
  }

  function offlineMessage(payload) {
    var intent = payload.intent || '';
    if (/\.create$/.test(intent)) return 'Cadastro pendente de sincronização.';
    return 'Alteração pendente de sincronização.';
  }

  function enqueueForm(form) {
    if (isMultipartForm(form)) {
      toast('Sem conexão: este formulário envia arquivos e não pode ser guardado para envio automático.', 'warning');
      return { ok: false, queued: false };
    }
    var payload = payloadOf(form);
    enqueue({
      action: form.getAttribute('action') || '/dashboard',
      payload: payload,
      savedAt: Date.now()
    });
    toast(offlineMessage(payload), 'warning');
    closeModal(form.closest('.modal-backdrop'));
    return { ok: false, queued: true };
  }

  /** Formulário que envia arquivos (vistoria com fotos, importação): o corpo vai
   *  como multipart e os arquivos precisam chegar inteiros ao servidor. */
  function isMultipartForm(form) {
    var enctype = (form.getAttribute('enctype') || '').toLowerCase();
    if (enctype.indexOf('multipart/form-data') >= 0) return true;
    return Boolean(form.querySelector('input[type="file"]'));
  }

  function formBody(form) {
    if (!isMultipartForm(form)) return formParams(form).toString();
    var data = new FormData(form);
    $$('[data-mask="money"]', form).forEach(function (field) {
      if (!field.name) return;
      data.set(field.name, moneyValue(field.value));
    });
    return data;
  }

  function sendForm(form, options) {
    var action = form.getAttribute('action') || '/dashboard';
    var multipart = isMultipartForm(form);
    var headers = { Accept: 'application/json' };
    if (!multipart) headers['Content-Type'] = 'application/x-www-form-urlencoded;charset=UTF-8';
    setBusy(form, true);
    return fetch(action, {
      method: 'POST',
      headers: headers,
      body: formBody(form),
      credentials: 'same-origin'
    }).then(function (response) {
      var contentType = response.headers.get('content-type') || '';
      if (contentType.indexOf('json') === -1) {
        // Resposta HTML/redirect (ex.: /login, /logout, portal): o POST já foi
        // consumido pelo servidor, então seguimos o destino com uma navegação
        // normal em vez de repetir a mutação.
        setBusy(form, false);
        if (response.redirected && response.url) {
          window.location.assign(response.url);
          return { ok: true, native: true };
        }
        form.submit();
        return { ok: true, native: true };
      }
      return response.json().catch(function () {
        return {};
      }).then(function (body) {
        if (response.ok && body && body.ok) {
          toast((options && options.successMessage) || 'Alteração salva.', 'success');
          closeModal(form.closest('.modal-backdrop'));
          reloadSoon();
          return { ok: true, body: body };
        }
        setBusy(form, false);
        toast((body && body.error) || CONNECTION_SAVE_ERROR, 'error');
        return { ok: false, body: body };
      });
    }).catch(function () {
      setBusy(form, false);
      if (form.hasAttribute('data-offline')) return enqueueForm(form);
      toast(CONNECTION_ERROR, 'error');
      return { ok: false, body: {} };
    });
  }

  function submitIntent(intent, extra) {
    var payload = { intent: intent, csrf_token: csrfToken() };
    Object.keys(extra || {}).forEach(function (key) {
      payload[key] = extra[key];
    });
    return postPayload('/dashboard', payload).then(function (result) {
      if (result.ok) {
        toast('Alteração salva.', 'success');
        reloadSoon();
        return result;
      }
      toast((result.body && result.body.error) || CONNECTION_SAVE_ERROR, 'error');
      return result;
    });
  }

  /* ── Tabelas: ordenação, busca, paginação ─────────────────────────────── */

  function tableGroup(element) {
    return element.closest('[data-table-group]')
      || element.closest('section')
      || element.closest('.card')
      || element.closest('main')
      || document.body;
  }

  function stateOf(table) {
    return table.__aluxen;
  }

  function columnCount(table) {
    if (table.tHead && table.tHead.rows.length) return table.tHead.rows[0].cells.length;
    var firstRow = table.querySelector('tbody tr');
    return firstRow ? firstRow.cells.length : 1;
  }

  function cellSortValue(cell) {
    if (!cell) return '';
    var explicit = cell.getAttribute('data-sort-value');
    return coerceSortValue(explicit === null ? cell.textContent : explicit);
  }

  function coerceSortValue(text) {
    var value = String(text === null || text === undefined ? '' : text).trim();
    if (!value || value === '-') return '';
    var money = value.match(/^-?R\$\s*([\d.]+),(\d{2})$/);
    if (money) return Number(money[1].replace(/\./g, '') + '.' + money[2]);
    var iso = value.match(/^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}))?/);
    if (iso) return iso[1] + '-' + iso[2] + '-' + iso[3] + (iso[4] ? ' ' + iso[4] + ':' + iso[5] : '');
    var br = value.match(/^(\d{2})\/(\d{2})\/(\d{4})(?:[ T](\d{2}):(\d{2}))?/);
    if (br) return br[3] + '-' + br[2] + '-' + br[1] + (br[4] ? ' ' + br[4] + ':' + br[5] : '');
    if (/^-?\d{1,3}(\.\d{3})*(,\d+)?$/.test(value) || /^-?\d+(\.\d+)?$/.test(value)) {
      return Number(value.replace(/\./g, '').replace(',', '.'));
    }
    return normalizeText(value);
  }

  function compareValues(left, right, direction) {
    var result;
    if (typeof left === 'number' && typeof right === 'number') {
      result = left - right;
    } else {
      result = String(left).localeCompare(String(right), 'pt-BR', { numeric: true, sensitivity: 'base' });
    }
    return direction === 'desc' ? -result : result;
  }

  function rowSearchText(row) {
    if (row.__aluxenSearch === undefined) {
      row.__aluxenSearch = normalizeText(row.textContent + ' ' + (row.getAttribute('data-search') || ''));
    }
    return row.__aluxenSearch;
  }

  function matchesSelects(row, filters) {
    for (var index = 0; index < filters.length; index += 1) {
      var filter = filters[index];
      if (!filter.value) continue;
      var matched = filter.fields.some(function (field) {
        var actual = row.getAttribute('data-' + field);
        return actual !== null && actual === filter.value;
      });
      if (!matched) return false;
    }
    return true;
  }

  function tableRows(table) {
    if (!table.tBodies.length) return [];
    return Array.prototype.slice.call(table.tBodies[0].rows).filter(function (row) {
      return !row.hasAttribute('data-table-empty');
    });
  }

  function tableFilters(table, group) {
    var filters = [];
    $$('select[data-filter-select]', group).forEach(function (select) {
      if (tableGroup(select) !== group) return;
      filters.push({
        select: select,
        fields: String(select.getAttribute('data-filter-select') || '').split(',').map(function (field) {
          return field.trim();
        }).filter(Boolean),
        value: select.value
      });
    });
    return filters;
  }

  function tableSearchInput(table, group) {
    var target = table.getAttribute('data-filter-target');
    if (target) {
      var explicit = $(target);
      if (explicit) return explicit;
    }
    var inputs = $$('input[data-filter]', group).filter(function (input) {
      return tableGroup(input) === group;
    });
    return inputs.length ? inputs[0] : null;
  }

  function buildSortHeader(th) {
    if (th.getAttribute('data-sort-ready') === 'true') return;
    th.setAttribute('data-sort-ready', 'true');
    if (th.querySelector('button.sort-header')) return;
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'sort-header';
    while (th.firstChild) button.appendChild(th.firstChild);
    var arrows = document.createElement('span');
    arrows.className = 'sort-arrows';
    arrows.innerHTML = '<svg class="sort-arrow asc" width="8" height="5" viewBox="0 0 8 5" fill="none" aria-hidden="true"><path d="M4 0L7.5 5H0.5L4 0Z"/></svg>'
      + '<svg class="sort-arrow desc" width="8" height="5" viewBox="0 0 8 5" fill="none" aria-hidden="true"><path d="M4 5L0.5 0H7.5L4 5Z"/></svg>';
    button.appendChild(arrows);
    th.appendChild(button);
    th.setAttribute('aria-sort', 'none');
  }

  function buildPagination(table, state, host) {
    host.innerHTML = '';
    host.classList.add('pagination');

    var nav = document.createElement('div');
    nav.className = 'pagination-nav';

    var prev = document.createElement('button');
    prev.type = 'button';
    prev.className = 'btn btn-sm btn-secondary';
    prev.textContent = 'Anterior';
    prev.addEventListener('click', function () {
      if (state.page <= 1) return;
      state.page -= 1;
      applyTable(table);
    });

    var label = document.createElement('span');
    label.className = 'pagination-label';

    var next = document.createElement('button');
    next.type = 'button';
    next.className = 'btn btn-sm btn-secondary';
    next.textContent = 'Próxima';
    next.addEventListener('click', function () {
      state.page += 1;
      applyTable(table);
    });

    nav.appendChild(prev);
    nav.appendChild(label);
    nav.appendChild(next);

    var sizeHost = document.createElement('div');
    sizeHost.className = 'pagination-size';
    var sizeLabel = document.createElement('span');
    sizeLabel.className = 'pagination-label';
    sizeLabel.textContent = 'Itens por página';
    var sizeSelect = document.createElement('select');
    sizeSelect.className = 'pagination-size-select';
    sizeSelect.setAttribute('aria-label', 'Itens por página');
    PAGESIZE_OPTIONS.forEach(function (option) {
      var item = document.createElement('option');
      item.value = String(option);
      item.textContent = String(option);
      sizeSelect.appendChild(item);
    });
    sizeSelect.value = String(state.pageSize);
    sizeSelect.addEventListener('change', function () {
      state.pageSize = Number(sizeSelect.value) || state.defaultPageSize;
      state.page = 1;
      storageSet(PAGESIZE_KEY + state.key, String(state.pageSize));
      applyTable(table);
    });
    sizeHost.appendChild(sizeLabel);
    sizeHost.appendChild(sizeSelect);

    host.appendChild(nav);
    host.appendChild(sizeHost);

    state.ui = { prev: prev, next: next, label: label, sizeSelect: sizeSelect };
  }

  function ensurePagination(table, state, group) {
    var hosts = $$('[data-pagination]', group).filter(function (host) {
      return tableGroup(host) === group;
    });
    var host = hosts.length ? hosts[0] : null;
    if (!host) {
      host = document.createElement('div');
      host.setAttribute('data-pagination', '');
      var wrap = table.closest('.table-wrap') || table;
      wrap.parentNode.insertBefore(host, wrap.nextSibling);
    }
    buildPagination(table, state, host);
  }

  function updateEmptyRow(table, matchedCount) {
    var tbody = table.tBodies[0];
    if (!tbody) return;
    var existing = tbody.querySelector('tr[data-table-empty]');
    if (matchedCount === 0 && !existing) {
      var row = document.createElement('tr');
      row.setAttribute('data-table-empty', '');
      var cell = document.createElement('td');
      cell.className = 'empty-state';
      cell.colSpan = columnCount(table);
      cell.textContent = 'Nenhum registro encontrado.';
      row.appendChild(cell);
      tbody.appendChild(row);
      return;
    }
    if (matchedCount > 0 && existing) existing.remove();
  }

  function applyTable(table) {
    var state = stateOf(table);
    if (!state) return;
    var group = state.group;
    var search = tableSearchInput(table, group);
    state.filterText = search ? normalizeText(search.value) : '';
    state.filters = tableFilters(table, group);

    var rows = tableRows(table);
    var matched = [];
    rows.forEach(function (row) {
      var keep = true;
      if (state.filterText && rowSearchText(row).indexOf(state.filterText) === -1) keep = false;
      if (keep && !matchesSelects(row, state.filters)) keep = false;
      if (keep) matched.push(row);
    });

    if (state.sortIndex >= 0) {
      matched = matched.slice().sort(function (left, right) {
        return compareValues(
          cellSortValue(left.cells[state.sortIndex]),
          cellSortValue(right.cells[state.sortIndex]),
          state.sortDir
        );
      });
    }

    matched.forEach(function (row) {
      if (table.tBodies[0].lastChild !== row) table.tBodies[0].appendChild(row);
    });

    var totalPages = Math.max(1, Math.ceil(matched.length / state.pageSize));
    if (state.page > totalPages) state.page = totalPages;
    var start = (state.page - 1) * state.pageSize;
    var end = start + state.pageSize;

    rows.forEach(function (row) {
      var index = matched.indexOf(row);
      row.hidden = index === -1 || index < start || index >= end;
    });

    updateEmptyRow(table, matched.length);

    if (state.ui) {
      state.ui.label.textContent = 'Página ' + state.page + ' de ' + totalPages;
      state.ui.prev.disabled = state.page <= 1;
      state.ui.next.disabled = state.page >= totalPages;
      state.ui.sizeSelect.value = String(state.pageSize);
    }

    var headers = $$('thead th[data-sort]', table);
    headers.forEach(function (th) {
      var index = Array.prototype.indexOf.call(th.parentNode.children, th);
      var active = index === state.sortIndex;
      th.setAttribute('aria-sort', active ? (state.sortDir === 'asc' ? 'ascending' : 'descending') : 'none');
      var button = th.querySelector('button.sort-header');
      if (!button) return;
      button.classList.toggle('active', active);
      var asc = button.querySelector('.sort-arrow.asc');
      var desc = button.querySelector('.sort-arrow.desc');
      if (asc) asc.classList.toggle('active', active && state.sortDir === 'asc');
      if (desc) desc.classList.toggle('active', active && state.sortDir === 'desc');
    });

    applyBatch();
  }

  function sortByHeader(th) {
    var table = th.closest('table[data-table]');
    if (!table) return;
    var state = stateOf(table);
    if (!state) return;
    var index = Array.prototype.indexOf.call(th.parentNode.children, th);
    if (state.sortIndex === index && state.sortDir === 'asc') {
      state.sortDir = 'desc';
    } else {
      state.sortDir = 'asc';
    }
    state.sortIndex = index;
    state.page = 1;
    applyTable(table);
  }

  function initTables() {
    $$('table[data-table]').forEach(function (table) {
      if (stateOf(table)) return;
      var group = tableGroup(table);
      var key = table.getAttribute('data-table') || table.id || 'default';
      var defaultPageSize = Number(table.getAttribute('data-page-size')) || 25;
      var stored = Number(storageGet(PAGESIZE_KEY + key));
      var state = {
        key: key,
        group: group,
        defaultPageSize: defaultPageSize,
        pageSize: stored > 0 ? stored : defaultPageSize,
        page: 1,
        sortIndex: -1,
        sortDir: 'asc',
        filterText: '',
        filters: [],
        ui: null
      };
      table.__aluxen = state;
      $$('thead th[data-sort]', table).forEach(buildSortHeader);
      ensurePagination(table, state, group);
      applyTable(table);
    });
  }

  /* ── Seleção em lote ──────────────────────────────────────────────────── */

  function batchContainers() {
    return $$('[data-batch]');
  }

  function batchItems(container) {
    return $$('[data-batch-item]', container).filter(function (item) {
      return item.type === 'checkbox';
    });
  }

  function selectedItems(container) {
    return batchItems(container).filter(function (item) {
      return item.checked;
    });
  }

  function batchAllBox(container) {
    var explicit = $('[data-batch-all]', container);
    if (explicit) return explicit;
    var table = container.querySelector('table[data-table]');
    if (!table || !table.tHead) return null;
    return table.tHead.querySelector('input[type="checkbox"]');
  }

  function applyBatch() {
    batchContainers().forEach(function (container) {
      var items = batchItems(container);
      var selected = selectedItems(container);
      var count = selected.length;

      var counter = $('[data-batch-count]', container);
      if (counter) counter.textContent = count + ' selecionado(s)';

      var all = batchAllBox(container);
      if (all) {
        var visible = items.filter(function (item) {
          var row = item.closest('tr');
          return !row || !row.hidden;
        });
        var checkedVisible = visible.filter(function (item) {
          return item.checked;
        }).length;
        all.checked = visible.length > 0 && checkedVisible === visible.length;
        all.indeterminate = checkedVisible > 0 && checkedVisible < visible.length;
      }

      $$('[data-batch-action]', container).forEach(function (button) {
        var template = button.getAttribute('data-batch-label');
        if (template) button.textContent = template.replace('{n}', String(count));
        if (!button.hasAttribute('data-batch-allow-empty')) button.disabled = count === 0;
      });

      $$('[data-batch-toolbar]', container).forEach(function (toolbar) {
        if (!toolbar.hasAttribute('data-batch-hide-empty')) return;
        toolbar.hidden = count === 0;
      });
    });
  }

  function toggleAllBatch(container, checked) {
    batchItems(container).forEach(function (item) {
      var row = item.closest('tr');
      if (row && row.hidden) return;
      item.checked = checked;
    });
    applyBatch();
  }

  function batchIntent(button) {
    var value = button.getAttribute('data-batch-action');
    if (value && value.indexOf('.') > 0) return value;
    var form = button.closest('form') || (button.closest('[data-batch]') || document).querySelector('form');
    var field = form ? form.querySelector('[name="intent"]') : null;
    return field ? field.value : value || '';
  }

  function runBatchAction(button) {
    var container = button.closest('[data-batch]') || document;
    var ids = selectedItems(container).map(function (item) {
      return item.value;
    }).filter(Boolean);
    if (!ids.length && !button.hasAttribute('data-batch-allow-empty')) return;

    var confirmText = button.getAttribute('data-confirm');
    if (confirmText && !window.confirm(confirmText)) return;

    var form = button.closest('form') || container.querySelector('form');
    if (form) {
      var idsField = form.querySelector('[data-batch-ids], input[name="ids"]');
      if (idsField) idsField.value = ids.join(',');
      var intentField = form.querySelector('[name="intent"]');
      var intent = batchIntent(button);
      if (intentField && intent) intentField.value = intent;
      if (window.navigator.onLine === false) {
        if (form.hasAttribute('data-offline')) {
          enqueueForm(form);
          return;
        }
        toast(OFFLINE_NOTICE, 'warning');
      }
      sendForm(form);
      return;
    }

    submitIntent(batchIntent(button), { ids: ids.join(',') });
  }

  /* ── Linhas repetíveis (itens da vistoria) ────────────────────────────── */

  /** `data-inspection-add-item`: clona a última linha de `[data-inspection-items]`,
   *  limpa os valores e acrescenta no fim (o servidor lê os campos repetidos). */
  function addInspectionItem(button) {
    var scope = button.closest('form') || button.closest('.modal-backdrop') || document;
    var host = scope.querySelector('[data-inspection-items]') || document.querySelector('[data-inspection-items]');
    if (!host) return;
    var last = host.lastElementChild;
    if (!last) return;

    var row = last.cloneNode(true);
    $$('input, select, textarea', row).forEach(function (field) {
      if (field.type === 'checkbox' || field.type === 'radio') {
        field.checked = false;
      } else if (field.tagName === 'SELECT') {
        field.selectedIndex = 0;
      } else {
        field.value = '';
      }
      field.removeAttribute('id');
      field.removeAttribute('value');
    });
    $$('label[for]', row).forEach(function (label) {
      label.removeAttribute('for');
    });
    host.appendChild(row);

    var first = row.querySelector('input, select, textarea');
    if (first && first.focus) first.focus();
  }

  /* ── Modais ───────────────────────────────────────────────────────────── */

  function openModals() {
    return $$('.modal-backdrop').filter(function (modal) {
      return !modal.hidden;
    });
  }

  function modalFields(modal) {
    return $$('input[name], select[name], textarea[name]', modal);
  }

  function snapshotModal(modal) {
    if (modal.__aluxenBaseline) return;
    var baseline = {};
    modalFields(modal).forEach(function (field) {
      baseline[field.name] = {
        value: field.value,
        checked: field.checked === true
      };
    });
    modal.__aluxenBaseline = baseline;
  }

  function restoreModal(modal) {
    var baseline = modal.__aluxenBaseline;
    if (!baseline) return;
    modalFields(modal).forEach(function (field) {
      var saved = baseline[field.name];
      if (!saved) return;
      field.value = saved.value;
      if (field.type === 'checkbox' || field.type === 'radio') field.checked = saved.checked;
      syncFieldDisplay(field);
    });
  }

  function syncFieldDisplay(field) {
    var mask = field.getAttribute('data-mask');
    if (mask === 'money') {
      field.value = moneyFromDecimal(field.value);
      return;
    }
    if (field.__aluxenDisplay) {
      syncDateDisplay(field);
    }
  }

  function truthy(value) {
    if (value === true) return true;
    if (value === false || value === null || value === undefined) return false;
    var text = String(value).toLowerCase();
    return text === '1' || text === 'true' || text === 'on' || text === 'sim';
  }

  function applyModalFill(modal, opener) {
    var raw = opener.getAttribute('data-modal-fill');
    if (raw === null) return;
    var fill;
    try {
      fill = JSON.parse(raw);
    } catch (error) {
      if (window.console && window.console.warn) {
        window.console.warn('data-modal-fill inválido em', opener, error);
      }
      return;
    }
    if (!fill || typeof fill !== 'object') return;
    Object.keys(fill).forEach(function (key) {
      var field = modal.querySelector('[name="' + key + '"], [data-field="' + key + '"]');
      if (!field) return;
      if (field.type === 'checkbox' || field.type === 'radio') {
        field.checked = truthy(fill[key]);
        return;
      }
      field.value = fill[key] === null || fill[key] === undefined ? '' : String(fill[key]);
      syncFieldDisplay(field);
    });
  }

  function focusFirstField(modal) {
    if (modal.hasAttribute('data-modal-no-focus')) return;
    var explicit = modal.getAttribute('data-modal-focus');
    var target = explicit ? $(explicit, modal) : null;
    if (!target) {
      target = $$('input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([data-modal-close])', modal)[0];
    }
    if (target && target.focus) target.focus();
  }

  function openModal(id, opener) {
    var modal = typeof id === 'string' ? document.getElementById(id) : id;
    if (!modal) return;
    snapshotModal(modal);
    restoreModal(modal);
    if (opener) applyModalFill(modal, opener);
    modal.hidden = false;
    modal.setAttribute('aria-hidden', 'false');
    document.body.classList.add('modal-open');
    focusFirstField(modal);
  }

  function closeModal(modal) {
    if (!modal) return;
    modal.hidden = true;
    modal.setAttribute('aria-hidden', 'true');
    if (!openModals().length) document.body.classList.remove('modal-open');
  }

  function closeTopModal() {
    var modals = openModals();
    if (!modals.length) return false;
    closeModal(modals[modals.length - 1]);
    return true;
  }

  /* ── WhatsApp (envio assistido) ───────────────────────────────────────── */

  function whatsAppFriendlyError(raw) {
    var message = String(raw || '').toLowerCase();
    if (message.indexOf('already running') >= 0) {
      return 'Já existe um envio em andamento. Aguarde a conclusão e tente novamente.';
    }
    if (message.indexOf('chromium') >= 0 || message.indexOf('failed to launch') >= 0) {
      return 'O envio automático está indisponível neste dispositivo no momento.';
    }
    if (message.indexOf('expirad') >= 0 || message.indexOf('autenticad') >= 0 || message.indexOf('whatsapp:login') >= 0) {
      return 'Sua conexão com o WhatsApp expirou. Conecte novamente para enviar.';
    }
    if (message.indexOf('waiting failed') >= 0 || message.indexOf('timeout') >= 0
      || message.indexOf('exceeded') >= 0 || message.indexOf('tempo esgotado') >= 0) {
      return 'Não foi possível concluir o envio pelo WhatsApp agora. Tente novamente em instantes.';
    }
    return raw || 'Falha ao enviar mensagem.';
  }

  function whatsAppModal() {
    var modal = document.getElementById('aluxen-whatsapp-modal');
    if (modal) return modal;
    modal = document.createElement('div');
    modal.id = 'aluxen-whatsapp-modal';
    modal.className = 'modal-backdrop';
    modal.hidden = true;
    modal.setAttribute('aria-hidden', 'true');
    modal.innerHTML = '<div class="modal-card">'
      + '<div class="modal-header"><h3>Enviar por WhatsApp</h3>'
      + '<button type="button" class="btn btn-sm btn-outline" data-modal-close>Fechar</button></div>'
      + '<div class="stack" data-whatsapp-body></div>'
      + '</div>';
    document.body.appendChild(modal);
    return modal;
  }

  function whatsAppState(modal) {
    if (!modal.__aluxenState) {
      modal.__aluxenState = { poll: null, preview: null, sending: false, stage: '' };
    }
    return modal.__aluxenState;
  }

  function whatsAppStopPolling(modal) {
    var state = whatsAppState(modal);
    if (state.poll) state.poll.cancelled = true;
    state.poll = null;
  }

  function whatsAppBody(modal) {
    return modal.querySelector('[data-whatsapp-body]');
  }

  function whatsAppRenderPreview(modal) {
    var state = whatsAppState(modal);
    var preview = state.preview || {};
    var body = whatsAppBody(modal);
    body.innerHTML = '<p class="cell-muted"><strong>Locatário:</strong> <span data-whatsapp-tenant></span></p>'
      + '<p class="cell-muted"><strong>Imóvel:</strong> <span data-whatsapp-property></span></p>'
      + '<div class="form-group"><label for="aluxen-whatsapp-phone">Número de WhatsApp (com DDD)</label>'
      + '<input id="aluxen-whatsapp-phone" type="text" data-mask="phone" data-whatsapp-phone value=""></div>'
      + '<div class="form-group"><label for="aluxen-whatsapp-message">Mensagem de Envio</label>'
      + '<textarea id="aluxen-whatsapp-message" rows="6" data-whatsapp-message></textarea></div>'
      + '<div data-whatsapp-feedback></div>'
      + '<div data-whatsapp-steps></div>'
      + '<div class="modal-actions">'
      + '<a class="btn btn-outline" data-whatsapp-link target="_blank" rel="noopener">Abrir no WhatsApp</a>'
      + '<button type="button" class="btn btn-primary" data-whatsapp-confirm>Enviar</button>'
      + '</div>';
    body.querySelector('[data-whatsapp-tenant]').textContent = preview.tenantName || '-';
    body.querySelector('[data-whatsapp-property]').textContent = preview.propertyName || '-';
    var phone = body.querySelector('[data-whatsapp-phone]');
    phone.value = preview.phone || '';
    maskField(phone);
    var message = body.querySelector('[data-whatsapp-message]');
    message.value = preview.message || '';
    var link = body.querySelector('[data-whatsapp-link]');
    if (preview.directUrl) {
      link.setAttribute('href', preview.directUrl);
    } else {
      link.hidden = true;
    }
  }

  function whatsAppFeedback(modal, message, tone) {
    var host = whatsAppBody(modal).querySelector('[data-whatsapp-feedback]');
    if (!host) return;
    if (!message) {
      host.innerHTML = '';
      return;
    }
    var node = document.createElement('div');
    node.className = tone === 'error' ? 'error' : 'success';
    node.textContent = message;
    host.innerHTML = '';
    host.appendChild(node);
  }

  function whatsAppRenderSteps(modal) {
    var host = whatsAppBody(modal).querySelector('[data-whatsapp-steps]');
    if (!host) return;
    var state = whatsAppState(modal);
    if (!state.sending) {
      host.innerHTML = '';
      return;
    }
    var current = -1;
    WHATSAPP_STEPS.forEach(function (step, index) {
      if (step.stage === state.stage) current = index;
    });
    host.innerHTML = '';
    var list = document.createElement('ul');
    list.className = 'whatsapp-steps';
    WHATSAPP_STEPS.forEach(function (step, index) {
      var item = document.createElement('li');
      if (current === -1 || index > current) item.className = 'pending';
      else if (index === current) item.className = 'active';
      else item.className = 'done';
      item.textContent = step.label;
      list.appendChild(item);
    });
    host.appendChild(list);
  }

  function whatsAppFail(modal, message) {
    var state = whatsAppState(modal);
    whatsAppStopPolling(modal);
    state.sending = false;
    state.stage = '';
    whatsAppRenderSteps(modal);
    whatsAppFeedback(modal, whatsAppFriendlyError(message), 'error');
    var confirm = whatsAppBody(modal).querySelector('[data-whatsapp-confirm]');
    if (confirm) confirm.disabled = false;
  }

  function whatsAppSend(modal) {
    var state = whatsAppState(modal);
    var body = whatsAppBody(modal);
    var phone = body.querySelector('[data-whatsapp-phone]');
    var message = body.querySelector('[data-whatsapp-message]');
    var confirm = body.querySelector('[data-whatsapp-confirm]');
    if (state.sending) return;
    if (!phone.value || !message.value) {
      whatsAppFeedback(modal, 'Destinatário e mensagem são obrigatórios.', 'error');
      return;
    }

    state.sending = true;
    state.stage = 'preparing';
    whatsAppFeedback(modal, '', '');
    whatsAppRenderSteps(modal);
    if (confirm) confirm.disabled = true;

    var payload = new URLSearchParams();
    payload.append('to', phone.value);
    payload.append('message', message.value);
    payload.append('csrf_token', csrfToken());

    fetch('/api/whatsapp/send', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        Accept: 'application/json'
      },
      body: payload.toString(),
      credentials: 'same-origin'
    }).then(function (response) {
      return response.json().then(function (data) {
        return { ok: response.ok, data: data };
      }).catch(function () {
        return { ok: false, data: {} };
      });
    }).then(function (result) {
      if (!result.data || !result.data.ok || !result.data.jobId) {
        whatsAppFail(modal, (result.data && result.data.error) || 'Não foi possível iniciar o envio.');
        return;
      }
      whatsAppPoll(modal, result.data.jobId);
    }).catch(function () {
      whatsAppFail(modal, 'Erro de comunicação com o servidor.');
    });
  }

  function whatsAppPoll(modal, jobId) {
    var state = whatsAppState(modal);
    var poll = { cancelled: false };
    state.poll = poll;
    var deadline = Date.now() + WHATSAPP_SEND_TIMEOUT;

    function step() {
      if (poll.cancelled || !modal.isConnected) return;
      if (Date.now() > deadline) {
        whatsAppFail(modal, 'O envio está demorando mais que o esperado. Confira a conversa no WhatsApp antes de tentar novamente.');
        return;
      }
      delay(WHATSAPP_POLL_INTERVAL).then(function () {
        if (poll.cancelled || !modal.isConnected) return;
        fetch('/api/whatsapp/send/status?jobId=' + encodeURIComponent(jobId), {
          credentials: 'same-origin',
          headers: { Accept: 'application/json' }
        }).then(function (response) {
          return response.json();
        }).then(function (data) {
          if (poll.cancelled) return;
          var job = data && data.job;
          if (!job) {
            step();
            return;
          }
          if (job.status === 'done') {
            state.poll = null;
            state.sending = false;
            state.stage = '';
            whatsAppRenderSteps(modal);
            whatsAppFeedback(modal, 'Mensagem enviada com sucesso pelo WhatsApp!', 'success');
            var confirm = whatsAppBody(modal).querySelector('[data-whatsapp-confirm]');
            if (confirm) confirm.disabled = false;
            return;
          }
          if (job.status === 'error') {
            whatsAppFail(modal, job.error || 'Falha ao enviar mensagem.');
            return;
          }
          state.stage = job.stage || state.stage;
          whatsAppRenderSteps(modal);
          step();
        }).catch(function () {
          // Oscilação momentânea de rede: mantém o acompanhamento.
          step();
        });
      });
    }

    step();
  }

  function openWhatsAppSend(button) {
    var type = button.getAttribute('data-whatsapp-send') || 'receipt';
    var id = button.getAttribute('data-whatsapp-id') || '';
    var modal = whatsAppModal();
    var state = whatsAppState(modal);
    whatsAppStopPolling(modal);
    state.preview = null;
    state.sending = false;
    state.stage = '';
    whatsAppBody(modal).innerHTML = '<p class="cell-muted">Carregando prévia…</p>';
    openModal(modal);

    var payload = new URLSearchParams();
    payload.append('type', type);
    payload.append('id', id);
    payload.append('csrf_token', csrfToken());

    fetch('/api/whatsapp/preview', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        Accept: 'application/json'
      },
      body: payload.toString(),
      credentials: 'same-origin'
    }).then(function (response) {
      return response.json().catch(function () {
        return {};
      });
    }).then(function (data) {
      if (data && data.ok) {
        state.preview = data;
        whatsAppRenderPreview(modal);
        return;
      }
      whatsAppBody(modal).innerHTML = '';
      whatsAppFeedback(modal, (data && data.error) || 'Não foi possível carregar prévia.', 'error');
    }).catch(function () {
      whatsAppBody(modal).innerHTML = '';
      whatsAppFeedback(modal, 'Erro de conexão ao carregar dados do WhatsApp.', 'error');
    });
  }

  /* ── Menu lateral no mobile ───────────────────────────────────────────── */

  function initMobileMenu() {
    var sidebar = document.querySelector('.sidebar');
    if (!sidebar || document.querySelector('.hamburger-float')) return;
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'hamburger-float';
    button.setAttribute('aria-label', 'Abrir menu');
    button.setAttribute('aria-expanded', 'false');
    button.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">'
      + '<path d="M4 7h16M4 12h16M4 17h16"/></svg>';
    var backdrop = document.createElement('div');
    backdrop.className = 'sidebar-backdrop';
    backdrop.hidden = true;
    document.body.appendChild(button);
    document.body.appendChild(backdrop);

    var setOpen = function (open) {
      sidebar.classList.toggle('mobile-open', open);
      backdrop.hidden = !open;
      button.setAttribute('aria-expanded', open ? 'true' : 'false');
      button.setAttribute('aria-label', open ? 'Fechar menu' : 'Abrir menu');
    };
    button.addEventListener('click', function () {
      setOpen(!sidebar.classList.contains('mobile-open'));
    });
    backdrop.addEventListener('click', function () {
      setOpen(false);
    });
    sidebar.addEventListener('click', function (event) {
      var link = event.target.closest ? event.target.closest('a') : null;
      if (link) setOpen(false);
    });
    window.addEventListener('resize', function () {
      if (window.innerWidth > 768) setOpen(false);
    });
  }

  /* ── Delegação de eventos ─────────────────────────────────────────────── */

  document.addEventListener('click', function (event) {
    var target = element(event.target);
    if (!target) return;

    if (activePicker && !target.closest('.date-picker-dropdown') && !target.closest('.custom-date-picker')) {
      closePicker();
    }

    var themeToggle = target.closest('[data-theme-toggle]');
    if (themeToggle) {
      event.preventDefault();
      toggleTheme();
      return;
    }

    var opener = target.closest('[data-modal-open]');
    if (opener) {
      event.preventDefault();
      openModal(opener.getAttribute('data-modal-open'), opener);
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
      return;
    }

    var dateTrigger = target.closest('.date-picker-display');
    if (dateTrigger) {
      event.preventDefault();
      var dateField = dateTrigger.parentNode.querySelector('input[data-datepicker]');
      if (dateField) openPicker(dateField);
      return;
    }

    var dayButton = target.closest('.date-picker-days button');
    if (dayButton && activePicker) {
      event.preventDefault();
      var pickerState = activePicker;
      var day = Number(dayButton.getAttribute('data-day'));
      var view = pickerState.view;
      var current = parseDateValue(pickerState.field.value, pickerState.withTime);
      var updated = new Date(
        view.getFullYear(),
        view.getMonth(),
        day,
        pickerState.withTime && current ? current.getHours() : 0,
        pickerState.withTime && current ? current.getMinutes() : 0
      );
      setDateValue(pickerState.field, formatIsoDate(updated, pickerState.withTime));
      renderPicker(pickerState);
      if (!pickerState.withTime) closePicker();
      return;
    }

    var sortHeader = target.closest('th[data-sort]');
    if (sortHeader) {
      event.preventDefault();
      sortByHeader(sortHeader);
      return;
    }

    var batchButton = target.closest('[data-batch-action]');
    if (batchButton) {
      event.preventDefault();
      runBatchAction(batchButton);
      return;
    }

    var inspectionButton = target.closest('[data-inspection-add-item]');
    if (inspectionButton) {
      event.preventDefault();
      addInspectionItem(inspectionButton);
      return;
    }

    var whatsAppButton = target.closest('[data-whatsapp-send]');
    if (whatsAppButton) {
      event.preventDefault();
      openWhatsAppSend(whatsAppButton);
      return;
    }

    var whatsAppConfirm = target.closest('[data-whatsapp-confirm]');
    if (whatsAppConfirm) {
      event.preventDefault();
      whatsAppSend(whatsAppConfirm.closest('.modal-backdrop'));
      return;
    }

    var tabLink = target.closest('a.tab-link');
    if (tabLink && window.navigator.onLine === false) {
      var tab = '';
      try {
        tab = new URLSearchParams(tabLink.search).get('tab') || '';
      } catch (error) {
        tab = '';
      }
      if (tab && restoreTabCache(tab)) {
        event.preventDefault();
        return;
      }
      toast('Sem conexão: esta aba ainda não foi carregada para uso offline.', 'warning');
    }
  });

  document.addEventListener('change', function (event) {
    var target = element(event.target);
    if (!target) return;

    if (target.matches('[data-batch-item]')) {
      applyBatch();
      return;
    }

    if (target.matches('[data-batch-all]')) {
      var container = target.closest('[data-batch]');
      if (container) toggleAllBatch(container, target.checked);
      return;
    }

    if (target.matches('select[data-filter-select]')) {
      var group = tableGroup(target);
      $$('table[data-table]', group).forEach(function (table) {
        if (tableGroup(table) !== group) return;
        var state = stateOf(table);
        if (!state) return;
        state.page = 1;
        applyTable(table);
      });
    }
  });

  document.addEventListener('input', function (event) {
    var target = element(event.target);
    if (!target) return;

    if (target.matches('input[data-mask]')) {
      maskField(target, true);
    }

    if (target.matches('input[data-filter]')) {
      var group = tableGroup(target);
      $$('table[data-table]', group).forEach(function (table) {
        if (tableGroup(table) !== group) return;
        var state = stateOf(table);
        if (!state) return;
        state.page = 1;
        applyTable(table);
      });
    }
  });

  document.addEventListener('submit', function (event) {
    var form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.hasAttribute('data-native')) return;
    var method = (form.getAttribute('method') || 'get').toLowerCase();
    if (method !== 'post') return;

    event.preventDefault();

    var submitter = event.submitter || null;
    var confirmText = (submitter && submitter.getAttribute('data-confirm')) || form.getAttribute('data-confirm');
    if (confirmText && !window.confirm(confirmText)) return;

    if (window.navigator.onLine === false && form.hasAttribute('data-offline')) {
      enqueueForm(form);
      return;
    }

    sendForm(form);
  });

  document.addEventListener('keydown', function (event) {
    if (event.key !== 'Escape') return;
    if (activePicker) {
      closePicker();
      return;
    }
    closeTopModal();
  });

  window.addEventListener('online', function () {
    updateSyncBanner(readQueue().length);
    flushQueue();
  });

  window.addEventListener('offline', function () {
    updateSyncBanner(readQueue().length);
  });

  window.addEventListener('beforeunload', function () {
    if (activePicker) closePicker();
  });

  /* ── PWA ──────────────────────────────────────────────────────────────── */

  function registerServiceWorker() {
    if (!('serviceWorker' in window.navigator)) return;
    if (window.location.protocol === 'file:') return;
    window.addEventListener('load', function () {
      window.navigator.serviceWorker.register('/sw.js').catch(function () {
        /* sem service worker o painel segue funcionando normalmente */
      });
    });
  }

  /* ── Inicialização ────────────────────────────────────────────────────── */

  function refresh() {
    initMasks();
    initDatepickers();
    initTables();
    applyBatch();
    updateSyncBanner(readQueue().length);
  }

  function boot() {
    if (booted) return;
    booted = true;
    initMobileMenu();
    refresh();
    saveTabCache();
    registerServiceWorker();
    if (window.navigator.onLine !== false) flushQueue();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }

  window.aluxen = {
    toast: toast,
    currentTheme: currentTheme,
    applyTheme: applyTheme,
    toggleTheme: toggleTheme,
    refresh: refresh,
    readQueue: readQueue,
    writeQueue: writeQueue,
    flushQueue: flushQueue,
    readCache: readCache,
    saveTabCache: saveTabCache,
    openModal: openModal,
    closeModal: closeModal
  };
})();
