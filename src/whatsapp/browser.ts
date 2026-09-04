import puppeteer from 'puppeteer-core';
import type { Browser, Page } from 'puppeteer-core';
import { resolve } from 'node:path';
import { existsSync, unlinkSync } from 'node:fs';

export function resolveProfileDir(): string {
  if (process.env.WHATSAPP_PROFILE_DIR) {
    return resolve(process.env.WHATSAPP_PROFILE_DIR);
  }
  const siblingProfile = resolve(process.cwd(), '../whatsapp/.profile');
  if (existsSync(siblingProfile)) {
    return siblingProfile;
  }
  return resolve(process.cwd(), '.whatsapp-profile');
}

export const PROFILE_DIR = resolveProfileDir();

export const DESKTOP_USER_AGENT =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36';

export class SessionExpiredError extends Error {
  constructor(message = 'WhatsApp Web não autenticado ou sessão expirada.') {
    super(message);
    this.name = 'SessionExpiredError';
  }
}

export function findBrowserBinary(): string {
  if (process.env.CHROMIUM_PATH && existsSync(process.env.CHROMIUM_PATH)) {
    return process.env.CHROMIUM_PATH;
  }

  const candidates = [
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/google-chrome',
    '/snap/bin/chromium',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
  ];

  for (const candidate of candidates) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }

  throw new Error(
    'Nenhum binário do Chromium ou Google Chrome foi encontrado no sistema.\n' +
      'Defina a variável CHROMIUM_PATH apontando para o executável.'
  );
}

export async function launchBrowser(options: { headless?: boolean } = {}): Promise<Browser> {
  const executablePath = findBrowserBinary();
  const headless = options.headless ?? true;
  const profilePath = resolveProfileDir();

  // Limpeza de travas antigas do Chromium caso o processo anterior tenha caído
  const singletonLock = resolve(profilePath, 'SingletonLock');
  if (existsSync(singletonLock)) {
    try {
      unlinkSync(singletonLock);
    } catch {}
  }

  const browser = await puppeteer.launch({
    executablePath,
    headless,
    userDataDir: profilePath,
    args: [
      '--no-sandbox',
      '--disable-setuid-sandbox',
      '--disable-dev-shm-usage',
      '--disable-accelerated-2d-canvas',
      '--no-first-run',
      '--no-zygote',
      '--disable-gpu',
      '--window-size=1280,800',
    ],
    defaultViewport: {
      width: 1280,
      height: 800,
    },
  });

  return browser;
}

export async function openWhatsApp(
  browser: Browser,
  options: { headless?: boolean; silent?: boolean } = {}
): Promise<Page> {
  const { headless = true, silent = false } = options;
  const pages = await browser.pages();
  const page = pages[0] || (await browser.newPage());

  await page.setUserAgent(DESKTOP_USER_AGENT);

  if (!silent) {
    console.log('[WhatsApp] Acessando web.whatsapp.com...');
  }

  await page.goto('https://web.whatsapp.com', {
    waitUntil: 'domcontentloaded',
    timeout: 60000,
  });

  if (!headless) {
    return page;
  }

  const result = await Promise.race([
    page
      .waitForSelector('#pane-side, [data-testid="chat-list"]', { timeout: 45000 })
      .then(() => 'logged_in' as const),
    page
      .waitForSelector('canvas[aria-label*="Scan" i], [data-ref], [data-testid="qrcode"]', {
        timeout: 45000,
      })
      .then(() => 'needs_qr' as const),
  ]).catch(() => 'timeout' as const);

  if (result === 'needs_qr') {
    throw new SessionExpiredError(
      'Sessão do WhatsApp não encontrada ou expirada.\n' +
        'Execute "bun run whatsapp:login" para autenticar o QR Code.'
    );
  }

  if (result === 'timeout') {
    throw new Error('Tempo esgotado aguardando o carregamento do WhatsApp Web.');
  }

  return page;
}
