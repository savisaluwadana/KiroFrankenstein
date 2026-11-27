package httpapi

import "net/http"

// managerDTO is one Manager kind and its availability (Req 19.5, 20.8, 26.2).
// The LLM model identifier may be shown; the API key and endpoint never are.
type managerDTO struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Available         bool   `json:"available"`
	Model             string `json:"model,omitempty"`
	UnavailableReason string `json:"unavailableReason,omitempty"`
	// Server-wide LLM allowance left today (UTC); only when the LLM is available.
	LLMGlobalCallsRemaining  *int `json:"llmGlobalCallsRemaining,omitempty"`
	LLMGlobalTokensRemaining *int `json:"llmGlobalTokensRemaining,omitempty"`
}

type managerListDTO struct {
	Managers []managerDTO `json:"managers"`
}

// listManagers handles GET /managers.
func (s *Server) listManagers(w http.ResponseWriter, r *http.Request) {
	llmOK, model := s.svc.LLMAvailable()
	llm := managerDTO{
		Kind: "ai_llm", Name: "LLM AI", Available: llmOK,
		Description: "A language model proposes commands with a rationale, once per decision window. " +
			"Its proposals pass the same validator as yours and are recorded, so replay never calls the model. " +
			"It falls back to the rule-based AI when the provider fails.",
	}
	if llmOK {
		llm.Model = model
		if calls, tokens, ok, err := s.svc.LLMGlobalStatus(r.Context()); ok && err == nil {
			llm.LLMGlobalCallsRemaining, llm.LLMGlobalTokensRemaining = &calls, &tokens
		}
	} else {
		llm.UnavailableReason = "not configured"
	}
	writeJSON(w, http.StatusOK, managerListDTO{Managers: []managerDTO{
		{Kind: "human", Name: "Human", Available: true,
			Description: "You decide in each decision window."},
		{Kind: "ai_rule_based", Name: "Rule-based AI", Available: true,
			Description: "A fixed, explainable policy (reorder points and capacity rules). " +
				"Deterministic, needs no network and is always available."},
		llm,
	}})
}
