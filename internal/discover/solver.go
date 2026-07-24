package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/moontechs/signalforge/internal/domain"
	"strings"
	"time"
)

type solutionResponse struct {
	Solutions []domain.SolutionHypothesis `json:"solutions"`
}
type Solver struct {
	Client domain.LLMClient
	Model  string
}

func (s Solver) Generate(ctx context.Context, job domain.JobToBeDone) ([]domain.SolutionHypothesis, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("discover: nil LLM client")
	}
	var out solutionResponse
	b, _ := json.Marshal(job)
	resp, err := s.Client.Complete(ctx, domain.CompletionRequest{Model: s.Model, System: "Return only valid JSON. Produce distinct, realistic product concepts.", Prompt: "Produce at least 3 solutions with different valid product_type values for this JTBD. Include title, summary, product_type_reason, target_user, problem, proposed_solution, core_workflow, differentiation, must_have_features, competitors, implementation, strengths, weaknesses, risks, and unknowns. JTBD:\n" + string(b), MaxTokens: 5000, Schema: &out})
	if err != nil {
		return nil, fmt.Errorf("generate solutions: %w", err)
	}
	if err = json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("parse solutions: %w", err)
	}
	if len(out.Solutions) < 3 {
		return nil, fmt.Errorf("solution count must be at least 3")
	}
	seen := map[domain.ProductType]bool{}
	now := time.Now().UTC()
	for i := range out.Solutions {
		x := &out.Solutions[i]
		if strings.TrimSpace(x.Title) == "" || strings.TrimSpace(x.Summary) == "" || !domain.IsValidProductType(string(x.ProductType)) || x.ProductType == domain.ProductTypeNoProduct {
			return nil, fmt.Errorf("invalid solution %d", i)
		}
		if seen[x.ProductType] {
			return nil, fmt.Errorf("solutions must use distinct product types")
		}
		seen[x.ProductType] = true
		x.ID = stableID(job.ID, x.Title, string(x.ProductType))
		x.JobID = job.ID
		x.ProblemClusterID = job.ProblemClusterID
		x.CreatedAt = now
		x.UpdatedAt = now
		x.SolutionTotal = x.SolutionScore.Total()
	}
	return out.Solutions, nil
}
