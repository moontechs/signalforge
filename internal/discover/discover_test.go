package discover

import (
	"context"
	"encoding/json"
	"github.com/moontechs/signalforge/internal/domain"
	"testing"
)

type mockLLM struct {
	content string
	calls   int
}

func (m *mockLLM) Complete(_ any, _ domain.CompletionRequest) (domain.CompletionResponse, error) {
	m.calls++
	return domain.CompletionResponse{Content: m.content, Model: "test"}, nil
}
func TestRenderStatement(t *testing.T) {
	got := RenderStatement("deploying often", "developers", "debug failures", "ship safely")
	want := "When deploying often, developers wants to debug failures, so they can ship safely"
	if got != want {
		t.Fatalf("got %q", got)
	}
}
func TestDeduplicate(t *testing.T) {
	a := domain.SolutionHypothesis{ID: "a", Title: "Desk App!", ProductType: domain.ProductTypeDesktopApp}
	b := domain.SolutionHypothesis{ID: "b", Title: "desk app", ProductType: domain.ProductTypeDesktopApp}
	c := domain.SolutionHypothesis{ID: "c", Title: "desk app", ProductType: domain.ProductTypeSaaS}
	out, rels := Deduplicate([]domain.SolutionHypothesis{a, b, c})
	if len(out) != 2 || len(rels) != 1 || rels[0].KeptID != "a" {
		t.Fatalf("unexpected %#v %#v", out, rels)
	}
}
func TestGeneratorValidatesAndRenders(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"jobs": []domain.JobToBeDone{{Situation: "on call", Motivation: "find incidents", ExpectedOutcome: "restore service", TargetUsers: []string{"operators"}}}})
	m := &mockLLM{content: string(payload)}
	jobs, e := (Generator{Client: m}).Generate(context.Background(), domain.ProblemCluster{ID: "cluster", RepresentativeSignalIDs: []string{"sig"}})
	if e != nil {
		t.Fatal(e)
	}
	if jobs[0].Statement != "When on call, operators wants to find incidents, so they can restore service" {
		t.Fatalf("bad statement: %s", jobs[0].Statement)
	}
}
func TestSolverRequiresDistinctThree(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"solutions": []domain.SolutionHypothesis{{Title: "one", Summary: "s", ProductType: domain.ProductTypeSaaS}, {Title: "two", Summary: "s", ProductType: domain.ProductTypeMobileApp}, {Title: "three", Summary: "s", ProductType: domain.ProductTypeAPI}}})
	got, e := (Solver{Client: &mockLLM{content: string(payload)}}).Generate(context.Background(), domain.JobToBeDone{ID: "j"})
	if e != nil || len(got) != 3 {
		t.Fatalf("got %d, err %v", len(got), e)
	}
}
func TestProductTypeValidation(t *testing.T) {
	if !domain.IsValidProductType(string(domain.ProductTypeAIAgent)) || domain.IsValidProductType("telepathy") {
		t.Fatal("product type validation failed")
	}
}
func TestClassifierNoProduct(t *testing.T) {
	m := &mockLLM{content: `{"product_type":"no_product","rationale":"too rare","worth_solving":false}`}
	p, reason, e := (Classifier{Client: m}).Classify(context.Background(), domain.JobToBeDone{Statement: "x"})
	if e != nil || p != domain.ProductTypeNoProduct || reason != "too rare" {
		t.Fatalf("%v %q %v", p, reason, e)
	}
}

func TestGeneratorRejectsInvalidJTBD(t *testing.T) {
	m := &mockLLM{content: `{"jobs":[{"situation":"", "motivation":"x", "expected_outcome":"y", "target_users":["users"]}]}`}
	if _, err := (Generator{Client: m}).Generate(context.Background(), domain.ProblemCluster{ID: "cluster"}); err == nil {
		t.Fatal("expected invalid JTBD error")
	}
}

func TestSolverRejectsInvalidProductType(t *testing.T) {
	m := &mockLLM{content: `{"solutions":[{"title":"one","summary":"s","product_type":"invalid"},{"title":"two","summary":"s","product_type":"api"},{"title":"three","summary":"s","product_type":"saas"}]}`}
	if _, err := (Solver{Client: m}).Generate(context.Background(), domain.JobToBeDone{ID: "job"}); err == nil {
		t.Fatal("expected invalid product type error")
	}
}

func TestSolverRejectsNoProduct(t *testing.T) {
	m := &mockLLM{content: `{"solutions":[{"title":"one","summary":"s","product_type":"no_product"},{"title":"two","summary":"s","product_type":"api"},{"title":"three","summary":"s","product_type":"saas"}]}`}
	if _, err := (Solver{Client: m}).Generate(context.Background(), domain.JobToBeDone{ID: "job"}); err == nil {
		t.Fatal("expected no-product solution error")
	}
}
