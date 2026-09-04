import type { Browser, Page } from 'puppeteer-core';
import { launchBrowser, resolveProfileDir, isChromiumAvailable } from './browser';
import type { LoginState } from './types';
import QRCode from 'qrcode';
import { existsSync, rmSync } from 'node:fs';

class WhatsAppLoginManager {
  private browser: Browser | null = null;
  private page: Page | null = null;
  private state: LoginState = { status: 'idle' };
  private monitorTimer: any = null;
  private timeoutTimer: any = null;
  private lastQrRaw: string | null = null;
  private isProcessing = false;

  getState(): LoginState {
    return this.state;
  }

  async startLogin(): Promise<LoginState> {
    if (!isChromiumAvailable()) {
      this.state = {
        status: 'error',
        error:
          'Nenhum binário do Chromium ou Google Chrome foi encontrado no sistema. Defina a variável CHROMIUM_PATH ou verifique a instalação.',
      };
      return this.state;
    }

    if (
      this.state.status === 'scan_qr' ||
      this.state.status === 'starting' ||
      this.state.status === 'syncing'
    ) {
      return this.state;
    }

    await this.cancelLogin();

    this.state = { status: 'starting', message: 'Iniciando navegador e acessando WhatsApp Web...' };
    this.isProcessing = true;

    try {
      this.browser = await launchBrowser({ headless: true });
      const pages = await this.browser.pages();
      this.page = pages[0] || (await this.browser.newPage());

      await this.page.setUserAgent(
        'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
      );

      // Limite de 3 minutos para autenticação
      this.timeoutTimer = setTimeout(() => {
        if (this.state.status !== 'connected') {
          void this.cancelLogin('Tempo limite de 3 minutos esgotado. Tente novamente.');
        }
      }, 180000);

      console.log('[WhatsApp Login] Acessando web.whatsapp.com...');
      await this.page.goto('https://web.whatsapp.com', {
        waitUntil: 'domcontentloaded',
        timeout: 60000,
      });

      this.startMonitoring();
    } catch (err: any) {
      console.error('[WhatsApp Login] Erro ao iniciar:', err);
      await this.cancelLogin(err?.message || 'Falha ao iniciar o WhatsApp Web.');
    }

    return this.state;
  }

  private startMonitoring() {
    if (this.monitorTimer) clearInterval(this.monitorTimer);

    this.monitorTimer = setInterval(async () => {
      if (!this.page || !this.browser || !this.isProcessing) {
        if (this.monitorTimer) clearInterval(this.monitorTimer);
        return;
      }

      try {
        // 1. Verificar se já concluiu o login (painel de conversas carregado)
        const isChatsLoaded = await this.page.evaluate(() => {
          const pane = document.querySelector(
            '#pane-side, [data-testid="chat-list"], div[role="navigation"]'
          );
          return !!pane;
        });

        if (isChatsLoaded) {
          console.log('[WhatsApp Login] Login detectado! Sincronizando dados...');
          if (this.monitorTimer) clearInterval(this.monitorTimer);
          if (this.timeoutTimer) clearTimeout(this.timeoutTimer);
          this.isProcessing = false;

          this.state = { status: 'syncing', message: 'Leitura confirmada! Sincronizando conversas e chaves...' };

          // Aguarda 3 segundos para que os dados e cookies sejam gravados no disco
          await Bun.sleep(3000);

          try {
            await this.browser.close();
          } catch {}

          this.browser = null;
          this.page = null;
          this.state = { status: 'connected', message: 'WhatsApp conectado com sucesso ao Aluxen!' };
          console.log('[WhatsApp Login] Sessão salva em:', resolveProfileDir());
          return;
        }

        // 2. Tentar ler o valor do QR Code
        const qrRaw = await this.page.evaluate(() => {
          const refContainer = document.querySelector('[data-ref]');
          if (refContainer) {
            const attr = refContainer.getAttribute('data-ref');
            if (attr) return attr;
          }

          const canvas = document.querySelector(
            'canvas[aria-label*="Scan" i], [data-testid="qrcode"] canvas, canvas'
          );
          if (canvas) {
            const attr = canvas.getAttribute('data-ref');
            if (attr) return attr;
          }

          return null;
        });

        if (qrRaw) {
          if (qrRaw !== this.lastQrRaw || this.state.status !== 'scan_qr') {
            this.lastQrRaw = qrRaw;
            const qrCodeDataUrl = await QRCode.toDataURL(qrRaw, {
              width: 280,
              margin: 2,
              color: {
                dark: '#0b0b0d',
                light: '#ffffff',
              },
            });

            this.state = {
              status: 'scan_qr',
              qrCode: qrCodeDataUrl,
              message: 'Abra o WhatsApp no celular > Aparelhos Conectados > Conectar e aponte a câmera.',
            };
          }
        } else {
          // Fallback via screenshot do canvas caso não encontre data-ref em texto
          const qrElement = await this.page.$('canvas, [data-testid="qrcode"], div[data-ref]');
          if (qrElement && this.state.status !== 'scan_qr') {
            const screenshotBuffer = await qrElement.screenshot({ type: 'png' });
            const dataUrl = `data:image/png;base64,${Buffer.from(screenshotBuffer).toString('base64')}`;
            this.state = {
              status: 'scan_qr',
              qrCode: dataUrl,
              message: 'Abra o WhatsApp no celular > Aparelhos Conectados > Conectar e aponte a câmera.',
            };
          }
        }
      } catch (e) {
        // Ignora erros transitórios durante navegação do WhatsApp
      }
    }, 1500);
  }

  async cancelLogin(errorMessage?: string) {
    this.isProcessing = false;
    if (this.monitorTimer) clearInterval(this.monitorTimer);
    if (this.timeoutTimer) clearTimeout(this.timeoutTimer);
    this.monitorTimer = null;
    this.timeoutTimer = null;
    this.lastQrRaw = null;

    if (this.browser) {
      try {
        await this.browser.close();
      } catch {}
      this.browser = null;
      this.page = null;
    }

    this.state = errorMessage ? { status: 'error', error: errorMessage } : { status: 'idle' };
  }

  async logout(): Promise<void> {
    await this.cancelLogin();

    const profileDir = resolveProfileDir();
    if (existsSync(profileDir)) {
      try {
        rmSync(profileDir, { recursive: true, force: true });
      } catch (err) {
        console.warn('Aviso ao remover diretório de perfil:', err);
      }
    }

    this.state = { status: 'idle' };
  }
}

export const whatsappLoginManager = new WhatsAppLoginManager();
