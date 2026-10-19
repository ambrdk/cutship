package gate

import (
	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
)

func Bump(rel config.Release, changes []forge.Change) (forge.Change, bool) {
	for _, c := range changes {
		if _, _, ok := bump.Parse(rel, c.HeadRef); ok && c.MergedAt != nil {
			return c, true
		}
	}
	return forge.Change{}, false
}

func Skip(rel config.Release, l config.Lanes, changes []forge.Change, checks []forge.Check, pending []string) bool {
	if pending == nil || len(pending) > 0 || l.RequiredCheck == "" {
		return false
	}
	if _, ok := Bump(rel, changes); !ok {
		return false
	}
	latest, ok := forge.Latest(checks, l.RequiredCheck)
	return ok && latest.State == forge.Success
}
