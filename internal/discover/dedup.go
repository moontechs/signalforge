package discover

import (
	"strings"
	"unicode"

	"github.com/moontechs/signalforge/internal/domain"
)

// Duplicate records an auditable duplicate relationship.
type Duplicate struct {
	DuplicateID string `json:"duplicate_id"`
	KeptID      string `json:"kept_id"`
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Deduplicate removes exact title/product-type duplicates, preserving first-seen order.
func Deduplicate(in []domain.SolutionHypothesis) ([]domain.SolutionHypothesis, []Duplicate) {
	out := make([]domain.SolutionHypothesis, 0, len(in))
	rel := []Duplicate{}
	seen := map[string]string{}
	for index := range in {
		x := &in[index]
		k := normalize(x.Title) + "|" + string(x.ProductType)
		if id, ok := seen[k]; ok {
			rel = append(rel, Duplicate{DuplicateID: x.ID, KeptID: id})
			continue
		}
		seen[k] = x.ID
		out = append(out, *x)
	}
	return out, rel
}
