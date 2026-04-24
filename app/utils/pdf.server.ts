import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { PDFDocument, StandardFonts, rgb } from 'pdf-lib';

type ReceiptPdfInput = {
  receiptId: string;
  tenantName: string;
  propertyName: string;
  dueDate: string;
  paidAt: string | null;
  amount: number;
};

function formatMoney(value: number) {
  return new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  }).format(value);
}

function formatDate(value: string | null) {
  if (!value) return '-';
  return new Intl.DateTimeFormat('pt-BR', {
    dateStyle: 'medium',
    timeStyle: value.includes('T') ? 'short' : undefined,
  }).format(new Date(value));
}

export async function createReceiptPdf(input: ReceiptPdfInput) {
  const pdf = await PDFDocument.create();
  const page = pdf.addPage([595.28, 841.89]);
  const { width, height } = page.getSize();

  const fontRegular = await pdf.embedFont(StandardFonts.Helvetica);
  const fontBold = await pdf.embedFont(StandardFonts.HelveticaBold);

  const cream = rgb(0.968, 0.957, 0.91);
  const mint = rgb(0.522, 0.902, 0.753);
  const deepMint = rgb(0.42, 0.702, 0.608);
  const ink = rgb(0.043, 0.043, 0.051);
  const muted = rgb(0.33, 0.43, 0.4);

  page.drawRectangle({ x: 0, y: 0, width, height, color: cream });
  page.drawRectangle({ x: 36, y: height - 158, width: width - 72, height: 122, color: mint });
  page.drawRectangle({ x: 54, y: height - 140, width: 64, height: 64, color: deepMint });

  page.drawLine({ start: { x: 66, y: height - 103 }, end: { x: 86, y: height - 83 }, thickness: 4, color: cream });
  page.drawLine({ start: { x: 86, y: height - 83 }, end: { x: 106, y: height - 103 }, thickness: 4, color: cream });
  page.drawLine({ start: { x: 73, y: height - 103 }, end: { x: 73, y: height - 120 }, thickness: 4, color: cream });
  page.drawLine({ start: { x: 100, y: height - 103 }, end: { x: 100, y: height - 120 }, thickness: 4, color: cream });
  page.drawLine({ start: { x: 73, y: height - 120 }, end: { x: 100, y: height - 120 }, thickness: 4, color: cream });

  page.drawText('Aluxen', { x: 132, y: height - 92, size: 30, font: fontBold, color: ink });
  page.drawText('Recibo de aluguel', { x: 132, y: height - 122, size: 14, font: fontRegular, color: muted });
  page.drawText(`ID ${input.receiptId.slice(0, 8).toUpperCase()}`, { x: width - 170, y: height - 122, size: 12, font: fontBold, color: ink });

  page.drawText('Recebemos de', { x: 54, y: height - 205, size: 11, font: fontBold, color: deepMint });
  page.drawText(input.tenantName, { x: 54, y: height - 230, size: 24, font: fontBold, color: ink });
  page.drawText(`referente ao imóvel ${input.propertyName}`, { x: 54, y: height - 254, size: 13, font: fontRegular, color: muted });

  page.drawRectangle({ x: 54, y: height - 390, width: width - 108, height: 104, color: rgb(1, 1, 1), borderColor: mint, borderWidth: 1.2 });
  page.drawText('Resumo do recebimento', { x: 72, y: height - 320, size: 15, font: fontBold, color: ink });
  page.drawText('Valor', { x: 72, y: height - 352, size: 11, font: fontBold, color: muted });
  page.drawText(formatMoney(input.amount), { x: 72, y: height - 376, size: 22, font: fontBold, color: deepMint });
  page.drawText('Vencimento', { x: 260, y: height - 352, size: 11, font: fontBold, color: muted });
  page.drawText(formatDate(input.dueDate), { x: 260, y: height - 376, size: 14, font: fontBold, color: ink });
  page.drawText('Pago em', { x: 420, y: height - 352, size: 11, font: fontBold, color: muted });
  page.drawText(formatDate(input.paidAt), { x: 420, y: height - 376, size: 14, font: fontBold, color: ink });

  page.drawText('Declaração', { x: 54, y: height - 450, size: 15, font: fontBold, color: ink });
  page.drawText(
    `Declaramos, para os devidos fins, que o valor de ${formatMoney(input.amount)} foi lançado para o contrato associado a ${input.tenantName}, vinculado ao imóvel ${input.propertyName}.`,
    { x: 54, y: height - 478, size: 12, font: fontRegular, color: muted, maxWidth: width - 108, lineHeight: 18 },
  );

  page.drawLine({ start: { x: 54, y: 128 }, end: { x: width - 54, y: 128 }, thickness: 1, color: mint });
  page.drawText('Documento gerado automaticamente por Aluxen', { x: 54, y: 98, size: 11, font: fontRegular, color: muted });
  page.drawText(formatDate(new Date().toISOString()), { x: width - 150, y: 98, size: 11, font: fontBold, color: ink });

  const bytes = await pdf.save();
  const relativePath = `/receipts/${input.receiptId}.pdf`;
  const absoluteDir = path.join(process.cwd(), 'public', 'receipts');
  await mkdir(absoluteDir, { recursive: true });
  await writeFile(path.join(absoluteDir, `${input.receiptId}.pdf`), bytes);
  return relativePath;
}
