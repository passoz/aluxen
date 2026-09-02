import { launchBrowser, openWhatsApp, resolveProfileDir, SessionExpiredError } from './browser';
import type { SendResult, WhatsAppStatus } from './types';
import { existsSync } from 'node:fs';
import path from 'node:path';

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
*${params.landlordName || 'Fernando Henrique Passos de Souza'}*`;
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
*${params.landlordName || 'Fernando Henrique Passos de Souza'}*`;
}

export async function checkWhatsAppSessionStatus(deep = false): Promise<WhatsAppStatus> {
  const profileDir = resolveProfileDir();
  const hasProfileDir = existsSync(profileDir);
  const hasDefaultDir = existsSync(path.join(profileDir, 'Default'));

  if (!hasProfileDir || !hasDefaultDir) {
    return { isLoggedIn: false, hasQRCode: false };
  }

  if (!deep) {
    return { isLoggedIn: true, hasQRCode: false };
  }

  let browser;
  try {
    browser = await launchBrowser({ headless: true });
    await openWhatsApp(browser, { headless: true, silent: true });
    await browser.close();
    return { isLoggedIn: true, hasQRCode: false };
  } catch (err) {
    if (browser) {
      try {
        await browser.close();
      } catch {}
    }
    if (err instanceof SessionExpiredError) {
      return { isLoggedIn: false, hasQRCode: true };
    }
    return { isLoggedIn: false, hasQRCode: false };
  }
}

export async function sendWhatsAppMessageHeadless(to: string, message: string): Promise<SendResult> {
  const cleanNumber = formatWhatsAppNumber(to);
  if (!cleanNumber || cleanNumber.length < 12) {
    throw new Error(`Número de telefone "${to}" inválido para envio via WhatsApp.`);
  }

  const browser = await launchBrowser({ headless: true });
  try {
    const page = await openWhatsApp(browser, { headless: true, silent: false });

    const url = `https://web.whatsapp.com/send?phone=${cleanNumber}&text=${encodeURIComponent(message)}`;
    console.log(`[WhatsApp] Navegando para conversa com ${cleanNumber}...`);
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });

    const sendButtonLoaded = await page
      .waitForSelector(
        'button span[data-icon="send"], button[aria-label*="Enviar" i], footer [role="textbox"]',
        { timeout: 35000 }
      )
      .catch(() => null);

    if (!sendButtonLoaded) {
      throw new Error(`Não foi possível carregar a conversa com ${cleanNumber}. Verifique se o número possui WhatsApp.`);
    }

    await Bun.sleep(1500);

    const clicked = await page.evaluate(() => {
      const sendBtn = document.querySelector(
        'button span[data-icon="send"], button[aria-label*="Enviar" i]'
      )?.closest('button');

      if (sendBtn) {
        sendBtn.click();
        return true;
      }

      const input = document.querySelector('footer div[role="textbox"]');
      if (input) {
        (input as HTMLElement).focus();
        return 'pressEnter';
      }

      return false;
    });

    if (clicked === 'pressEnter') {
      await page.keyboard.press('Enter');
    }

    await Bun.sleep(2000);

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
