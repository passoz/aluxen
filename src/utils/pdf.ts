import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { PDFDocument, StandardFonts, rgb } from 'pdf-lib';
import { getSetting } from '../db';

const PDF_TIME_ZONE = 'America/Sao_Paulo';

export type ReceiptPdfInput = {
  receiptId: string;
  tenantName: string;
  propertyName: string;
  propertyAddress?: string;
  dueDate: string;
  paidAt: string | null;
  amount: number;
  signature?: boolean;
};

type FontLike = {
  widthOfTextAtSize(text: string, size: number): number;
};

function formatMoney(value: number) {
  return new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  }).format(value);
}

function formatDate(value: string | null) {
  if (!value) return '-';
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const [year, month, day] = value.split('-').map(Number);
    return new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium' }).format(new Date(year ?? 2000, (month ?? 1) - 1, day ?? 1));
  }
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) {
    const [datePart = '', timePart = '00:00'] = value.split('T');
    const [year, month, day] = datePart.split('-').map(Number);
    const [hour, minute] = timePart.split(':').map(Number);
    return new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium', timeStyle: 'short' }).format(
      new Date(year ?? 2000, (month ?? 1) - 1, day ?? 1, hour ?? 0, minute ?? 0)
    );
  }
  return new Intl.DateTimeFormat('pt-BR', {
    dateStyle: 'medium',
    timeStyle: value.includes('T') ? 'short' : undefined,
    timeZone: value.includes('T') ? PDF_TIME_ZONE : undefined,
  }).format(new Date(value));
}

function formatReceiptIssuedAt(value: Date) {
  const date = value;
  const day = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', timeZone: PDF_TIME_ZONE }).format(date);
  const month = new Intl.DateTimeFormat('pt-BR', { month: 'long', timeZone: PDF_TIME_ZONE }).format(date);
  const year = new Intl.DateTimeFormat('pt-BR', { year: 'numeric', timeZone: PDF_TIME_ZONE }).format(date);
  const time = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit', timeZone: PDF_TIME_ZONE }).format(date);
  return `${day} de ${month} de ${year}, ${time}`;
}

function numberToPortuguese(value: number): string {
  const units = ['zero', 'um', 'dois', 'três', 'quatro', 'cinco', 'seis', 'sete', 'oito', 'nove'];
  const teens = ['dez', 'onze', 'doze', 'treze', 'quatorze', 'quinze', 'dezesseis', 'dezessete', 'dezoito', 'dezenove'];
  const tens = ['', '', 'vinte', 'trinta', 'quarenta', 'cinquenta', 'sessenta', 'setenta', 'oitenta', 'noventa'];
  const hundreds = ['', 'cento', 'duzentos', 'trezentos', 'quatrocentos', 'quinhentos', 'seiscentos', 'setecentos', 'oitocentos', 'novecentos'];

  function underThousand(n: number): string {
    if (n === 0) return '';
    if (n < 10) return units[n] ?? '';
    if (n < 20) return teens[n - 10] ?? '';
    if (n < 100) {
      const ten = Math.floor(n / 10);
      const rest = n % 10;
      return rest ? `${tens[ten]} e ${underThousand(rest)}` : (tens[ten] ?? '');
    }
    if (n === 100) return 'cem';
    const hundred = Math.floor(n / 100);
    const rest = n % 100;
    return rest ? `${hundreds[hundred]} e ${underThousand(rest)}` : (hundreds[hundred] ?? '');
  }

  if (value === 0) return 'zero';

  const billions = Math.floor(value / 1_000_000_000);
  const millions = Math.floor((value % 1_000_000_000) / 1_000_000);
  const thousands = Math.floor((value % 1_000_000) / 1_000);
  const remainder = value % 1_000;
  const parts: string[] = [];

  if (billions) parts.push(`${billions === 1 ? 'um bilhão' : `${numberToPortuguese(billions)} bilhões`}`);
  if (millions) parts.push(`${millions === 1 ? 'um milhão' : `${numberToPortuguese(millions)} milhões`}`);
  if (thousands) parts.push(thousands === 1 ? 'mil' : `${numberToPortuguese(thousands)} mil`);
  if (remainder) parts.push(underThousand(remainder));

  if (parts.length === 1) return parts[0] ?? '';
  const last = parts.pop();
  return `${parts.join(', ')} e ${last}`;
}

function moneyToPortuguese(value: number): string {
  const whole = Math.floor(value);
  const cents = Math.round((value - whole) * 100);
  const wholeText = `${numberToPortuguese(whole)} ${whole === 1 ? 'real' : 'reais'}`;
  if (!cents) return wholeText;
  const centsText = `${numberToPortuguese(cents)} ${cents === 1 ? 'centavo' : 'centavos'}`;
  return `${wholeText} e ${centsText}`;
}

