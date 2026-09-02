import { launchBrowser, openWhatsApp, resolveProfileDir } from './browser';
import qrcode from 'qrcode-terminal';

export async function getQRCodeData(page: any): Promise<string | null> {
  return await page.evaluate(() => {
    const qrCanvas = document.querySelector('canvas[aria-label*="Scan" i], [data-ref], [data-testid="qrcode"]');
    if (!qrCanvas) return null;
    return qrCanvas.getAttribute('data-ref') || null;
  });
}

export async function waitForChatsLoaded(page: any, timeoutMs = 180000): Promise<void> {
  const startTime = Date.now();
  while (Date.now() - startTime < timeoutMs) {
    const isLoaded = await page.evaluate(() => {
      const pane = document.querySelector('#pane-side, [data-testid="chat-list"]');
      return !!pane;
    });

    if (isLoaded) return;
    await Bun.sleep(1000);
  }

  throw new Error('Tempo esgotado aguardando o carregamento da lista de conversas do WhatsApp.');
}

async function main() {
  console.log('\n============================================================');
  console.log('🔑 Autenticação do WhatsApp Web (Aluxen)');
  console.log('============================================================');
  console.log(`Perfil de armazenamento: ${resolveProfileDir()}`);
  console.log('Iniciando o Chromium para leitura do QR Code...\n');

  const browser = await launchBrowser({ headless: false });

  try {
    const page = await openWhatsApp(browser, { headless: false });

    console.log('-> Janela do Chromium aberta.');
    console.log('-> Abra o WhatsApp no seu celular.');
    console.log('-> Toque em Aparelhos Conectados > Conectar um aparelho.');
    console.log('-> Aponte a câmera para o QR Code na janela ou abaixo:\n');

    let qrRendered = false;
    for (let i = 0; i < 20; i++) {
      const qrData = await getQRCodeData(page);
      if (qrData && !qrRendered) {
        try {
          console.log('[QR Code no terminal]:');
          qrcode.generate(qrData, { small: true });
          qrRendered = true;
          break;
        } catch {}
      }
      await Bun.sleep(1000);
    }

    console.log('\n[*] Aguardando a sincronização das conversas...');
    await waitForChatsLoaded(page, 180000);

    console.log('\n============================================================');
    console.log('✅ WhatsApp Web conectado com sucesso ao Aluxen!');
    console.log(`As chaves e a sessão foram gravadas em: ${resolveProfileDir()}`);
    console.log('A partir de agora o Aluxen pode enviar mensagens 100% headless.');
    console.log('============================================================\n');
  } finally {
    await Bun.sleep(2000);
    await browser.close();
  }
}

if (import.meta.main) {
  main().catch((err) => {
    console.error('\n❌ Erro durante o login do WhatsApp:', err instanceof Error ? err.message : err);
    process.exit(1);
  });
}
