// Package domain contains all domain models for SignalForge.
package domain

import "time"

// ProductType represents the type of product a solution could be.
type ProductType string

const (
	ProductTypeSaaS             ProductType = "saas"
	ProductTypeMobileApp        ProductType = "mobile_app"
	ProductTypeBrowserExtension ProductType = "browser_extension"
	ProductTypeDesktopApp       ProductType = "desktop_app"
	ProductTypeAPI              ProductType = "api"
	ProductTypeDeveloperTool    ProductType = "developer_tool"
	ProductTypeCLITool          ProductType = "cli_tool"
	ProductTypePlugin           ProductType = "plugin"
	ProductTypeAIAgent          ProductType = "ai_agent"
	ProductTypeIntegration      ProductType = "integration"
	ProductTypeMarketplace      ProductType = "marketplace"
	ProductTypeNoProduct        ProductType = "no_product"
)

// ValidProductTypes returns all valid product types.
func ValidProductTypes() []ProductType {
	return []ProductType{
		ProductTypeSaaS, ProductTypeMobileApp, ProductTypeBrowserExtension,
		ProductTypeDesktopApp, ProductTypeAPI, ProductTypeDeveloperTool,
		ProductTypeCLITool, ProductTypePlugin, ProductTypeAIAgent,
		ProductTypeIntegration, ProductTypeMarketplace, ProductTypeNoProduct,
	}
}

// IsValidProductType checks if a product type is valid.
func IsValidProductType(pt string) bool {
	for _, v := range ValidProductTypes() {
		if string(v) == pt {
			return true
		}
	}
	return false
}

// RunStatus represents the status of a pipeline run.
type RunStatus string

const (
	RunStatusRunning           RunStatus = "running"
	RunStatusCompleted         RunStatus = "completed"
	RunStatusCompletedWithErrs RunStatus = "completed_with_errors"
	RunStatusCancelled         RunStatus = "cancelled"
	RunStatusFailed            RunStatus = "failed"
	RunStatusRequestLimitReach RunStatus = "request_limit_reached"
)

// Recommendation represents a recommendation for a solution.
type Recommendation string

const (
	RecommendationStrongCandidate  Recommendation = "strong_candidate"
	RecommendationInvestigate      Recommendation = "investigate_further"
	RecommendationNicheOpportunity Recommendation = "niche_opportunity"
	RecommendationFeatureNotCo     Recommendation = "feature_not_company"
	RecommendationTooCompetitive   Recommendation = "too_competitive"
	RecommendationWeakEvidence     Recommendation = "weak_evidence"
	RecommendationPlatformRisk     Recommendation = "platform_risk"
	RecommendationLowFrequency     Recommendation = "low_frequency"
	RecommendationPoorProductFit   Recommendation = "poor_product_fit"
	RecommendationNoProduct        Recommendation = "no_product"
)

