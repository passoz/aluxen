package ai

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
)

// Este arquivo guarda os prompts e a conversão da saída crua do provedor nos
// valores tipados do domínio. O prompt diz o formato exato; o parse confere o
// formato; nada aqui decide negócio.

// paymentPrompt descreve a leitura do comprovante de pagamento. O texto cita
// "JSON" porque é exigência dos provedores em modo JSON.
const paymentPrompt = `Você lê comprovantes de pagamento de aluguel (Pix, transferência, boleto, recibo) e responde apenas com JSON.
Formato exato, sem texto em volta:
{"amount": 0.0, "date": "YYYY-MM-DD", "payer": "", "payee": "", "identifier": "", "confidence": 0.0}
Regras:
- amount: valor pago em reais, número puro com ponto decimal (ex.: 1500.50). Se o valor pago não estiver legível, use null.
- date: data do pagamento em YYYY-MM-DD. Se não estiver legível, use "".
- payer: nome de quem pagou (pagador).
- payee: nome de quem recebeu (destinatário/beneficiário).
- identifier: identificador da transação (id, end-to-end, autenticação, hash), quando existir.
- confidence: sua confiança na leitura, de 0 a 1.
Nunca invente dado: o que não estiver no documento fica vazio ou null.`

// paymentInstruction acompanha a imagem do comprovante.
const paymentInstruction = "Leia o comprovante anexado e devolva o JSON do formato pedido."

// leasePrompt descreve a leitura de contrato de locação antigo (PDF nativo ou
// digitalizado), para propor cadastro — nunca gravar direto.
const leasePrompt = `Você lê contratos de locação residencial (PDF nativo ou digitalizado) e responde apenas com JSON.
Formato exato, sem texto em volta:
{"tenantName": "", "tenantCpf": "", "tenantEmail": "", "tenantPhone": "", "propertyName": "", "propertyCity": "", "propertyState": "", "startDate": "YYYY-MM-DD", "endDate": "YYYY-MM-DD", "dueDay": 0, "rentAmount": 0.0, "adjustIndex": "", "guarantee": "", "confidence": 0.0}
Regras:
- tenantName: nome completo do locatário. É obrigatório: se não estiver legível, use "".
- tenantCpf: CPF do locatário, só dígitos.
- tenantEmail e tenantPhone: contato do locatário, quando constarem.
- propertyName: endereço do imóvel locado; propertyCity e propertyState: cidade e UF.
- startDate e endDate: início e fim do prazo, em YYYY-MM-DD.
- dueDay: dia de vencimento do aluguel, de 1 a 31.
- rentAmount: valor do aluguel em reais, número puro com ponto decimal.
- adjustIndex: índice de reajuste (IGPM, IPCA, IGP-M).
- guarantee: garantia (fiador, caução, seguro-fiança), quando constar.
- confidence: sua confiança na leitura, de 0 a 1.
Nunca invente dado: o que não estiver no contrato fica vazio ou zero.`

// leaseInstruction acompanha o PDF do contrato.
const leaseInstruction = "Leia o contrato anexado e devolva o JSON do formato pedido."

// personPrompt descreve a leitura de documento pessoal (RG, CNH, comprovante de
// residência), para propor cadastro — nunca gravar direto.
const personPrompt = `Você lê documentos pessoais (RG, CNH, comprovante de residência) e responde apenas com JSON.
Formato exato, sem texto em volta:
{"name": "", "cpf": "", "email": "", "phone": "", "address": "", "city": "", "state": "", "zip": "", "confidence": 0.0}
Regras:
- name: nome completo da pessoa. É obrigatório: se não estiver legível, use "".
- cpf: CPF, só dígitos, quando constar.
- email e phone: contato, quando constar.
- address: logradouro, número e complemento; city e state: cidade e UF; zip: CEP, só dígitos.
- confidence: sua confiança na leitura, de 0 a 1.
Nunca invente dado: o que não estiver no documento fica vazio.`

// personInstruction acompanha a foto do documento pessoal.
const personInstruction = "Leia o documento anexado e devolva o JSON do formato pedido."

// ExtractPaymentProof lê um comprovante de pagamento e devolve o que foi lido,
// junto com a resposta crua do provedor para a trilha de auditoria.
//
// A baixa não acontece aqui: quem decide é domain.EvaluatePaymentMatch, aplicada
// por internal/application.ReceiptPay. Erro de leitura nunca vira baixa.
func (c *Client) ExtractPaymentProof(ctx context.Context, doc Document) (domain.PaymentExtraction, string, error) {
	raw, err := c.complete(ctx, paymentPrompt, paymentInstruction, doc)
	if err != nil {
		return domain.PaymentExtraction{}, raw, err
	}
	extraction, err := parsePaymentExtraction(raw)
	if err != nil {
		return domain.PaymentExtraction{}, raw, err
	}
	return extraction, raw, nil
}