function wrapText(text: string, font: FontLike, size: number, maxWidth: number): string[] {
  const words = text.trim().split(/\s+/).filter(Boolean);
  const lines: string[] = [];
  let currentLine = '';

  for (const word of words) {
    const candidate = currentLine ? `${currentLine} ${word}` : word;
    if (font.widthOfTextAtSize(candidate, size) <= maxWidth) {
      currentLine = candidate;
      continue;
    }

    if (currentLine) {
      lines.push(currentLine);
      currentLine = word;
      continue;
    }

    let slice = '';
    for (const char of word) {
      const nextSlice = `${slice}${char}`;
      if (font.widthOfTextAtSize(nextSlice, size) <= maxWidth || !slice) {
        slice = nextSlice;
      } else {
        lines.push(slice);
        slice = char;
      }
    }
    currentLine = slice;
  }

  if (currentLine) lines.push(currentLine);
  return lines;
}

function drawWrappedText(
  page: any,
  text: string,
  options: { x: number; y: number; maxWidth: number; lineHeight: number; size: number; font: any; color: any }
): number {
  const lines = wrapText(text, options.font, options.size, options.maxWidth);
  lines.forEach((line, index) => {
    page.drawText(line, {
      x: options.x,
      y: options.y - index * options.lineHeight,
      size: options.size,
      font: options.font,
      color: options.color,
    });
  });
  return lines.length;
}