// Comment represents a comment from any source.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Score     int       `json:"score,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RawSignal represents a raw signal collected from any source.
type RawSignal struct {
	ID           string            `json:"id"`
	Source       string            `json:"source"`
	SourceID     string            `json:"source_id"`
	SourceType   string            `json:"source_type"`
	URL          string            `json:"url"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	Comments     []Comment         `json:"comments,omitempty"`
	Community    string            `json:"community"`
	Repository   string            `json:"repository,omitempty"`
	Category     string            `json:"category,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Labels       []string          `json:"labels,omitempty"`
	Score        int               `json:"score,omitempty"`
	CommentCount int               `json:"comment_count,omitempty"`
	ReactionCnt  int               `json:"reaction_count,omitempty"`
	ViewCount    int               `json:"view_count,omitempty"`
	AnswerCount  int               `json:"answer_count,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
	CollectedAt  time.Time         `json:"collected_at"`
	ContentHash  string            `json:"content_hash"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ProblemSignal represents a classified problem signal.
type ProblemSignal struct {
	ID                  string    `json:"id"`
	RawSignalID         string    `json:"raw_signal_id"`
	Source              string    `json:"source"`
	URL                 string    `json:"url"`
	IsProblemSignal     bool      `json:"is_problem_signal"`
	Relevance           float64   `json:"relevance"`
	Problem             string    `json:"problem"`
	TargetUser          string    `json:"target_user"`
	Context             string    `json:"context"`
	CurrentWorkaround   string    `json:"current_workaround"`
	DesiredOutcome      string    `json:"desired_outcome"`
	Recurring           bool      `json:"recurring"`
	ProductSolvable     bool      `json:"product_solvable"`
	IsTemporaryIncident bool      `json:"is_temporary_incident"`
	IsSupportQuestion   bool      `json:"is_support_question"`
	IsExistingBug       bool      `json:"is_existing_bug"`
	IsConfigurationIssue bool     `json:"is_configuration_issue"`
	IsFeatureRequest    bool      `json:"is_feature_request"`
	SeverityHint        float64   `json:"severity_hint"`
	FrequencyHint       float64   `json:"frequency_hint"`
	PaymentHint         float64   `json:"payment_hint"`
	FrustrationHint     float64   `json:"frustration_hint"`
	Keywords            []string  `json:"keywords"`
	Entities            []string  `json:"entities"`
	Actions             []string  `json:"actions"`
	Constraints         []string  `json:"constraints"`
	ClassificationModel string    `json:"classification_model"`
	ClassifiedAt        time.Time `json:"classified_at"`
}

// ProblemScorecard holds scores for a problem cluster.
type ProblemScorecard struct {
	EvidenceStrength     float64 `json:"evidence_strength"`
	Recurrence           float64 `json:"recurrence"`
	Severity             float64 `json:"severity"`
	WorkaroundCost       float64 `json:"workaround_cost"`
	SourceDiversity      float64 `json:"source_diversity"`
	Longevity            float64 `json:"longevity"`
	UserSpecificity      float64 `json:"user_specificity"`
	ProductSolvability   float64 `json:"product_solvability"`
}

// ProblemScorecardWeights returns the weights for each scoring dimension.
func ProblemScorecardWeights() map[string]float64 {
	return map[string]float64{
		"evidence_strength":   0.20,
		"recurrence":          0.15,
		"severity":            0.15,
		"workaround_cost":     0.15,
		"source_diversity":    0.10,
		"longevity":           0.10,
		"user_specificity":    0.05,
		"product_solvability": 0.10,
	}
}

// Total calculates the weighted total for a problem scorecard.
func (ps ProblemScorecard) Total() float64 {
	w := ProblemScorecardWeights()
	total := 0.0
	total += ps.EvidenceStrength * w["evidence_strength"]
	total += ps.Recurrence * w["recurrence"]
	total += ps.Severity * w["severity"]
	total += ps.WorkaroundCost * w["workaround_cost"]
	total += ps.SourceDiversity * w["source_diversity"]
	total += ps.Longevity * w["longevity"]
	total += ps.UserSpecificity * w["user_specificity"]
	total += ps.ProductSolvability * w["product_solvability"]
	return total * 10
}

// ProblemCluster represents a cluster of related problem signals.
type ProblemCluster struct {
	ID                     string          `json:"id"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
	Title                  string          `json:"title"`
	Summary                string          `json:"summary"`
	Problem                string          `json:"problem"`
	TargetUsers            []string        `json:"target_users"`
	Contexts               []string        `json:"contexts"`
	CurrentWorkarounds     []string        `json:"current_workarounds"`
	DesiredOutcomes        []string        `json:"desired_outcomes"`
	Constraints            []string        `json:"constraints"`
	SignalIDs              []string        `json:"signal_ids"`
	RepresentativeSignalIDs []string       `json:"representative_signal_ids"`
	SignalCount            int             `json:"signal_count"`
	IndependentSources     int             `json:"independent_sources"`
	IndependentDomains     int             `json:"independent_domains"`
	SourceTypes            []string        `json:"source_types"`
	FirstObservedAt        time.Time       `json:"first_observed_at"`
	LastObservedAt         time.Time       `json:"last_observed_at"`
	Keywords               []string        `json:"keywords"`
	Entities               []string        `json:"entities"`
	Actions                []string        `json:"actions"`
	ProblemScore           ProblemScorecard `json:"problem_score"`
	ProblemTotal           float64         `json:"problem_total"`
	Confidence             float64         `json:"confidence"`
	Status                 string          `json:"status"`
}

// JobToBeDone represents a job to be done derived from a cluster.
type JobToBeDone struct {
	ID                string    `json:"id"`
	ProblemClusterID  string    `json:"problem_cluster_id"`
	Statement         string    `json:"statement"`
	Situation         string    `json:"situation"`
	Motivation        string    `json:"motivation"`
	ExpectedOutcome   string    `json:"expected_outcome"`
	TargetUsers       []string  `json:"target_users"`
	CurrentSolutions  []string  `json:"current_solutions"`
	Constraints       []string  `json:"constraints"`
	EvidenceSignalIDs []string  `json:"evidence_signal_ids"`
	CreatedAt         time.Time `json:"created_at"`
	Model             string    `json:"model"`
}

// Evidence represents a piece of evidence for a solution hypothesis.
type Evidence struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
	Source             string    `json:"source"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	Snippet            string    `json:"snippet"`
	Query              string    `json:"query,omitempty"`
	IsDirectObservation bool     `json:"is_direct_observation"`
	IsInference        bool     `json:"is_inference"`
	Relevance          float64   `json:"relevance"`
	CollectedAt        time.Time `json:"collected_at"`
}

// ValidEvidenceTypes returns valid evidence types.
func ValidEvidenceTypes() []string {
	return []string{
		"problem_mention", "manual_workaround", "feature_request",
		"integration_gap", "negative_feedback", "competitor",
		"alternative_solution", "pricing_signal", "adoption_signal",
		"platform_risk", "implementation_constraint", "temporary_incident", "irrelevant",
	}
}

// Competitor represents a competitor or alternative solution.
type Competitor struct {
	Name              string   `json:"name"`
	URL               string   `json:"url"`
	Domain            string   `json:"domain"`
	ProductType       string   `json:"product_type"`
	Description       string   `json:"description"`
	TargetUser        string   `json:"target_user"`
	PricingText       string   `json:"pricing_text"`
	RatingText        string   `json:"rating_text"`
	UsersText         string   `json:"users_text"`
	ActivityText      string   `json:"activity_text"`
	Strengths         []string `json:"strengths"`
	Weaknesses        []string `json:"weaknesses"`
	MissingCapabilities []string `json:"missing_capabilities"`
	EvidenceIDs       []string `json:"evidence_ids"`
}

// ImplementationAnalysis holds implementation analysis for a solution.
type ImplementationAnalysis struct {
	Complexity              string   `json:"complexity"`
	EstimatedDaysMin        int      `json:"estimated_days_min"`
	EstimatedDaysMax        int      `json:"estimated_days_max"`
	NeedsBackend            bool     `json:"needs_backend"`
	NeedsAuth               bool     `json:"needs_auth"`
	NeedsPayments           bool     `json:"needs_payments"`
	NeedsMobileApp          bool     `json:"needs_mobile_app"`
	NeedsBrowserExtension   bool     `json:"needs_browser_extension"`
	NeedsDesktopClient      bool     `json:"needs_desktop_client"`
	NeedsExternalAPI        bool     `json:"needs_external_api"`
	NeedsAI                 bool     `json:"needs_ai"`
	StoresUserData          bool     `json:"stores_user_data"`
	ProcessesSensitiveData  bool     `json:"processes_sensitive_data"`
	PlatformDependencies    []string `json:"platform_dependencies"`
	ExternalIntegrations    []string `json:"external_integrations"`
	Permissions             []string `json:"permissions"`
	TechnicalRisks          []string `json:"technical_risks"`
	PolicyRisks             []string `json:"policy_risks"`
	DataRisks               []string `json:"data_risks"`
}

// SolutionScorecard holds scores for a solution hypothesis.
type SolutionScorecard struct {
	ProblemFit            float64 `json:"problem_fit"`
	ProductTypeFit        float64 `json:"product_type_fit"`
	CompetitionGap        float64 `json:"competition_gap"`
	BuildSimplicity       float64 `json:"build_simplicity"`
	DistributionPotential float64 `json:"distribution_potential"`
	MonetizationPotential float64 `json:"monetization_potential"`
	RetentionPotential    float64 `json:"retention_potential"`
	PlatformSafety        float64 `json:"platform_safety"`
	Defensibility         float64 `json:"defensibility"`
}

// SolutionScorecardWeights returns the weights for each solution scoring dimension.
func SolutionScorecardWeights() map[string]float64 {
	return map[string]float64{
		"problem_fit":             0.20,
		"product_type_fit":        0.15,
		"competition_gap":         0.15,
		"build_simplicity":        0.10,
		"distribution_potential":  0.10,
		"monetization_potential":  0.10,
		"retention_potential":     0.08,
		"platform_safety":         0.07,
		"defensibility":           0.05,
	}
}

// Total calculates the weighted total for a solution scorecard.
func (ss SolutionScorecard) Total() float64 {
	w := SolutionScorecardWeights()
	total := 0.0
	total += ss.ProblemFit * w["problem_fit"]
	total += ss.ProductTypeFit * w["product_type_fit"]
	total += ss.CompetitionGap * w["competition_gap"]
	total += ss.BuildSimplicity * w["build_simplicity"]
	total += ss.DistributionPotential * w["distribution_potential"]
	total += ss.MonetizationPotential * w["monetization_potential"]
	total += ss.RetentionPotential * w["retention_potential"]
	total += ss.PlatformSafety * w["platform_safety"]
	total += ss.Defensibility * w["defensibility"]
	return total * 10
}

// SolutionHypothesis represents a proposed solution for a problem cluster.
type SolutionHypothesis struct {
	ID                  string                 `json:"id"`
	ProblemClusterID    string                 `json:"problem_cluster_id"`
	JobID               string                 `json:"job_id"`
	Title               string                 `json:"title"`
	Summary             string                 `json:"summary"`
	ProductType         ProductType            `json:"product_type"`
	ProductTypeConfid   float64                `json:"product_type_confidence"`
	ProductTypeReason   string                 `json:"product_type_reason"`
	TargetUser          string                 `json:"target_user"`
	Problem             string                 `json:"problem"`
	ProposedSolution    string                 `json:"proposed_solution"`
	CoreWorkflow        string                 `json:"core_workflow"`
	Differentiation     string                 `json:"differentiation"`
	MustHaveFeatures    []string               `json:"must_have_features"`
	OptionalFeatures    []string               `json:"optional_features"`
	NonGoals            []string               `json:"non_goals"`
	Competitors         []Competitor           `json:"competitors"`
	Evidence            []Evidence             `json:"evidence"`
	Implementation      ImplementationAnalysis `json:"implementation"`
	SolutionScore       SolutionScorecard      `json:"solution_score"`
	SolutionTotal       float64                `json:"solution_total"`
	Confidence          float64                `json:"confidence"`
	Strengths           []string               `json:"strengths"`
	Weaknesses          []string               `json:"weaknesses"`
	Risks               []string               `json:"risks"`
	Unknowns            []string               `json:"unknowns"`
	Recommendation      Recommendation         `json:"recommendation"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

// PipelineRun represents a pipeline run.
type PipelineRun struct {
	ID                  string            `json:"id"`
	StartedAt           time.Time         `json:"started_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	FinishedAt          *time.Time        `json:"finished_at,omitempty"`
	Command             string            `json:"command"`
	Stage               string            `json:"stage"`
	Status              RunStatus         `json:"status"`
	Sources             []string          `json:"sources"`
	CursorState         map[string]any    `json:"cursor_state"`
	ProcessedRawSignals int               `json:"processed_raw_signals"`
	ClassifiedSignals   int               `json:"classified_signals"`
	ClustersCreated     int               `json:"clusters_created"`
	IdeasCreated        int               `json:"ideas_created"`
	Errors              []RunError        `json:"errors"`
	Stats               ResearchStats     `json:"stats"`
}

// RunError represents an error during a pipeline run.
type RunError struct {
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ResearchStats holds statistics for a pipeline run.
type ResearchStats struct {
	RawSignalsCollected  int `json:"raw_signals_collected"`
	RawSignalsSkipped    int `json:"raw_signals_skipped"`
	ProblemSignalsFound  int `json:"problem_signals_found"`
	NoiseSignals         int `json:"noise_signals"`
	ClustersCreated      int `json:"clusters_created"`
	JobsCreated          int `json:"jobs_created"`
	IdeasCreated         int `json:"ideas_created"`
	DuplicateIdeas       int `json:"duplicate_ideas"`
	GitHubRequests       int `json:"github_requests"`
	HackerNewsRequests   int `json:"hackernews_requests"`
	StackExchangeReqs    int `json:"stackexchange_requests"`
	RedditRequests       int `json:"reddit_requests"`
	SERPRequests         int `json:"serp_requests"`
	UnlockerRequests     int `json:"unlocker_requests"`
	LLMRequests          int `json:"llm_requests"`
	GitHubCacheHits      int `json:"github_cache_hits"`
	HackerNewsCacheHits  int `json:"hackernews_cache_hits"`
	StackExchangeCache   int `json:"stackexchange_cache_hits"`
	RedditCacheHits      int `json:"reddit_cache_hits"`
	SERPCacheHits        int `json:"serp_cache_hits"`
	UnlockerCacheHits    int `json:"unlocker_cache_hits"`
}

// Memory represents the persistent memory of the system.
type Memory struct {
	Version             int                     `json:"version"`
	UpdatedAt           time.Time               `json:"updated_at"`
	RawSignalIDs        map[string]string       `json:"raw_signal_ids"`
	ContentHashes       map[string]string       `json:"content_hashes"`
	ProblemFingerprints map[string]string       `json:"problem_fingerprints"`
	ClusterFingerprints map[string]string       `json:"cluster_fingerprints"`
	IdeaFingerprints    map[string]string       `json:"idea_fingerprints"`
	UsedQueries         map[string]QueryMemory  `json:"used_queries"`
	RejectedPatterns    []RejectedPattern       `json:"rejected_patterns"`
	Stats               ResearchStats           `json:"stats"`
}

// QueryMemory stores information about a previously used query.
type QueryMemory struct {
	LastUsed    time.Time `json:"last_used"`
	ResultCount int       `json:"result_count"`
	Source      string    `json:"source"`
}

// RejectedPattern stores a pattern that should be rejected in future.
type RejectedPattern struct {
	Pattern   string    `json:"pattern"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// SourceCollector interface for collecting signals from a source.
type SourceCollector interface {
	Name() string
	Collect(ctx any, req CollectRequest) ([]RawSignal, error)
}

// CollectRequest represents a collection request for a source.
type CollectRequest struct {
	Since     string
	Until     string
	MaxItems  int
	Language  string
	Force     bool
	DryRun    bool
	Sources   []string
	Subreddits []string
}

// SearchResult represents a search result from Bright Data.
type SearchResult struct {
	Items []SearchItem `json:"items"`
}

// SearchItem represents a single search result item.
type SearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Domain  string `json:"domain"`
}

// PageResult represents a fetched page.
type PageResult struct {
	URL     string `json:"url"`
	Body    string `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// PageRequest represents a page fetch request.
type PageRequest struct {
	URL string `json:"url"`
}

// LLMClient interface for LLM operations.
type LLMClient interface {
	Complete(ctx any, req CompletionRequest) (CompletionResponse, error)
}

// CompletionRequest represents an LLM completion request.
type CompletionRequest struct {
	Model       string            `json:"model"`
	Prompt      string            `json:"prompt"`
	System      string            `json:"system"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	Schema      any               `json:"schema,omitempty"`
}

// CompletionResponse represents an LLM completion response.
type CompletionResponse struct {
	Content string `json:"content"`
	Model   string `json:"model"`
	Usage   *Usage `json:"usage,omitempty"`
}

// Usage represents token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SearchClient interface for search operations.
type SearchClient interface {
	SearchGoogle(ctx any, req SearchRequest) (SearchResult, error)
}

// SearchRequest represents a search request.
type SearchRequest struct {
	Query    string `json:"query"`
	Country  string `json:"country"`
	Language string `json:"language"`
}

// PageFetcher interface for fetching public pages.
type PageFetcher interface {
	FetchPublicPage(ctx any, req PageRequest) (PageResult, error)
}

// Repository interface for data persistence.
type Repository interface {
	SaveRawSignals(ctx any, signals []RawSignal) error
	ListUnclassifiedSignals(ctx any, limit int) ([]RawSignal, error)
	SaveProblemSignals(ctx any, signals []ProblemSignal) error
	SaveCluster(ctx any, cluster *ProblemCluster) error
	SaveJob(ctx any, job *JobToBeDone) error
	SaveIdea(ctx any, idea *SolutionHypothesis) error
	SaveRun(ctx any, run *PipelineRun) error
}