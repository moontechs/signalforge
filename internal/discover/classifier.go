package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/moontechs/signalforge/internal/domain"
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
func (c Classifier) Classify(ctx context.Context, job *domain.JobToBeDone) (domain.ProductType, string, error) {
	if c.Client == nil {
		return "", "", errors.New("discover: nil LLM client")
	}
	var out classification
	body, err := json.Marshal(job)
	if err != nil {
		return "", "", fmt.Errorf("encode JTBD: %w", err)
	}
	response, err := c.Client.Complete(ctx, domain.CompletionRequest{Model: c.Model, System: "Return only valid JSON.", Prompt: "Assess whether this JTBD is worth solving with a product. Return product_type as no_product when not worth solving, with rationale. JTBD:\n" + string(body), Schema: &out, MaxTokens: 500})
	if err != nil {
		return "", "", fmt.Errorf("classify JTBD: %w", err)
	}
	if err := json.Unmarshal([]byte(response.Content), &out); err != nil {
		return "", "", fmt.Errorf("parse classification: %w", err)
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