export async function createReceiptPdfBuffer(input: ReceiptPdfInput): Promise<Uint8Array> {
  const pdf = await PDFDocument.create();
  const page = pdf.addPage([595.28, 841.89]);
  const { width, height } = page.getSize();
  const issuedAt = new Date();

  const fontRegular = await pdf.embedFont(StandardFonts.Helvetica);
  const fontBold = await pdf.embedFont(StandardFonts.HelveticaBold);

  const showSignature = input.signature !== false;

  const pageBg = rgb(0.985, 0.985, 0.98);
  const panelBg = rgb(1, 1, 1);
  const accent = rgb(0.21, 0.55, 0.48);
  const accentSoft = rgb(0.88, 0.95, 0.92);
  const ink = rgb(0.1, 0.12, 0.14);
  const muted = rgb(0.39, 0.43, 0.46);
  const border = rgb(0.83, 0.88, 0.86);

  const margin = 48;
  const contentWidth = width - margin * 2;
  const cardX = margin;

  page.drawRectangle({ x: 0, y: 0, width, height, color: pageBg });
  page.drawRectangle({ x: cardX, y: height - 140, width: contentWidth, height: 92, color: panelBg, borderColor: border, borderWidth: 1 });
  page.drawRectangle({ x: cardX, y: height - 140, width: contentWidth, height: 6, color: accent });

  page.drawText('Recibo de aluguel', { x: cardX + 20, y: height - 92, size: 24, font: fontBold, color: ink });
  page.drawText(`Recibo ${input.receiptId.slice(0, 8).toUpperCase()}`, { x: cardX + 20, y: height - 116, size: 11, font: fontRegular, color: muted });

  let cursorY = height - 180;
  page.drawText('Recebido de', { x: cardX, y: cursorY, size: 11, font: fontBold, color: accent });
  cursorY -= 24;
  const tenantLines = drawWrappedText(page, input.tenantName, {
    x: cardX,
    y: cursorY,
    maxWidth: contentWidth,
    lineHeight: 24,
    size: 22,
    font: fontBold,
    color: ink,
  });
  cursorY -= tenantLines * 24 + 8;
  const propertyLines = drawWrappedText(page, `Imovel: ${input.propertyName}`, {
    x: cardX,
    y: cursorY,
    maxWidth: contentWidth,
    lineHeight: 18,
    size: 12,
    font: fontRegular,
    color: muted,
  });
  cursorY -= propertyLines * 18 + 28;

  const summaryHeight = 122;
  page.drawRectangle({ x: cardX, y: cursorY - summaryHeight, width: contentWidth, height: summaryHeight, color: panelBg, borderColor: border, borderWidth: 1 });
  page.drawRectangle({ x: cardX + 18, y: cursorY - 38, width: contentWidth - 36, height: 24, color: accentSoft });
  page.drawText('Resumo do recebimento', { x: cardX + 28, y: cursorY - 30, size: 12, font: fontBold, color: accent });

  const col1 = cardX + 24;
  const col2 = cardX + 210;
  const col3 = cardX + 372;
  page.drawText('Valor', { x: col1, y: cursorY - 64, size: 10, font: fontBold, color: muted });
  page.drawText(formatMoney(input.amount), { x: col1, y: cursorY - 90, size: 20, font: fontBold, color: accent });
  page.drawText('Vencimento', { x: col2, y: cursorY - 64, size: 10, font: fontBold, color: muted });
  drawWrappedText(page, formatDate(input.dueDate), { x: col2, y: cursorY - 82, maxWidth: 130, lineHeight: 15, size: 12, font: fontBold, color: ink });
  page.drawText('Pago em', { x: col3, y: cursorY - 64, size: 10, font: fontBold, color: muted });
  drawWrappedText(page, formatDate(input.paidAt), { x: col3, y: cursorY - 82, maxWidth: 120, lineHeight: 15, size: 12, font: fontBold, color: ink });

  cursorY -= summaryHeight + 34;
  page.drawText('Declaração', { x: cardX, y: cursorY, size: 14, font: fontBold, color: ink });
  cursorY -= 26;
  const addressSuffix = input.propertyAddress ? `, na ${input.propertyAddress}` : '';
  const declarationLines = drawWrappedText(
    page,
    `Declaro, para os devidos fins, o recebimento do valor de ${formatMoney(input.amount)} (${moneyToPortuguese(input.amount)}) referente ao aluguel do imóvel ${input.propertyName}${addressSuffix}, vinculado ao locatário ${input.tenantName}.`,
    {
      x: cardX,
      y: cursorY,
      maxWidth: contentWidth,
      lineHeight: 18,
      size: 12,
      font: fontRegular,
      color: muted,
    }
  );

  cursorY -= declarationLines * 18 + 42;
  page.drawText(`Cabo Frio, ${formatReceiptIssuedAt(issuedAt)}`, { x: cardX, y: cursorY, size: 12, font: fontRegular, color: ink });

  let signatureY: number;

  if (showSignature) {
    const signaturePath = path.join(process.cwd(), 'public', 'assets', 'assinatura01.png');
    try {
      const signatureBytes = await readFile(signaturePath);
      const signatureImage = await pdf.embedPng(signatureBytes);

      const signatureMaxWidth = 180;
      const signatureScale = signatureMaxWidth / signatureImage.width;
      const signatureWidth = signatureImage.width * signatureScale;
      const signatureHeight = signatureImage.height * signatureScale;
      signatureY = cursorY - signatureHeight - 20;
      const signatureX = cardX + (contentWidth - signatureWidth) / 2;
      page.drawImage(signatureImage, {
        x: signatureX,
        y: signatureY,
        width: signatureWidth,
        height: signatureHeight,
      });
    } catch {
      signatureY = cursorY - 20;
    }
  } else {
    signatureY = cursorY - 20;
  }

  const signatureLineWidth = 220;
  const signatureLineX = cardX + (contentWidth - signatureLineWidth) / 2;
  const signerName = (await getSetting('landlord_name')) || 'Fernando Passos';
  const signerCpfRaw = (await getSetting('landlord_cpf')) || '075.898.017-55';
  const signerCpf = `CPF ${signerCpfRaw}`;
  const signerNameWidth = fontBold.widthOfTextAtSize(signerName, 11);
  const signerCpfWidth = fontRegular.widthOfTextAtSize(signerCpf, 10);
  page.drawLine({ start: { x: signatureLineX, y: signatureY - 10 }, end: { x: signatureLineX + signatureLineWidth, y: signatureY - 10 }, thickness: 1, color: border });
  page.drawText(signerName, { x: cardX + (contentWidth - signerNameWidth) / 2, y: signatureY - 26, size: 11, font: fontBold, color: ink });
  page.drawText(signerCpf, { x: cardX + (contentWidth - signerCpfWidth) / 2, y: signatureY - 42, size: 10, font: fontRegular, color: muted });

  page.drawLine({ start: { x: cardX, y: 92 }, end: { x: cardX + contentWidth, y: 92 }, thickness: 1, color: border });
  page.drawText('Documento gerado automaticamente.', { x: cardX, y: 68, size: 10, font: fontRegular, color: muted });

  return await pdf.save();
}

export async function createReceiptPdf(input: ReceiptPdfInput): Promise<string> {
  const bytes = await createReceiptPdfBuffer(input);
  const relativePath = `/receipts/${input.receiptId}.pdf`;
  const absoluteDir = path.join(process.cwd(), 'public', 'receipts');
  await mkdir(absoluteDir, { recursive: true });
  await writeFile(path.join(absoluteDir, `${input.receiptId}.pdf`), bytes);
  return relativePath;
}
