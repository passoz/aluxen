import { launchBrowser, openWhatsApp, resolveProfileDir, SessionExpiredError, isChromiumAvailable, DESKTOP_USER_AGENT } from './browser';
import type { SendResult, WhatsAppStatus } from './types';
import { existsSync } from 'node:fs';
import path from 'node:path';
export { whatsappLoginManager } from './login-manager';

export function formatWhatsAppNumber(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  if (!digits) return '';
  if (digits.startsWith('55') && digits.length >= 12) {
    return digits;
  }
  return `55${digits}`;
}

export function buildDirectWhatsAppUrl(phone: string, message: string): string {
  const clean = formatWhatsAppNumber(phone);
  if (!clean) return '';
  return `https://wa.me/${clean}?text=${encodeURIComponent(message)}`;
}

export interface ReceiptMessageParams {
  tenantName: string;
  propertyName: string;
  propertyAddress?: string;
  dueDate: string;
  amount: string;
  paidAt?: string | null;
  pdfUrl: string;
  validationUrl: string;
  landlordName?: string;
}

export function buildReceiptWhatsAppMessage(params: ReceiptMessageParams): string {
  const isPaid = !!params.paidAt;
  const statusLine = isPaid ? `✅ *Status*: Quitado em ${params.paidAt}` : '⏳ *Status*: Aguardando pagamento';

  return `Olá, *${params.tenantName}*! Tudo bem?

Segue o comprovante do seu aluguel referente ao imóvel *${params.propertyName}*:

📄 *Recibo de Aluguel*
💰 *Valor*: ${params.amount}
📅 *Vencimento*: ${params.dueDate}
${statusLine}

🔗 *Visualizar / Baixar Recibo (PDF)*:
${params.pdfUrl}

🔒 *Verificação de Autenticidade Digital*:
${params.validationUrl}

Qualquer dúvida, estou à disposição!
Atenciosamente,
*${params.landlordName || 'Locador'}*`;
}

export interface ContractMessageParams {
  tenantName: string;
  propertyName: string;
  propertyAddress?: string;
  startDate: string;
  endDate: string;
  dueDay: number;
  rentAmount: string;
  pdfUrl: string;
  validationUrl: string;
  landlordName?: string;
}

export function buildContractWhatsAppMessage(params: ContractMessageParams): string {
  return `Olá, *${params.tenantName}*! Tudo bem?

Segue o seu contrato de locação residencial referente ao imóvel *${params.propertyName}*:

📄 *Contrato de Locação Residencial*
💰 *Valor do Aluguel*: ${params.rentAmount}
📅 *Vencimento Mensal*: Todo dia ${params.dueDay}
🗓️ *Vigência*: ${params.startDate} a ${params.endDate}

🔗 *Visualizar / Baixar Contrato (PDF)*:
${params.pdfUrl}

🔒 *Verificação de Autenticidade Digital*:
${params.validationUrl}

Qualquer dúvida, estou à disposição!
Atenciosamente,
*${params.landlordName || 'Locador'}*`;
}

export async function checkWhatsAppSessionStatus(deep = false): Promise<WhatsAppStatus> {
  const profileDir = resolveProfileDir();
  const hasProfileDir = existsSync(profileDir);
  const hasDefaultDir = existsSync(path.join(profileDir, 'Default'));
  const chromiumAvailable = isChromiumAvailable();

  if (!hasProfileDir || !hasDefaultDir) {
    return { isLoggedIn: false, hasQRCode: false, chromiumAvailable, profileDir };
  }

  if (!deep) {
    return { isLoggedIn: true, hasQRCode: false, chromiumAvailable, profileDir };
  }

  let browser;
  try {
    browser = await launchBrowser({ headless: true });
    await openWhatsApp(browser, { headless: true, silent: true });
    await browser.close();
    return { isLoggedIn: true, hasQRCode: false, chromiumAvailable, profileDir };
  } catch (err) {
    if (browser) {
      try {
        await browser.close();
      } catch {}
    }
    if (err instanceof SessionExpiredError) {
      return { isLoggedIn: false, hasQRCode: true, chromiumAvailable, profileDir };
    }
    return { isLoggedIn: false, hasQRCode: false, chromiumAvailable, profileDir };
  }
}

