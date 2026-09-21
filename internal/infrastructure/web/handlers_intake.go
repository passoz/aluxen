package web

import "net/http"

// Onboarding por documento (funcionalidades novas).
//
//	POST /api/intake/document — upload de contrato antigo/RG/CNH/comprovante → proposta de cadastro
//	POST /api/intake/confirm  — confirmação humana antes de gravar o cadastro
//
// Este arquivo pertence ao dono da área de onboarding; substitua os stubs pela
// implementação real mantendo as assinaturas.

func (h *Handlers) DocumentIntake(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "intake.document")
}

func (h *Handlers) DocumentIntakeConfirm(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "intake.confirm")
}
