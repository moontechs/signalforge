// Package discover turns problem clusters into jobs and product hypotheses.
package discover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

type jobResponse struct {
	Jobs []domain.JobToBeDone `json:"jobs"`
}

// Generator creates JTBD records from clusters.
type Generator struct {
	Client domain.LLMClient
	Model  string
}

func (g Generator) Generate(ctx context.Context, cluster domain.ProblemCluster) ([]domain.JobToBeDone, error) {
	if g.Client == nil {
		return nil, fmt.Errorf("discover: nil LLM client")
	}
	var schema jobResponse
	resp, err := g.Client.Complete(ctx, domain.CompletionRequest{Model: g.Model, System: "Return only valid JSON. Derive concise, non-overlapping jobs-to-be-done.", Prompt: clusterPrompt(cluster), MaxTokens: 2000, Schema: &schema})
	if err != nil {
		return nil, fmt.Errorf("generate JTBD: %w", err)
	}
	if err := json.Unmarshal([]byte(resp.Content), &schema); err != nil {
		return nil, fmt.Errorf("parse JTBD: %w", err)
	}
	if len(schema.Jobs) < 1 || len(schema.Jobs) > 3 {
		return nil, fmt.Errorf("JTBD count must be 1-3")
	}
	now := time.Now().UTC()
	for i := range schema.Jobs {
		j := &schema.Jobs[i]
		if strings.TrimSpace(j.Situation) == "" || strings.TrimSpace(j.Motivation) == "" || strings.TrimSpace(j.ExpectedOutcome) == "" || len(j.TargetUsers) == 0 {
			return nil, fmt.Errorf("JTBD %d missing required fields", i)
		}
		j.Statement = RenderStatement(j.Situation, j.TargetUsers[0], j.Motivation, j.ExpectedOutcome)
		j.ID = stableID(cluster.ID, j.Statement)
		j.ProblemClusterID = cluster.ID
		j.EvidenceSignalIDs = append([]string(nil), cluster.RepresentativeSignalIDs...)
		j.CreatedAt = now
		j.Model = resp.Model
	}
	return schema.Jobs, nil
}

// RenderStatement renders the canonical JTBD statement.
func RenderStatement(situation, user, motivation, outcome string) string {
	return fmt.Sprintf("When %s, %s wants to %s, so they can %s", strings.TrimSpace(situation), strings.TrimSpace(user), strings.TrimSpace(motivation), strings.TrimSpace(outcome))
}

func clusterPrompt(c domain.ProblemCluster) string {
	b, _ := json.Marshal(c)
	return "Create 1-3 JTBDs from this complete problem cluster JSON. Every job must include situation, motivation, expected_outcome, target_users, current_solutions, and constraints.\n" + string(b)
}
func stableID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
