package models

import "strings"

// LowestMultiplierID returns the public model id with the smallest creditMultiplier.
// Models without a known multiplier are ignored when any known multiplier exists;
// otherwise the first non-blank id wins. Ties keep list order.
func LowestMultiplierID(list []Model) (string, bool) {
	bestID := ""
	var bestMult float64
	haveKnown := false

	for _, m := range list {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if m.CreditMultiplier == nil {
			if !haveKnown && bestID == "" {
				bestID = id
			}
			continue
		}
		mult := *m.CreditMultiplier
		if !haveKnown || mult < bestMult {
			bestID = id
			bestMult = mult
			haveKnown = true
		}
	}
	return bestID, bestID != ""
}
