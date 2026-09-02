import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { PDFDocument, StandardFonts, rgb } from 'pdf-lib';
import { getSetting } from '../db';

export type ContractPdfInput = {
  leaseId: string;
  tenantName: string;
  tenantPhone: string;
  tenantCpf: string;
  propertyName: string;
  propertyAddress: string;
  propertyCity: string;
  propertyState: string;
  propertyZip: string;
  startDate: string;
  endDate: string;
  dueDay: number;
  rentAmount: number;
};

function formatMoney(value: number): string {
  return new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  }).format(value);
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

function wrapText(text: string, font: any, size: number, maxWidth: number): string[] {
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

function formatDueDay(dueDay: number): string {
  const days = [
    'primeiro', 'dois', 'três', 'quatro', 'cinco', 'seis', 'sete', 'oito', 'nove', 'dez',
    'onze', 'doze', 'treze', 'quatorze', 'quinze', 'dezesseis', 'dezessete', 'dezoito', 'dezenove',
    'vinte', 'vinte e um', 'vinte e dois', 'vinte e três', 'vinte e quatro', 'vinte e cinco',
    'vinte e seis', 'vinte e sete', 'vinte e oito', 'vinte e nove', 'trinta', 'trinta e um',
  ];
  return days[dueDay - 1] || String(dueDay);
}

function capitalize(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1).toLowerCase();
}

function drawLine(page: any, x: number, y: number, width: number, color: any) {
  page.drawLine({
    start: { x, y },
    end: { x: x + width, y },
    thickness: 1,
    color,
  });
}