// ExtractLeaseDocument lê um contrato de locação antigo e devolve os campos
// propostos para o cadastro. Nenhum registro é criado sem confirmação humana.
func (c *Client) ExtractLeaseDocument(ctx context.Context, doc Document) (domain.LeaseIntakeFields, string, error) {
	raw, err := c.complete(ctx, leasePrompt, leaseInstruction, doc)
	if err != nil {
		return domain.LeaseIntakeFields{}, raw, err
	}
	fields, err := parseLeaseFields(raw)
	if err != nil {
		return domain.LeaseIntakeFields{}, raw, err
	}
	return fields, raw, nil
}

// ExtractPersonDocument lê um documento pessoal e devolve os campos propostos
// para o cadastro. Nenhum registro é criado sem confirmação humana.
func (c *Client) ExtractPersonDocument(ctx context.Context, doc Document) (domain.PersonIntakeFields, string, error) {
	raw, err := c.complete(ctx, personPrompt, personInstruction, doc)
	if err != nil {
		return domain.PersonIntakeFields{}, raw, err
	}
	fields, err := parsePersonFields(raw)
	if err != nil {
		return domain.PersonIntakeFields{}, raw, err
	}
	return fields, raw, nil
}

// parsePaymentExtraction valida a leitura do comprovante. O que o casamento
// determinístico precisa (valor, data e confiança) é obrigatório; os demais
// campos ficam vazios quando o documento não os traz.
func parsePaymentExtraction(raw string) (domain.PaymentExtraction, error) {
	object, err := fields(raw)
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	amount, err := requiredAmount(object, "amount")
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	date, err := dateField(object, "date", true)
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	payer, err := textField(object, "payer")
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	payee, err := textField(object, "payee")
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	identifier, err := textField(object, "identifier")
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	confidence, err := confidenceField(object)
	if err != nil {
		return domain.PaymentExtraction{}, err
	}
	return domain.PaymentExtraction{
		Amount:     amount,
		Date:       date,
		Payer:      payer,
		Payee:      payee,
		Identifier: identifier,
		Confidence: confidence,
	}, nil
}

// parseLeaseFields valida a leitura do contrato. O nome do locatário é a âncora:
// sem ele a proposta de cadastro não serve para nada.
func parseLeaseFields(raw string) (domain.LeaseIntakeFields, error) {
	object, err := fields(raw)
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	tenantName, err := requiredText(object, "tenantName")
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	startDate, err := dateField(object, "startDate", false)
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	endDate, err := dateField(object, "endDate", false)
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	dueDay, err := dayField(object, "dueDay")
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	rentAmount, err := amountField(object, "rentAmount")
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}
	confidence, err := confidenceField(object)
	if err != nil {
		return domain.LeaseIntakeFields{}, err
	}

	var result domain.LeaseIntakeFields
	result.TenantName = tenantName
	result.StartDate = startDate
	result.EndDate = endDate
	result.DueDay = dueDay
	result.RentAmount = rentAmount
	result.Confidence = confidence
	texts := []struct {
		key    string
		target *string
	}{
		{"tenantCpf", &result.TenantCPF},
		{"tenantEmail", &result.TenantEmail},
		{"tenantPhone", &result.TenantPhone},
		{"propertyName", &result.PropertyName},
		{"propertyCity", &result.PropertyCity},
		{"propertyState", &result.PropertyState},
		{"adjustIndex", &result.AdjustIndex},
		{"guarantee", &result.Guarantee},
	}
	for _, field := range texts {
		value, err := textField(object, field.key)
		if err != nil {
			return domain.LeaseIntakeFields{}, err
		}
		*field.target = value
	}
	return result, nil
}

// parsePersonFields valida a leitura do documento pessoal. O nome é a âncora.
func parsePersonFields(raw string) (domain.PersonIntakeFields, error) {
	object, err := fields(raw)
	if err != nil {
		return domain.PersonIntakeFields{}, err
	}
	name, err := requiredText(object, "name")
	if err != nil {
		return domain.PersonIntakeFields{}, err
	}
	confidence, err := confidenceField(object)
	if err != nil {
		return domain.PersonIntakeFields{}, err
	}

	var result domain.PersonIntakeFields
	result.Name = name
	result.Confidence = confidence
	texts := []struct {
		key    string
		target *string
	}{
		{"cpf", &result.CPF},
		{"email", &result.Email},
		{"phone", &result.Phone},
		{"address", &result.Address},
		{"city", &result.City},
		{"state", &result.State},
		{"zip", &result.Zip},
	}
	for _, field := range texts {
		value, err := textField(object, field.key)
		if err != nil {
			return domain.PersonIntakeFields{}, err
		}
		*field.target = value
	}
	return result, nil
}
