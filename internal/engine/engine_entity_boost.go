package engine

import (
	"context"
	"sort"

	"github.com/scrypster/muninndb/internal/engine/activation"
	"github.com/scrypster/muninndb/internal/storage"
)

const (
	// entityBoostFactor is the score added to engrams that share a named entity
	// with a top-N BFS result. Kept well below typical BFS association weights
	// (~0.3–0.9) so the boost surfaces related content without dominating.
	entityBoostFactor = float64(0.15)

	// entityBoostCap is the maximum cumulative entity boost any single engram
	// can receive. Prevents unbounded score inflation from many entity overlaps.
	entityBoostCap = float64(0.30)

	// entityBoostTopN is the number of top BFS results whose entity links are
	// used as seeds for the spread-activation pass.
	entityBoostTopN = 5
)

// applyEntityBoost performs a post-BFS spread-activation pass using named
// entities. It takes the top-N results from the BFS activation, collects
// every entity linked to those engrams via the 0x20 forward index, then
// finds all other engrams in the same vault that mention those entities via
// the 0x23 reverse index. Each such engram receives a score boost of
// entityBoostFactor, capped at entityBoostCap per engram. Only engrams
// already in the result set (with score > 0 from the activation pipeline)
// are eligible for boosting. Results are re-sorted by score descending.
func (e *Engine) applyEntityBoost(ctx context.Context, ws [8]byte, results []activation.ScoredEngram) []activation.ScoredEngram {
	if len(results) == 0 {
		return results
	}

	// Seed the boost from at most entityBoostTopN top results.
	seedCount := len(results)
	if seedCount > entityBoostTopN {
		seedCount = entityBoostTopN
	}
	seeds := results[:seedCount]

	// Build a reverse lookup: ULID → index in results slice.
	seenInResults := make(map[storage.ULID]int, len(results))
	for i, r := range results {
		seenInResults[r.Engram.ID] = i
	}

	// Track cumulative boost per engram to enforce the cap.
	boostAccum := make(map[storage.ULID]float64)

	// For each seed engram, iterate its entity links (0x20 forward index).
	for _, topEng := range seeds {
		_ = e.store.ScanEngramEntities(ctx, ws, topEng.Engram.ID, func(entityName string) error {
			// For each entity, scan all engrams that mention it (0x23 reverse index).
			return e.store.ScanEntityEngrams(ctx, entityName, func(entityWS [8]byte, engramID storage.ULID) error {
				if entityWS != ws {
					return nil // skip other vaults
				}
				// Skip the seed itself — it already has its BFS score.
				if engramID == topEng.Engram.ID {
					return nil
				}

				idx, found := seenInResults[engramID]
				if !found {
					// Not in the result set from the pipeline — skip.
					// Entity co-occurrence alone is not sufficient for inclusion.
					return nil
				}

				// Only boost results that scored > 0 through the pipeline.
				if results[idx].Score <= 0 {
					return nil
				}

				// Apply boost, respecting the per-engram cap.
				accumulated := boostAccum[engramID]
				if accumulated >= entityBoostCap {
					return nil
				}
				boost := entityBoostFactor
				if accumulated+boost > entityBoostCap {
					boost = entityBoostCap - accumulated
				}
				results[idx].Score += boost
				boostAccum[engramID] = accumulated + boost

				return nil
			})
		})
	}

	// Re-sort descending by score after boost adjustments.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}