export async function createContractPdfBuffer(input: ContractPdfInput): Promise<Uint8Array> {
  const pdf = await PDFDocument.create();
  let currentPage = pdf.addPage([595.28, 841.89]);
  const { width, height } = currentPage.getSize();

  const fontRegular = await pdf.embedFont(StandardFonts.Helvetica);
  const fontBold = await pdf.embedFont(StandardFonts.HelveticaBold);

  const ink = rgb(0.1, 0.12, 0.14);
  const accent = rgb(0.13, 0.13, 0.13);
  const muted = rgb(0.45, 0.45, 0.45);

  const margin = 50;
  const contentWidth = width - margin * 2;

  const rentExt = moneyToPortuguese(input.rentAmount);
  const dueDayExt = formatDueDay(input.dueDay);
  const rentStr = formatMoney(input.rentAmount);
  const startParts = input.startDate.split('-').map(Number);
  const endParts = input.endDate.split('-').map(Number);

  let cursorY = height - margin;

  function ensureSpace(needed: number) {
    if (cursorY - needed >= 60) return;
    currentPage = pdf.addPage([595.28, 841.89]);
    cursorY = height - margin;
  }

  function sectionTitle(text: string) {
    ensureSpace(48);
    cursorY -= 8;
    currentPage.drawText(text, { x: margin, y: cursorY, size: 11.5, font: fontBold, color: accent });
    cursorY -= 18;
  }

  function bodyText(text: string, opts?: { indent?: boolean; size?: number }) {
    const size = opts?.size || 10.5;
    const lines = wrapText(text, fontRegular, size, contentWidth - (opts?.indent ? 20 : 0));
    const lineH = size * 1.5;
    ensureSpace(lines.length * lineH + 14);
    if (opts?.indent) {
      const firstLine = lines[0] ?? '';
      currentPage.drawText(`     ${firstLine}`, {
        x: margin,
        y: cursorY,
        size,
        font: fontRegular,
        color: ink,
      });
      for (let i = 1; i < lines.length; i++) {
        currentPage.drawText(lines[i] ?? '', {
          x: margin,
          y: cursorY - i * lineH,
          size,
          font: fontRegular,
          color: ink,
        });
      }
    } else {
      lines.forEach((line, i) => {
        currentPage.drawText(line, {
          x: margin,
          y: cursorY - i * lineH,
          size,
          font: fontRegular,
          color: ink,
        });
      });
    }
    cursorY -= lines.length * lineH + 10;
  }

  function bodyLabel(label: string, value: string) {
    ensureSpace(20);
    const labelWidth = fontBold.widthOfTextAtSize(label, 10.5);
    currentPage.drawText(label, { x: margin, y: cursorY, size: 10.5, font: fontBold, color: ink });
    currentPage.drawText(value, { x: margin + labelWidth + 4, y: cursorY, size: 10.5, font: fontRegular, color: ink });
    cursorY -= 16;
  }

  const city = capitalize(input.propertyCity);
  const state = input.propertyState.toUpperCase();
  const today = new Date();
  const contractDate = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'long' }).format(today);

  const title = 'CONTRATO DE LOCAÇÃO RESIDENCIAL';
  const titleWidth = fontBold.widthOfTextAtSize(title, 15);
  currentPage.drawText(title, {
    x: (width - titleWidth) / 2,
    y: cursorY,
    size: 15,
    font: fontBold,
    color: ink,
  });
  cursorY -= 14;
  drawLine(currentPage, margin, cursorY, contentWidth, muted);
  cursorY -= 26;

  sectionTitle('IDENTIFICAÇÃO DAS PARTES CONTRATANTES');

  bodyText('LOCADOR:', { size: 10.5 });
  const landlordName = (await getSetting('landlord_name')) || 'Fernando Henrique Passos de Souza';
  const landlordCpf = (await getSetting('landlord_cpf')) || '075.898.017-55';
  bodyLabel('Nome:', landlordName);
  cursorY -= 20;
  bodyLabel('CPF:', landlordCpf);
  bodyText(`Endereço: Rua Alemanha, nº 132, Bairro Jardim Caiçara, ${city} - ${state}, CEP 28910-370`);
  cursorY -= 6;

  bodyText('LOCATÁRIO:', { size: 10.5 });
  bodyLabel('Nome:', input.tenantName);
  bodyLabel('CPF:', input.tenantCpf);
  bodyLabel('Telefone:', input.tenantPhone);
  cursorY -= 10;

  bodyText(
    `As partes acima identificadas têm, entre si, justo e acertado o presente Contrato de Locação Residencial, que se regerá pelas cláusulas seguintes e pelas condições de preço, forma e termo de pagamento descritas no presente.`
  );
  cursorY -= 4;

  sectionTitle('Cláusula 1ª - DO OBJETO');
  bodyText(
    `O presente contrato tem como OBJETO o imóvel de propriedade do LOCADOR, situado na ${input.propertyAddress}, ${input.propertyName}, bairro Jardim Caiçara, cidade ${city}, CEP 28910-370, no Estado do Rio de Janeiro.`
  );
  bodyText(
    `Parágrafo único: O imóvel entregue na data da assinatura deste contrato, pelo LOCADOR ao LOCATÁRIO, possui as características contidas no auto de vistoria anexo, que desde já aceitam expressamente.`
  );

  sectionTitle('Cláusula 2ª - DA DESTINAÇÃO');
  bodyText(
    `A presente LOCAÇÃO destina-se restritivamente ao uso do imóvel para fins residenciais, restando proibido ao LOCATÁRIO, sublocá-lo ou usá-lo de forma diferente do previsto, salvo autorização expressa do LOCADOR.`
  );

  sectionTitle('Cláusula 3ª - DO ESTADO DO IMÓVEL');
  bodyText(
    `O imóvel objeto deste contrato será entregue nas condições descritas no auto de vistoria, ou seja, com instalações elétricas e hidráulicas em perfeito funcionamento, com todos os cômodos e paredes pintados, sendo que portas, portões e acessórios se encontram também em funcionamento correto, devendo o LOCATÁRIO mantê-lo desta forma. Fica também acordado, que o imóvel será devolvido nas mesmas condições previstas no auto de vistoria, além de, no ato da entrega das chaves, com todas as despesas pagas.`
  );

  sectionTitle('Cláusula 4ª - DAS BENFEITORIAS');
  bodyText(
    `Qualquer benfeitoria ou construção que seja destinada ao imóvel objeto deste, deverá de imediato, ser submetida a autorização expressa do LOCADOR. Vindo a ser feita a benfeitoria, faculta ao LOCADOR aceitá-la ou não, restando ao LOCATÁRIO em caso do LOCADOR não aceitá-la, modificar o imóvel da maneira que lhe foi entregue. As benfeitorias, consertos ou reparos farão parte integrante do imóvel, não assistindo ao LOCATÁRIO o direito de retenção ou indenização sobre a mesma, com exceção das benfeitorias necessárias, na forma da lei.`
  );

  sectionTitle('Cláusula 5ª - DA RESTITUIÇÃO');
  bodyText(
    `O LOCATÁRIO restituirá o imóvel locado nas mesmas condições as quais o recebeu, quais sejam, pintado com tinta na cor contida no auto de vistoria, sendo que as instalações elétricas, hidráulicas e acessórios deverão também, estar em perfeitas condições de funcionamento, salvo as deterioração decorrentes do uso normal e habitual do imóvel.`
  );
  bodyText(`Parágrafo único: Os autos de vistoria inicial e final, que farão parte deste contrato conterão assinatura de duas testemunhas e dos contratantes.`);

  sectionTitle('Cláusula 6ª - DA VENDA DO IMÓVEL');
  bodyText(
    `Caso o LOCADOR manifeste vontade de vender o imóvel objeto do presente, deverá propor por escrito ao LOCATÁRIO, que se obrigará a emitir a resposta em 30 (trinta) dias, a partir da comunicação inicial, sendo garantido ao LOCATÁRIO as mesmas condições de compra que serão ofertadas aos outros possíveis compradores.`
  );

  sectionTitle('Cláusula 7ª - DAS VISTORIAS');
  bodyText(
    `O LOCATÁRIO permitirá ao LOCADOR, realizar vistorias no imóvel em dia e hora a serem combinados, podendo este último averiguar o funcionamento de todas as instalações e acessórios. Constatando-se algum vício que possa afetar a estrutura física do imóvel, desde que não seja a parte da estrutura em concreto, e desde que este vício seja por falta de manutenção a que está obrigado por lei o LOCATÁRIO, este ficará compelido a realizar o conserto, no prazo de 15 dias, observado o direito de retenção e/ou indenização das benfeitorias necessárias, na forma da lei. Não ocorrendo o conserto, o LOCADOR ficará facultado a RESCINDIR O CONTRATO no prazo de 30 (trinta) dias, sem prejuízo dos numerários previstos neste.`
  );

  sectionTitle('Cláusula 8ª - DOS ACIDENTES');
  bodyText(
    `Qualquer acidente que porventura venha a ocorrer no imóvel por culpa ou dolo do LOCATÁRIO, o mesmo ficará obrigado a pagar, além da multa prevista na Cláusula 15ª, todas as despesas por danos causados ao imóvel, devendo restituí-lo no estado que o encontrou e que, sobretudo, teve conhecimento no auto de vistoria.`
  );

  sectionTitle('Cláusula 9ª - DO ALUGUEL');
  bodyText(
    `Como aluguel mensal, o LOCATÁRIO se obrigará a pagar o valor de ${rentStr} (${rentExt}), a ser efetuado diretamente ao LOCADOR, devendo fazê-lo até o dia ${dueDayExt} do mês vincendo, sob pena de multa, correções e despesas previstas nas Cláusulas 14ª e 15ª.`
  );

  sectionTitle('Cláusula 10ª - DO RECIBO');
  bodyText(
    `Fica obrigado o LOCADOR ou seu procurador, a emitir recibo da quantia paga, no ato do pagamento, relacionando pormenorizadamente todos os valores oriundos de juros, ou outra despesa. Caso o LOCATÁRIO venha a efetuar o pagamento do aluguel através de cheque, o LOCADOR emitirá o recibo de pagamento mencionado o número do cheque, número do banco e o emitente do mesmo, que será dado quitação somente após compensação do mesmo.`
  );

  sectionTitle('Cláusula 11ª - DO REAJUSTE');
  bodyText(
    `O valor do aluguel será reajustado anualmente, tendo como base, os índices previstos e acumulados no período anual do IGP-M, em caso de falta deste índice, o reajustamento do aluguel terá por base a média da variação dos índices inflacionários do ano corrente ao da execução do aluguel, até o primeiro dia anterior ao pagamento de todos os valores devidos.`
  );

  sectionTitle('Cláusula 12ª - DA COBRANÇA');
  bodyText(`Faculta ao LOCADOR ou ao seu procurador, cobrar do LOCATÁRIO, o(s) aluguel(éis) e despesa(s) vencido(s), oriundo(s) deste contrato, utilizando-se de todos os meios legais admitidos.`);
  bodyText(`Parágrafo único: O(s) cheque(s) utilizado(s) em pagamento, se não compensado(s) até o quinto dia útil contados a partir do vencimento do aluguel, ocasionará(ão) mora do LOCATÁRIO, facultando ao LOCADOR a aplicação do disposto na Cláusula 15ª.`);

  sectionTitle('Cláusula 13ª - DAS DESPESAS');
  bodyText(
    `As despesas diretamente ligadas à conservação do imóvel, quais sejam: água, ressalvado o disposto no parágrafo único da cláusula nona, para que não haja duplo pagamento; luz, gás e telefone e o valor único anual estipulado na cláusula nona parágrafo segundo, que estejam relacionadas ao uso do mesmo, ficarão sob a responsabilidade do LOCATÁRIO, ressalvando-se quanto à contribuição de melhoria, que é de responsabilidade do LOCADOR. Os demais tributos e taxas incidentes sobre o imóvel são de responsabilidade do LOCADOR.`
  );

  sectionTitle('Cláusula 14ª - DA MORA');
  bodyText(
    `O LOCATÁRIO, não vindo a efetuar o pagamento do aluguel até a data estipulada na Cláusula 10ª, fica obrigado a pagar multa de 2% (dois por cento) sobre o valor do aluguel estipulado neste contrato, bem como juros de mora de 1% (um por cento) ao mês, mais correção monetária.`
  );

  sectionTitle('Cláusula 15ª - DO ATRASO');
  bodyText(
    `Em caso de atraso no pagamento dos aluguéis e não compensando o cheque destinado para tal fim, restará em mora o LOCATÁRIO, ficando responsabilizado por todos os pagamentos previstos neste atraso, sem prejuízo do pagamento da multa, juros de mora e correção monetária. Não configurarão novação ou adição às cláusulas contidas no presente instrumento, os atos de mera tolerância referentes ao atraso no pagamento do aluguel.`
  );

  sectionTitle('Cláusula 16ª - DA TOLERÂNCIA');
  bodyText(
    `O LOCATÁRIO terá um prazo de tolerância para efetuar o pagamento do aluguel até o 5º (quinto) dia útil após o vencimento, caso não seja dia útil, ficará obrigado desde já a efetuar o pagamento no primeiro dia útil subsequente a esta data.`
  );

  sectionTitle('Cláusula 17ª - DA MULTA CONTRATUAL');
  bodyText(
    `As partes estipulam o pagamento da multa no valor de 3 (três) aluguéis vigentes a época da ocorrência do fato, a ser aplicada àquele que venha a infringir quaisquer das cláusulas contidas neste contrato exceto quando da ocorrência das hipóteses previstas nas Cláusulas 15ª e 18ª, sendo dispensado do pagamento da multa no caso do contrato tornar-se por prazo indeterminado.`
  );

  sectionTitle('Cláusula 18ª - DA RESCISÃO');
  bodyText(
    `Ocorrerá a rescisão do presente contrato, independente de qualquer comunicação prévia ou indenização por parte do LOCADOR, garantindo-se ao LOCATÁRIO o prazo de trinta dias para desocupar o imóvel, quando:`
  );
  bodyText(
    `a) Ocorrendo qualquer sinistro, incêndio ou algo que venha a impossibilitar a posse do imóvel, independente de dolo ou culpa do LOCATÁRIO; bem como quaisquer outras hipóteses que maculem o imóvel de vício e impossibilite sua posse;`
  );
  bodyText(`b) Em hipótese de desapropriação do imóvel alugado ou manifestação do poder público.`);

  sectionTitle('Cláusula 19ª - DO USO DIVERSO');
  bodyText(
    `Caso o imóvel seja utilizado de forma diversa da locação residencial, restará facultado ao LOCADOR, rescindir o presente contrato de plano, sem gerar direito a indenização ou qualquer ônus por parte deste último. Sem prejuízo da obrigação do LOCATÁRIO de efetuar o pagamento das multas e despesas previstas na Cláusula 15ª. Salvo autorização expressa do LOCADOR.`
  );

  sectionTitle('Cláusula 20ª - DA RESCISÃO PELO LOCATÁRIO');
  bodyText(
    `Caso o LOCATÁRIO deseje rescindir o presente contrato, deverá notificar ao LOCADOR, com antecedência mínima de 30 (trinta) dias, garantindo-se a incidência da cláusula 17ª.`
  );

  sectionTitle('Cláusula 21ª - DO PRAZO');
  const startYear = startParts[0] ?? 2026;
  const startMonth = (startParts[1] ?? 1) - 1;
  const startDay = startParts[2] ?? 1;
  const endYear = endParts[0] ?? 2027;
  const endMonth = (endParts[1] ?? 1) - 1;
  const endDay = endParts[2] ?? 1;

  bodyText(
    `A presente locação terá o lapso temporal de validade de 12 meses, a iniciar-se no dia ${startDay}, do mês ${new Intl.DateTimeFormat('pt-BR', { month: 'long' }).format(new Date(startYear, startMonth))} no ano de ${startYear} e findar-se no dia ${endDay} do mês ${new Intl.DateTimeFormat('pt-BR', { month: 'long' }).format(new Date(endYear, endMonth))} no ano de ${endYear}, data a qual o imóvel deverá ser devolvido nas condições previstas na Cláusula 5ª, efetivando-se com a entrega das chaves, independentemente de aviso ou qualquer outra medida judicial ou extrajudicial.`
  );

  sectionTitle('Cláusula 22ª - DA PRORROGAÇÃO');
  bodyText(
    `Ultrapassando o contrato a data prevista, ou seja, tornando-se contrato por tempo indeterminado, poderá o LOCADOR rescindi-lo a qualquer tempo, desde que ocorra notificação por escrito ao LOCATÁRIO, que ficará compelido a sair do imóvel dentro do prazo de 30 (trinta) dias, a contar do recebimento da notificação, facultando-se ao LOCATÁRIO o mesmo direito. Ocorrendo prorrogação, o LOCATÁRIO e o LOCADOR ficarão obrigados por todo o teor deste contrato.`
  );

  sectionTitle('Cláusula 23ª - DO REGISTRO');
  bodyText(`Este contrato deve ser registrado no Cartório de Registro Imobiliário, ficando sob a responsabilidade do LOCADOR a efetivação desta medida.`);

  sectionTitle('Cláusula 24ª - DA CAUÇÃO');
  bodyText(`As partes mutuamente dispensam quaisquer tipos de caução, tais como fiador e avalistas.`);

  sectionTitle('Cláusula 25ª - DO FORO');
  bodyText(
    `O presente contrato passa a vigorar entre as partes a partir da assinatura do mesmo, as quais elegem o foro da cidade de ${city}, onde se situa o imóvel, para dirimirem quaisquer dúvidas provenientes da execução e cumprimento do mesmo.`
  );

  ensureSpace(220);
  cursorY -= 16;
  bodyText(`Por estarem, assim justos e contratados, firmam o presente instrumento, em duas vias de igual teor, juntamente com 2 (duas) testemunhas.`);
  bodyText(`${city}, ${contractDate}.`);

  cursorY -= 36;
  const signatureWidth = 190;
  const leftX = margin + 10;
  const rightX = width / 2 + 10;

  drawLine(currentPage, leftX, cursorY, signatureWidth, muted);
  drawLine(currentPage, rightX, cursorY, signatureWidth, muted);
  cursorY -= 18;
  currentPage.drawText('Locador (a)', { x: leftX + (signatureWidth - fontRegular.widthOfTextAtSize('Locador (a)', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });
  currentPage.drawText('Cônjuge', { x: rightX + (signatureWidth - fontRegular.widthOfTextAtSize('Cônjuge', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });

  cursorY -= 52;
  drawLine(currentPage, leftX, cursorY, signatureWidth, muted);
  drawLine(currentPage, rightX, cursorY, signatureWidth, muted);
  cursorY -= 18;
  currentPage.drawText('Locatário (a)', { x: leftX + (signatureWidth - fontRegular.widthOfTextAtSize('Locatário (a)', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });
  currentPage.drawText('Cônjuge', { x: rightX + (signatureWidth - fontRegular.widthOfTextAtSize('Cônjuge', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });

  cursorY -= 52;
  drawLine(currentPage, leftX, cursorY, signatureWidth, muted);
  drawLine(currentPage, rightX, cursorY, signatureWidth, muted);
  cursorY -= 18;
  currentPage.drawText('Testemunha 1', { x: leftX + (signatureWidth - fontRegular.widthOfTextAtSize('Testemunha 1', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });
  currentPage.drawText('Testemunha 2', { x: rightX + (signatureWidth - fontRegular.widthOfTextAtSize('Testemunha 2', 11)) / 2, y: cursorY, size: 11, font: fontBold, color: ink });

  cursorY -= 16;
  currentPage.drawText('RG:', { x: leftX, y: cursorY, size: 10, font: fontRegular, color: muted });
  currentPage.drawText('RG:', { x: rightX, y: cursorY, size: 10, font: fontRegular, color: muted });

  return await pdf.save();
}

export async function createContractPdf(input: ContractPdfInput): Promise<string> {
  const bytes = await createContractPdfBuffer(input);
  const relativePath = `/contracts/${input.leaseId}.pdf`;
  const absoluteDir = path.join(process.cwd(), 'public', 'contracts');
  await mkdir(absoluteDir, { recursive: true });
  await writeFile(path.join(absoluteDir, `${input.leaseId}.pdf`), bytes);
  return relativePath;
}
