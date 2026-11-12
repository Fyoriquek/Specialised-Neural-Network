package agent

import (
	"fmt"
	"sort"
	"strings"
)

type SchemaAgent struct {
	ExpectedFeatures []string
	expectedSet      map[string]bool
	MinSimilarity    float64
}

type AgentStatus string

const (
	StatusSuccess         AgentStatus = "SUCCESS"
	StatusRequiresConfirm AgentStatus = "REQUIRES_CONFIRMATION"
	StatusFailed          AgentStatus = "FAILED"
)

type AgentResolution struct {
	Status       AgentStatus
	SuggestedMap map[string]string
	Message      string
}

type candidatePair struct {
	badCol     string
	targetCol  string
	similarity float64
}

func NewSchemaAgent(expectedFeatures []string) *SchemaAgent {
	expSet := make(map[string]bool, len(expectedFeatures))
	for _, f := range expectedFeatures {
		expSet[f] = true
	}

	return &SchemaAgent{
		ExpectedFeatures: expectedFeatures,
		expectedSet:      expSet,
		MinSimilarity:    0.45,
	}
}

func (sa *SchemaAgent) Analyze(inputFeatures map[string]float64) AgentResolution {
	var missing []string
	var extra []string

	for _, exp := range sa.ExpectedFeatures {
		if _, found := inputFeatures[exp]; !found {
			missing = append(missing, exp)
		}
	}

	for key := range inputFeatures {
		if !sa.expectedSet[key] {
			extra = append(extra, key)
		}
	}

	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) == 0 && len(extra) == 0 {
		return AgentResolution{
			Status:  StatusSuccess,
			Message: "Schema verified.",
		}
	}

	if len(missing) != len(extra) {
		return AgentResolution{
			Status:  StatusFailed,
			Message: fmt.Sprintf("Agent Error: Column count mismatch. Missing (%d): %v, Unexpected (%d): %v.", len(missing), missing, len(extra), extra),
		}
	}

	var pairs []candidatePair
	for _, bad := range extra {
		for _, target := range missing {
			sim := stringSimilarity(bad, target)
			pairs = append(pairs, candidatePair{
				badCol:     bad,
				targetCol:  target,
				similarity: sim,
			})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].similarity == pairs[j].similarity {
			return pairs[i].badCol < pairs[j].badCol
		}
		return pairs[i].similarity > pairs[j].similarity
	})

	suggestedMap := make(map[string]string, len(extra))
	usedBad := make(map[string]bool, len(extra))
	usedTarget := make(map[string]bool, len(missing))

	for _, p := range pairs {
		if usedBad[p.badCol] || usedTarget[p.targetCol] {
			continue
		}
		if len(extra) > 1 && p.similarity < sa.MinSimilarity {
			continue
		}
		suggestedMap[p.badCol] = p.targetCol
		usedBad[p.badCol] = true
		usedTarget[p.targetCol] = true
	}

	if len(suggestedMap) != len(extra) {
		return AgentResolution{
			Status:  StatusFailed,
			Message: fmt.Sprintf("Agent Error: Could not confidently match all columns. Missing: %v, Unexpected: %v.", missing, extra),
		}
	}

	var renameDescriptions []string
	for _, bad := range extra {
		renameDescriptions = append(renameDescriptions, fmt.Sprintf("'%s' -> '%s'", bad, suggestedMap[bad]))
	}

	var msg string
	if len(renameDescriptions) == 1 {
		target := suggestedMap[extra[0]]
		msg = fmt.Sprintf("Agent: Found column '%s' while '%s' was missing. Do you want to rename '%s' to '%s'?", extra[0], target, extra[0], target)
	} else {
		msg = fmt.Sprintf("Agent: Found %d mismatched columns. Do you want to apply these renames: %s?", len(renameDescriptions), strings.Join(renameDescriptions, ", "))
	}

	return AgentResolution{
		Status:       StatusRequiresConfirm,
		SuggestedMap: suggestedMap,
		Message:      msg,
	}
}

func stringSimilarity(a, b string) float64 {
	aNorm := strings.ToLower(strings.TrimSpace(a))
	bNorm := strings.ToLower(strings.TrimSpace(b))

	if aNorm == bNorm {
		return 1.0
	}

	maxLen := len([]rune(aNorm))
	if bLen := len([]rune(bNorm)); bLen > maxLen {
		maxLen = bLen
	}
	if maxLen == 0 {
		return 1.0
	}

	dist := levenshteinDistance(aNorm, bNorm)
	return 1.0 - (float64(dist) / float64(maxLen))
}

func levenshteinDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)

	for j := 0; j <= len(rb); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost

			minVal := del
			if ins < minVal {
				minVal = ins
			}
			if sub < minVal {
				minVal = sub
			}
			curr[j] = minVal
		}
		prev, curr = curr, prev
	}

	return prev[len(rb)]
}