export async function sendWhatsAppMessageHeadless(to: string, message: string): Promise<SendResult> {
  const cleanNumber = formatWhatsAppNumber(to);
  if (!cleanNumber || cleanNumber.length < 12) {
    throw new Error(`Número de telefone "${to}" inválido para envio via WhatsApp.`);
  }

  const browser = await launchBrowser({ headless: true });
  try {
    const pages = await browser.pages();
    const page = pages[0] || (await browser.newPage());
    await page.setUserAgent(DESKTOP_USER_AGENT);

    const url = `https://web.whatsapp.com/send?phone=${cleanNumber}&text=${encodeURIComponent(message)}`;
    console.log(`[WhatsApp] Navegando para conversa com ${cleanNumber}...`);

    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });

    // 1. Aguardar o carregamento da conversa e do botão de envio (ou alerta de número inválido)
    const sendBtnSelector =
      'button[aria-label="Enviar"], button span[data-icon="wds-ic-send-filled"], button span[data-icon="send"]';

    await page.waitForFunction(
      (btnSel) => {
        const dialog = document.querySelector('[role="dialog"]');
        if (
          dialog &&
          ((dialog as HTMLElement).innerText.includes('inválido') ||
            (dialog as HTMLElement).innerText.includes('invalid'))
        ) {
          return true;
        }
        const sendBtn = document.querySelector(btnSel)?.closest('button');
        return sendBtn && !sendBtn.disabled;
      },
      { timeout: 45000 },
      sendBtnSelector
    );

    // 2. Verificar se o número de telefone é inválido
    const isInvalid = await page.evaluate(() => {
      const dialog = document.querySelector('[role="dialog"]');
      return (
        dialog &&
        ((dialog as HTMLElement).innerText.includes('inválido') ||
          (dialog as HTMLElement).innerText.includes('invalid'))
      );
    });

    if (isInvalid) {
      throw new Error(`O número "${to}" não está registrado no WhatsApp ou o formato é inválido.`);
    }

    await Bun.sleep(1000);

    // 3. Clicar no botão Enviar real
    const sent = await page.evaluate((btnSel) => {
      const sendBtn = document.querySelector(btnSel)?.closest('button');
      if (sendBtn) {
        sendBtn.click();
        return true;
      }
      return false;
    }, sendBtnSelector);

    if (!sent) {
      // Fallback: focar no textbox e pressionar Enter
      const input = await page.$('footer [contenteditable="true"][role="textbox"]');
      if (input) {
        await input.focus();
        await page.keyboard.press('Enter');
      } else {
        throw new Error('Não foi possível localizar o botão de envio no WhatsApp Web.');
      }
    }

    console.log('[WhatsApp] Mensagem disparada. Aguardando transmissão pelo WebSocket...');

    // 4. Aguardar a confirmação de envio (relógio pendente desaparece e vira check)
    // Dá até 15 segundos para o WhatsApp Web confirmar a transmissão aos servidores
    for (let i = 0; i < 15; i++) {
      await Bun.sleep(1000);
      const isStillPending = await page.evaluate(() => {
        const pending = document.querySelector('span[data-icon="msg-time"], [aria-label*="Pendente" i]');
        return !!pending;
      });

      if (!isStillPending) {
        console.log(`[WhatsApp] Mensagem transmitida com sucesso aos servidores em ${i + 1}s.`);
        break;
      }
    }

    // Intervalo de segurança extra para flush do socket antes de fechar o navegador
    await Bun.sleep(3000);

    return {
      success: true,
      to: cleanNumber,
      message,
      timestamp: new Date().toISOString(),
    };
  } finally {
    await browser.close();
  }
}
