package gate

import (
	"time"

	"github.com/ambrdk/ship/bump"
)

const requiredCheck = "lint"

type Pull struct {
	MergedAt *time.Time `json:"merged_at"`
	Head     struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

type CheckRun struct {
	Name        string     `json:"name"`
	Conclusion  string     `json:"conclusion"`
	CompletedAt *time.Time `json:"completed_at"`
}

func mergedBump(pulls []Pull) (Pull, bool) {
	for _, p := range pulls {
		if _, _, ok := bump.Parse(p.Head.Ref); ok && p.MergedAt != nil {
			return p, true
		}
	}
	return Pull{}, false
}

func Head(pulls []Pull) string {
	if p, ok := mergedBump(pulls); ok {
		return p.Head.SHA
	}
	return ""
}

func Skip(pulls []Pull, runs []CheckRun, pending []string) bool {
	if pending == nil || len(pending) > 0 {
		return false
	}
	if _, ok := mergedBump(pulls); !ok {
		return false
	}
	var latest *CheckRun
	for i := range runs {
		r := &runs[i]
		if r.Name != requiredCheck || r.CompletedAt == nil {
			continue
		}
		if latest == nil || r.CompletedAt.After(*latest.CompletedAt) {
			latest = r
		}
	}
	return latest != nil && latest.Conclusion == "success"
}
