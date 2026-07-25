package clustering

import (
	"context"
	"testing"
	"time"

	"github.com/moontechs/signalforge/internal/domain"
)

// stubLLM is a minimal LLMClient stub for testing.
type stubLLM struct {
	response string
	err      error
}

func (s *stubLLM) Complete(
	_ any,
	_ domain.CompletionRequest, //nolint:gocritic // Value signature is required by domain.LLMClient.
) (domain.CompletionResponse, error) {
	if s.err != nil {
		return domain.CompletionResponse{}, s.err
	}
	return domain.CompletionResponse{Content: s.response}, nil
}

func TestNewClusterer_Defaults(t *testing.T) {
	c := New(Config{}, nil, nil)

	if c.cfg.JaccardThreshold != defaultJaccardThreshold {
		t.Errorf("JaccardThreshold = %f, want %f", c.cfg.JaccardThreshold, defaultJaccardThreshold)
	}
	if c.cfg.EntityWeight != defaultEntityWeight {
		t.Errorf("EntityWeight = %f, want %f", c.cfg.EntityWeight, defaultEntityWeight)
	}
	if c.cfg.ActionWeight != defaultActionWeight {
		t.Errorf("ActionWeight = %f, want %f", c.cfg.ActionWeight, defaultActionWeight)
	}
	if c.cfg.KeywordWeight != defaultKeywordWeight {
		t.Errorf("KeywordWeight = %f, want %f", c.cfg.KeywordWeight, defaultKeywordWeight)
	}
	if c.cfg.TextWeight != defaultTextWeight {
		t.Errorf("TextWeight = %f, want %f", c.cfg.TextWeight, defaultTextWeight)
	}
	if c.cfg.SemanticThreshold != defaultSemanticThreshold {
		t.Errorf("SemanticThreshold = %f, want %f", c.cfg.SemanticThreshold, defaultSemanticThreshold)
	}
	if c.cfg.MinClusterSize != defaultMinClusterSize {
		t.Errorf("MinClusterSize = %d, want %d", c.cfg.MinClusterSize, defaultMinClusterSize)
	}
}

func TestNewClusterer_CustomConfig(t *testing.T) {
	cfg := Config{
		JaccardThreshold:  0.5,
		EntityWeight:      0.4,
		ActionWeight:      0.3,
		KeywordWeight:     0.2,
		TextWeight:        0.1,
		SemanticThreshold: 0.1,
		MinClusterSize:    2,
		MaxClusters:       5,
	}
	c := New(cfg, nil, nil)

	if c.cfg.JaccardThreshold != 0.5 {
		t.Errorf("JaccardThreshold = %f, want 0.5", c.cfg.JaccardThreshold)
	}
	if c.cfg.MaxClusters != 5 {
		t.Errorf("MaxClusters = %d, want 5", c.cfg.MaxClusters)
	}
}

