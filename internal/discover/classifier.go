package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/moontechs/signalforge/internal/domain"
	"strings"
)

type classification struct {
	ProductType  string `json:"product_type"`
	Rationale    string `json:"rationale"`
	WorthSolving bool   `json:"worth_solving"`
}
type Classifier struct {
	Client domain.LLMClient
	Model  string
}

// Classify determines whether a JTBD merits product generation.
func (c Classifier) Classify(ctx context.Context, j domain.JobToBeDone) (domain.ProductType, string, error) {
	if c.Client == nil {
		return "", "", fmt.Errorf("discover: nil LLM client")
	}
	var out classification
	b, _ := json.Marshal(j)
	r, e := c.Client.Complete(ctx, domain.CompletionRequest{Model: c.Model, System: "Return only valid JSON.", Prompt: "Assess whether this JTBD is worth solving with a product. Return product_type as no_product when not worth solving, with rationale. JTBD:\n" + string(b), Schema: &out, MaxTokens: 500})
	if e != nil {
		return "", "", fmt.Errorf("classify JTBD: %w", e)
	}
	if e = json.Unmarshal([]byte(r.Content), &out); e != nil {
		return "", "", fmt.Errorf("parse classification: %w", e)
	}
	p := strings.TrimSpace(out.ProductType)
	if !domain.IsValidProductType(p) {
		return "", "", fmt.Errorf("invalid product type %q", p)
	}
	if !out.WorthSolving {
		p = string(domain.ProductTypeNoProduct)
	}
	return domain.ProductType(p), out.Rationale, nil
}
