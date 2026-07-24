// Package classify classifies raw signals into problem signals using an LLM.
package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
	"github.com/moontechs/signalforge/internal/storage"
)

// Config holds configuration for the classifier.
type Config struct {
	Model       string
	BatchSize   int
	Temperature float64
	MaxTokens   int
	PromptPath  string
}

// Classifier classifies raw signals into problem signals using an LLM.
type Classifier struct {
	client domain.LLMClient
	cfg    Config
}

// ClassifyFailure represents a signal that failed to classify.
type ClassifyFailure struct {
	RawSignalID string
	Err         error
}

// New creates a new Classifier with the given LLM client and configuration.
// It applies sensible defaults for BatchSize (20), Temperature (0.1), and
// MaxTokens (4000) when the supplied values are zero.
func New(client domain.LLMClient, cfg Config) *Classifier {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.1
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4000
	}
	return &Classifier{
		client: client,
		cfg:    cfg,
	}
}

// templateData holds the fields available to the classification prompt template.
type templateData struct {
	Title    string
	Body     string
	Comments string
	Source   string
	URL      string
}

// classifyResponse is the JSON structure expected from the LLM. It mirrors the
// schema defined in the classification prompt template.
type classifyResponse struct {
	IsProblemSignal      bool     `json:"is_problem_signal"`
	Relevance            float64  `json:"relevance"`
	Problem              string   `json:"problem"`
	TargetUser           string   `json:"target_user"`
	Context              string   `json:"context"`
	CurrentWorkaround    string   `json:"current_workaround"`
	DesiredOutcome       string   `json:"desired_outcome"`
	Recurring            bool     `json:"recurring"`
	ProductSolvable      bool     `json:"product_solvable"`
	IsTemporaryIncident  bool     `json:"is_temporary_incident"`
	IsSupportQuestion    bool     `json:"is_support_question"`
	IsExistingBug        bool     `json:"is_existing_bug"`
	IsConfigurationIssue bool     `json:"is_configuration_issue"`
	IsFeatureRequest     bool     `json:"is_feature_request"`
	SeverityHint         float64  `json:"severity_hint"`
	FrequencyHint        float64  `json:"frequency_hint"`
	PaymentHint          float64  `json:"payment_hint"`
	FrustrationHint      float64  `json:"frustration_hint"`
	Keywords             []string `json:"keywords"`
	Entities             []string `json:"entities"`
	Actions              []string `json:"actions"`
	Constraints          []string `json:"constraints"`
}

// Classify processes a batch of raw signals and returns classified problem
// signals alongside any individual failures. Signals are processed in
// batches of cfg.BatchSize. Each signal is sent as a separate LLM completion
// request with the rendered prompt template.
//
// Non-problem signals (is_problem_signal=false) are still returned so they
// can be persisted and excluded from re-classification.
//
// Partial failures are collected and returned; a failure for one signal does
// not cancel the entire batch.
func (c *Classifier) Classify(ctx context.Context, raw []domain.RawSignal) ([]domain.ProblemSignal, []ClassifyFailure) {
	promptTmpl, err := c.loadPrompt()
	if err != nil {
		failures := make([]ClassifyFailure, len(raw))
		for i, s := range raw {
			failures[i] = ClassifyFailure{RawSignalID: s.ID, Err: fmt.Errorf("load prompt: %w", err)}
		}
		return nil, failures
	}

	signals := make([]domain.ProblemSignal, 0, len(raw))
	var failures []ClassifyFailure

	for i := 0; i < len(raw); i += c.cfg.BatchSize {
		end := i + c.cfg.BatchSize
		if end > len(raw) {
			end = len(raw)
		}
		batch := raw[i:end]

		for _, s := range batch {
			ps, err := c.classifyOne(ctx, s, promptTmpl)
			if err != nil {
				slog.Warn("classification failed",
					"signal_id", s.ID,
					"source", s.Source,
					"error", err,
				)
				failures = append(failures, ClassifyFailure{RawSignalID: s.ID, Err: err})
				continue
			}
			signals = append(signals, ps)
		}
	}

	return signals, failures
}

// classifyOne classifies a single raw signal by rendering the prompt template
// and calling the LLM client.
func (c *Classifier) classifyOne(ctx context.Context, raw domain.RawSignal, promptTmpl *template.Template) (domain.ProblemSignal, error) {
	data := templateData{
		Title:    raw.Title,
		Body:     raw.Body,
		Comments: formatComments(raw.Comments),
		Source:   raw.Source,
		URL:      raw.URL,
	}

	var promptBuf strings.Builder
	if err := promptTmpl.Execute(&promptBuf, data); err != nil {
		return domain.ProblemSignal{}, fmt.Errorf("render prompt: %w", err)
	}

	// Build a target schema value the LLM client can validate against.
	var schema classifyResponse

	req := domain.CompletionRequest{
		Model:       c.cfg.Model,
		Prompt:      promptBuf.String(),
		Temperature: c.cfg.Temperature,
		MaxTokens:   c.cfg.MaxTokens,
		Schema:      &schema,
	}

	resp, err := c.client.Complete(ctx, req)
	if err != nil {
		return domain.ProblemSignal{}, fmt.Errorf("LLM completion: %w", err)
	}

	var parsed classifyResponse
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		return domain.ProblemSignal{}, fmt.Errorf("parse LLM response: %w", err)
	}

	ps := domain.ProblemSignal{
		ID:                   storage.GenerateID("ps"),
		RawSignalID:          raw.ID,
		Source:               raw.Source,
		URL:                  raw.URL,
		IsProblemSignal:      parsed.IsProblemSignal,
		Relevance:            parsed.Relevance,
		Problem:              parsed.Problem,
		TargetUser:           parsed.TargetUser,
		Context:              parsed.Context,
		CurrentWorkaround:    parsed.CurrentWorkaround,
		DesiredOutcome:       parsed.DesiredOutcome,
		Recurring:            parsed.Recurring,
		ProductSolvable:      parsed.ProductSolvable,
		IsTemporaryIncident:  parsed.IsTemporaryIncident,
		IsSupportQuestion:    parsed.IsSupportQuestion,
		IsExistingBug:        parsed.IsExistingBug,
		IsConfigurationIssue: parsed.IsConfigurationIssue,
		IsFeatureRequest:     parsed.IsFeatureRequest,
		SeverityHint:         parsed.SeverityHint,
		FrequencyHint:        parsed.FrequencyHint,
		PaymentHint:          parsed.PaymentHint,
		FrustrationHint:      parsed.FrustrationHint,
		Keywords:             parsed.Keywords,
		Entities:             parsed.Entities,
		Actions:              parsed.Actions,
		Constraints:          parsed.Constraints,
		ClassificationModel:  resp.Model,
		ClassifiedAt:         time.Now(),
	}

	return ps, nil
}

// loadPrompt reads and parses the classification prompt template from disk.
func (c *Classifier) loadPrompt() (*template.Template, error) {
	data, err := os.ReadFile(c.cfg.PromptPath)
	if err != nil {
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	tmpl, err := template.New("classify").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse prompt template: %w", err)
	}

	return tmpl, nil
}

// formatComments converts a comment slice into a human-readable string
// suitable for inclusion in the prompt template.
func formatComments(comments []domain.Comment) string {
	if len(comments) == 0 {
		return "none"
	}

	var sb strings.Builder
	for i, c := range comments {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		fmt.Fprintf(&sb, "Comment %d (score: %d):\n%s", i+1, c.Score, c.Body)
	}
	return sb.String()
}