func TestCluster_EmptySignals(t *testing.T) {
	c := New(Config{}, nil, nil)
	clusters, err := c.Cluster(context.Background(), nil)
	if err != nil {
		t.Fatalf("Cluster returned error: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}

	clusters, err = c.Cluster(context.Background(), []domain.ProblemSignal{})
	if err != nil {
		t.Fatalf("Cluster returned error: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestCluster_NoProblemSignals(t *testing.T) {
	c := New(Config{}, nil, nil)
	signals := []domain.ProblemSignal{
		{ID: "1", IsProblemSignal: false},
		{ID: "2", IsProblemSignal: false},
	}
	clusters, err := c.Cluster(context.Background(), signals)
	if err != nil {
		t.Fatalf("Cluster returned error: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestComputeSimilarity_Identical(t *testing.T) {
	c := New(Config{}, nil, nil)
	fp1 := signalFP{
		tokens:   NormalizeText("cannot install package manager"),
		actions:  CanonicalizeActions([]string{"install", "setup"}),
		entities: EntityFingerprints([]string{"npm", "node"}),
		keywords: []string{"package", "manager", "install"},
	}
	fp2 := signalFP{
		tokens:   NormalizeText("cannot install package manager"),
		actions:  CanonicalizeActions([]string{"install", "setup"}),
		entities: EntityFingerprints([]string{"npm", "node"}),
		keywords: []string{"package", "manager", "install"},
	}
	sim := c.computeSimilarity(&fp1, &fp2)
	if sim != 1.0 {
		t.Errorf("identical signals should have similarity 1.0, got %f", sim)
	}
}

func TestComputeSimilarity_Different(t *testing.T) {
	c := New(Config{}, nil, nil)
	fp1 := signalFP{
		tokens:   NormalizeText("database connection timeout error"),
		actions:  CanonicalizeActions([]string{"connect"}),
		entities: EntityFingerprints([]string{"postgresql"}),
		keywords: []string{"database", "timeout"},
	}
	fp2 := signalFP{
		tokens:   NormalizeText("ui button alignment issue on mobile"),
		actions:  CanonicalizeActions([]string{"align"}),
		entities: EntityFingerprints([]string{"react-native"}),
		keywords: []string{"ui", "mobile", "alignment"},
	}
	sim := c.computeSimilarity(&fp1, &fp2)
	if sim > 0.5 {
		t.Errorf("different signals should have low similarity, got %f", sim)
	}
}

func TestComputeSimilarity_PartialOverlap(t *testing.T) {
	c := New(Config{}, nil, nil)
	fp1 := signalFP{
		tokens:   NormalizeText("cannot install python package with pip"),
		actions:  CanonicalizeActions([]string{"install"}),
		entities: EntityFingerprints([]string{"python", "pip"}),
		keywords: []string{"package", "install", "python"},
	}
	fp2 := signalFP{
		tokens:   NormalizeText("unable to install pip package"),
		actions:  CanonicalizeActions([]string{"install", "setup"}),
		entities: EntityFingerprints([]string{"pip"}),
		keywords: []string{"install", "package"},
	}
	sim := c.computeSimilarity(&fp1, &fp2)
	if sim < 0.3 || sim > 0.95 {
		t.Errorf("partial overlap should have moderate similarity, got %f", sim)
	}
}

func TestBuildCluster_SingleSignal(t *testing.T) {
	c := New(Config{}, nil, nil)
	now := time.Now()
	signal := domain.ProblemSignal{
		ID:                "sig_1",
		Source:            "github",
		URL:               "https://github.com/example/repo/issues/1",
		IsProblemSignal:   true,
		Relevance:         0.9,
		Problem:           "Cannot install the package manager",
		TargetUser:        "developers",
		Context:           "On Ubuntu 22.04",
		CurrentWorkaround: "Use apt instead",
		DesiredOutcome:    "npm install should work",
		Recurring:         true,
		ProductSolvable:   true,
		SeverityHint:      7,
		Keywords:          []string{"install", "npm", "package"},
		Entities:          []string{"npm"},
		Actions:           []string{"install"},
		Constraints:       []string{"Ubuntu"},
		ClassifiedAt:      now,
	}

	cluster := c.buildCluster([]domain.ProblemSignal{signal})

	if cluster.SignalCount != 1 {
		t.Errorf("SignalCount = %d, want 1", cluster.SignalCount)
	}
	if cluster.Title != "Cannot install the package manager" {
		t.Errorf("Title = %q, want %q", cluster.Title, "Cannot install the package manager")
	}
	if cluster.ProblemTotal <= 0 {
		t.Errorf("ProblemTotal should be > 0, got %f", cluster.ProblemTotal)
	}
	if len(cluster.SignalIDs) != 1 {
		t.Errorf("SignalIDs length = %d, want 1", len(cluster.SignalIDs))
	}
	if cluster.ID == "" {
		t.Error("Cluster ID should not be empty")
	}
}

func TestBuildCluster_MultipleSignals(t *testing.T) {
	c := New(Config{}, nil, nil)
	now := time.Now()

	signals := []domain.ProblemSignal{
		{
			ID:                "sig_1",
			Source:            "github",
			IsProblemSignal:   true,
			Relevance:         0.9,
			Problem:           "Cannot install npm packages",
			TargetUser:        "developers",
			Context:           "On Ubuntu 22.04",
			CurrentWorkaround: "Use apt",
			DesiredOutcome:    "npm install should work",
			Recurring:         true,
			ProductSolvable:   true,
			SeverityHint:      7,
			Keywords:          []string{"install", "npm"},
			Entities:          []string{"npm"},
			Actions:           []string{"install"},
			ClassifiedAt:      now,
		},
		{
			ID:                "sig_2",
			Source:            "hackernews",
			IsProblemSignal:   true,
			Relevance:         0.8,
			Problem:           "Can't install node modules",
			TargetUser:        "javascript developers",
			Context:           "On macOS",
			CurrentWorkaround: "Use yarn instead",
			DesiredOutcome:    "npm ci should work",
			Recurring:         true,
			ProductSolvable:   true,
			SeverityHint:      6,
			Keywords:          []string{"install", "node", "modules"},
			Entities:          []string{"npm"},
			Actions:           []string{"install"},
			ClassifiedAt:      now.Add(time.Hour),
		},
	}

	cluster := c.buildCluster(signals)

	if cluster.SignalCount != 2 {
		t.Errorf("SignalCount = %d, want 2", cluster.SignalCount)
	}
	if cluster.IndependentSources != 2 {
		t.Errorf("IndependentSources = %d, want 2", cluster.IndependentSources)
	}
	if len(cluster.SourceTypes) != 2 {
		t.Errorf("SourceTypes length = %d, want 2", len(cluster.SourceTypes))
	}
	if len(cluster.Keywords) < 2 {
		t.Errorf("should have merged keywords, got %d", len(cluster.Keywords))
	}
	if len(cluster.SignalIDs) != 2 {
		t.Errorf("SignalIDs length = %d, want 2", len(cluster.SignalIDs))
	}
}

func TestComputeProblemScore(t *testing.T) {
	c := New(Config{}, nil, nil)
	now := time.Now()

	signals := []domain.ProblemSignal{
		{
			IsProblemSignal:   true,
			Relevance:         0.9,
			Recurring:         true,
			SeverityHint:      8,
			ProductSolvable:   true,
			Source:            "github",
			CurrentWorkaround: "Manual workaround",
			ClassifiedAt:      now,
		},
		{
			IsProblemSignal:   true,
			Relevance:         0.7,
			Recurring:         false,
			SeverityHint:      5,
			ProductSolvable:   false,
			Source:            "hackernews",
			CurrentWorkaround: "",
			ClassifiedAt:      now.Add(48 * time.Hour),
		},
	}

	score := c.computeProblemScore(signals)

	if score.EvidenceStrength != 0.8 {
		t.Errorf("EvidenceStrength = %f, want 0.8", score.EvidenceStrength)
	}
	if score.Recurrence != 0.5 {
		t.Errorf("Recurrence = %f, want 0.5", score.Recurrence)
	}
	if score.Severity != 0.65 {
		t.Errorf("Severity = %f, want 0.65", score.Severity)
	}
	if score.WorkaroundCost != 0.5 {
		t.Errorf("WorkaroundCost = %f, want 0.5", score.WorkaroundCost)
	}
	if score.SourceDiversity != 1.0 {
		t.Errorf("SourceDiversity = %f, want 1.0", score.SourceDiversity)
	}
	if score.ProductSolvability != 0.5 {
		t.Errorf("ProductSolvability = %f, want 0.5", score.ProductSolvability)
	}
}

func TestComputeConfidence(t *testing.T) {
	c := New(Config{}, nil, nil)
	now := time.Now()

	signals := []domain.ProblemSignal{
		{
			IsProblemSignal: true,
			Relevance:       0.9,
			Recurring:       true,
			SeverityHint:    8,
			Source:          "github",
			ClassifiedAt:    now,
		},
		{
			IsProblemSignal: true,
			Relevance:       0.8,
			Recurring:       true,
			SeverityHint:    7,
			Source:          "hackernews",
			ClassifiedAt:    now.Add(time.Hour),
		},
		{
			IsProblemSignal: true,
			Relevance:       0.7,
			Recurring:       false,
			SeverityHint:    6,
			Source:          "stackexchange",
			ClassifiedAt:    now.Add(2 * time.Hour),
		},
	}

	score := c.computeProblemScore(signals)
	confidence := c.computeConfidence(signals, score)

	if confidence <= 0 {
		t.Errorf("Confidence should be > 0, got %f", confidence)
	}
	if confidence > 100 {
		t.Errorf("Confidence should be <= 100, got %f", confidence)
	}
}

func TestCheckSemanticBoundary_LlmsaysYes(t *testing.T) {
	llm := &stubLLM{response: "yes"}
	c := New(Config{JaccardThreshold: 0.5, SemanticThreshold: 0.1}, llm, nil)

	a := domain.ProblemSignal{Problem: "Cannot login to app", Context: "Web app"}
	b := domain.ProblemSignal{Problem: "Unable to sign in", Context: "Mobile app"}

	same, err := c.checkSemanticBoundary(context.Background(), &a, &b)
	if err != nil {
		t.Fatalf("checkSemanticBoundary error: %v", err)
	}
	if !same {
		t.Error("expected same=true when LLM says yes")
	}
}

func TestCheckSemanticBoundary_LlmSaysNo(t *testing.T) {
	llm := &stubLLM{response: "no"}
	c := New(Config{JaccardThreshold: 0.5, SemanticThreshold: 0.1}, llm, nil)

	a := domain.ProblemSignal{Problem: "Cannot login to app"}
	b := domain.ProblemSignal{Problem: "Printer is out of paper"}

	same, err := c.checkSemanticBoundary(context.Background(), &a, &b)
	if err != nil {
		t.Fatalf("checkSemanticBoundary error: %v", err)
	}
	if same {
		t.Error("expected same=false when LLM says no")
	}
}

func TestCheckSemanticBoundary_NoLLM(t *testing.T) {
	c := New(Config{}, nil, nil)

	a := domain.ProblemSignal{Problem: "Cannot login to app"}
	b := domain.ProblemSignal{Problem: "Unable to sign in"}

	same, err := c.checkSemanticBoundary(context.Background(), &a, &b)
	if err != nil {
		t.Fatalf("checkSemanticBoundary error: %v", err)
	}
	if same {
		t.Error("expected same=false when no LLM client")
	}
}

func TestCluster_Integration(t *testing.T) {
	c := New(Config{JaccardThreshold: 0.3}, nil, nil)
	now := time.Now()

	signals := []domain.ProblemSignal{
		// Group A: npm install problems
		{
			ID:              "a1",
			Source:          "github",
			IsProblemSignal: true,
			Relevance:       0.9,
			Problem:         "Cannot install npm packages",
			TargetUser:      "developers",
			Context:         "Ubuntu 22.04",
			Recurring:       true,
			ProductSolvable: true,
			SeverityHint:    7,
			Keywords:        []string{"npm", "install", "package"},
			Entities:        []string{"npm"},
			Actions:         []string{"install"},
			ClassifiedAt:    now,
		},
		{
			ID:              "a2",
			Source:          "hackernews",
			IsProblemSignal: true,
			Relevance:       0.8,
			Problem:         "Npm install fails on macOS",
			TargetUser:      "javascript developers",
			Context:         "macOS Ventura",
			Recurring:       true,
			ProductSolvable: true,
			SeverityHint:    6,
			Keywords:        []string{"npm", "install", "macos"},
			Entities:        []string{"npm"},
			Actions:         []string{"install"},
			ClassifiedAt:    now.Add(time.Hour),
		},
		// Group B: Docker deployment issues
		{
			ID:              "b1",
			Source:          "github",
			IsProblemSignal: true,
			Relevance:       0.85,
			Problem:         "Docker container fails to deploy",
			TargetUser:      "devops engineers",
			Context:         "Kubernetes cluster",
			Recurring:       true,
			ProductSolvable: true,
			SeverityHint:    8,
			Keywords:        []string{"docker", "deploy", "container"},
			Entities:        []string{"docker", "kubernetes"},
			Actions:         []string{"deploy"},
			ClassifiedAt:    now.Add(2 * time.Hour),
		},
		{
			ID:              "b2",
			Source:          "stackexchange",
			IsProblemSignal: true,
			Relevance:       0.75,
			Problem:         "Cannot deploy docker image to production",
			TargetUser:      "devops",
			Context:         "AWS ECS",
			Recurring:       true,
			ProductSolvable: true,
			SeverityHint:    7,
			Keywords:        []string{"docker", "deploy", "aws"},
			Entities:        []string{"docker", "aws"},
			Actions:         []string{"deploy"},
			ClassifiedAt:    now.Add(3 * time.Hour),
		},
		// Noise signal
		{
			ID:              "c1",
			Source:          "reddit",
			IsProblemSignal: false,
			Relevance:       0.1,
			Problem:         "Just a random thought",
			Keywords:        []string{"random"},
			ClassifiedAt:    now.Add(4 * time.Hour),
		},
	}

	clusters, err := c.Cluster(context.Background(), signals)
	if err != nil {
		t.Fatalf("Cluster returned error: %v", err)
	}

	// Should get 2 clusters (noise signal excluded, 2 groups of 2 problem signals each)
	if len(clusters) != 2 {
		t.Errorf("expected 2 clusters, got %d", len(clusters))
		return
	}

	// Verify each cluster has the right number of signals
	for _, cluster := range clusters {
		if cluster.SignalCount < 2 {
			t.Errorf("cluster %s has %d signals, expected at least 2", cluster.ID, cluster.SignalCount)
		}
		if cluster.ProblemTotal <= 0 {
			t.Errorf("cluster %s has ProblemTotal=%f, expected > 0", cluster.ID, cluster.ProblemTotal)
		}
		if cluster.Confidence <= 0 {
			t.Errorf("cluster %s has Confidence=%f, expected > 0", cluster.ID, cluster.Confidence)
		}
	}
}

func TestCluster_MaxClusters(t *testing.T) {
	c := New(Config{MaxClusters: 1}, nil, nil)
	now := time.Now()

	signals := []domain.ProblemSignal{
		{
			ID:              "a1",
			Source:          "github",
			IsProblemSignal: true,
			Relevance:       0.9,
			Problem:         "Cannot install npm packages",
			Keywords:        []string{"npm"},
			Actions:         []string{"install"},
			ClassifiedAt:    now,
		},
		{
			ID:              "a2",
			Source:          "hackernews",
			IsProblemSignal: true,
			Relevance:       0.8,
			Problem:         "npm install fails",
			Keywords:        []string{"npm"},
			Actions:         []string{"install"},
			ClassifiedAt:    now.Add(time.Hour),
		},
		{
			ID:              "b1",
			Source:          "github",
			IsProblemSignal: true,
			Relevance:       0.85,
			Problem:         "Docker deploy fails",
			Keywords:        []string{"docker"},
			Actions:         []string{"deploy"},
			ClassifiedAt:    now.Add(2 * time.Hour),
		},
		{
			ID:              "b2",
			Source:          "stackexchange",
			IsProblemSignal: true,
			Relevance:       0.75,
			Problem:         "Cannot deploy docker container",
			Keywords:        []string{"docker"},
			Actions:         []string{"deploy"},
			ClassifiedAt:    now.Add(3 * time.Hour),
		},
	}

	clusters, err := c.Cluster(context.Background(), signals)
	if err != nil {
		t.Fatalf("Cluster returned error: %v", err)
	}

	if len(clusters) > 1 {
		t.Errorf("expected at most 1 cluster with MaxClusters=1, got %d", len(clusters))
	}
}

func TestFilterProblemSignals(t *testing.T) {
	signals := []domain.ProblemSignal{
		{ID: "1", IsProblemSignal: true},
		{ID: "2", IsProblemSignal: false},
		{ID: "3", IsProblemSignal: true},
	}

	filtered := filterProblemSignals(signals)
	if len(filtered) != 2 {
		t.Errorf("expected 2 problem signals, got %d", len(filtered))
	}
	if filtered[0].ID != "1" || filtered[1].ID != "3" {
		t.Error("filtered signals have wrong IDs")
	}
}

func TestJaccardTokens(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want float64
	}{
		{"both empty", nil, nil, 0},
		{"one empty", []string{"a"}, nil, 0},
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 1.0},
		{"no overlap", []string{"a"}, []string{"b"}, 0},
		{"partial", []string{"a", "b", "c"}, []string{"b", "c", "d"}, 0.5},
		{"with duplicates", []string{"a", "b", "b"}, []string{"b", "c"}, 1.0 / 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardTokens(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("jaccardTokens(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{1.5, 1},
	}

	for _, tt := range tests {
		got := clamp01(tt.input)
		if got != tt.want {
			t.Errorf("clamp01(%f) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestBuildSummary(t *testing.T) {
	tests := []struct {
		name     string
		signals  []domain.ProblemSignal
		contains string
	}{
		{
			name:     "single signal",
			signals:  []domain.ProblemSignal{{Problem: "Cannot login"}},
			contains: "Cannot login",
		},
		{
			name: "multiple signals same problem",
			signals: []domain.ProblemSignal{
				{Problem: "Cannot login"},
				{Problem: "Cannot login"},
			},
			contains: "Cannot login",
		},
		{
			name: "multiple signals different problems",
			signals: []domain.ProblemSignal{
				{Problem: "Login fails"},
				{Problem: "Cannot authenticate"},
			},
			contains: "Login fails",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildSummary(tt.signals)
			if summary == "" {
				t.Error("summary should not be empty")
			}
		})
	}
}
