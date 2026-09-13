package cursor

import (
	"fmt"
	"strings"
)

// Nil flags preserve a model's default; explicit flags constrain selection.
type RunOptions struct {
	Effort         string
	Thinking, Fast *bool
}
type runVariant struct {
	slug, family, effort string
	thinking, fast       bool
}

func NormalizeEffort(effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	effort = strings.NewReplacer("-", "", "_", "").Replace(effort)
	if effort == "extrahigh" {
		return "xhigh"
	}
	for _, known := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if effort == known {
			return known
		}
	}
	return ""
}
func effortRank(effort string) int {
	for rank, known := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if effort == known {
			return rank
		}
	}
	return 3
}
func parseRunVariant(slug string) runVariant {
	variant := runVariant{slug: slug, family: strings.TrimPrefix(slug, "cursor-")}
	for {
		found := false
		for _, suffix := range []string{"extra-high", "thinking", "minimal", "medium", "xhigh", "high", "none", "fast", "low", "max"} {
			if !strings.HasSuffix(variant.family, "-"+suffix) {
				continue
			}
			variant.family = strings.TrimSuffix(variant.family, "-"+suffix)
			switch suffix {
			case "thinking":
				variant.thinking = true
			case "fast":
				variant.fast = true
			default:
				variant.effort = NormalizeEffort(suffix)
			}
			found = true
			break
		}
		if !found {
			return variant
		}
	}
}

// Exact legacy slugs pass through when no options are given. Picker IDs and
// aliases select medium effort, nearest available rank (ties prefer higher).
// Explicit fast/thinking cannot silently select an incompatible variant.
func ResolveModelVariant(requested string, options RunOptions, catalog []AvailableModel) (string, error) {
	requested = strings.TrimSpace(requested)
	if options.Effort != "" && NormalizeEffort(options.Effort) == "" {
		return "", fmt.Errorf("cursor: unknown reasoning effort %q", options.Effort)
	}
	if len(catalog) == 0 {
		catalog = FallbackModels()
	}
	requestedVariant := parseRunVariant(requested)
	for _, model := range catalog {
		matches := requested == model.ID || requested == model.RunSlug
		exact := ""
		for _, alias := range model.Aliases {
			matches = matches || requested == alias
		}
		variants := model.LegacySlugs
		if len(variants) == 0 {
			slug := model.RunSlug
			if slug == "" {
				slug = model.ID
			}
			variants = []string{slug}
		}
		for _, slug := range variants {
			if requested == slug {
				matches = true
				exact = slug
			}
		}
		if !matches {
			for _, slug := range variants {
				if requestedVariant.family == parseRunVariant(slug).family && requestedVariant.family != requested {
					matches = true
					break
				}
			}
			for _, alias := range model.Aliases {
				if requestedVariant.family == alias && requestedVariant.family != requested {
					matches = true
				}
			}
			if requestedVariant.family == model.ID && requestedVariant.family != requested {
				matches = true
			}
			if matches {
				if options.Effort == "" {
					options.Effort = requestedVariant.effort
				}
				if requestedVariant.fast && options.Fast == nil {
					flag := true
					options.Fast = &flag
				}
				if requestedVariant.thinking && options.Thinking == nil {
					flag := true
					options.Thinking = &flag
				}
			}
		}
		if !matches {
			continue
		}
		if exact != "" && requested != model.ID && options.Effort == "" && options.Fast == nil && options.Thinking == nil {
			return exact, nil
		}
		if exact != "" && requested != model.ID {
			if options.Effort == "" {
				options.Effort = requestedVariant.effort
			}
			if options.Fast == nil {
				flag := requestedVariant.fast
				options.Fast = &flag
			}
			if options.Thinking == nil {
				flag := requestedVariant.thinking
				options.Thinking = &flag
			}
		}
		desired := NormalizeEffort(options.Effort)
		if desired == "" {
			desired = "medium"
		}
		best := ""
		bestScore := 10000
		bestRank := -1
		for _, slug := range variants {
			variant := parseRunVariant(slug)
			if options.Fast != nil && variant.fast != *options.Fast {
				continue
			}
			if options.Thinking != nil && variant.thinking != *options.Thinking {
				continue
			}
			rank := effortRank(variant.effort)
			distance := rank - effortRank(desired)
			if distance < 0 {
				distance = -distance
			}
			score := distance * 4
			if options.Fast == nil && variant.fast {
				score++
			}
			if options.Thinking == nil {
				preferredThinking := options.Effort != ""
				if variant.thinking != preferredThinking {
					score++
				}
			}
			if score < bestScore || score == bestScore && rank > bestRank {
				best = slug
				bestScore = score
				bestRank = rank
			}
		}
		if best == "" {
			return "", fmt.Errorf("cursor: unsupported variant for model %q", requested)
		}
		return best, nil
	}
	return "", fmt.Errorf("cursor: unknown model %q", requested)
}

// ModelReasoningLevels returns only effort values observed in picker variants.
// An empty result means unknown, not that the model cannot reason.
func ModelReasoningLevels(model AvailableModel) []string {
	seen := map[string]bool{}
	for _, slug := range model.LegacySlugs {
		if effort := parseRunVariant(slug).effort; effort != "" {
			seen[effort] = true
		}
	}
	var levels []string
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if seen[effort] {
			levels = append(levels, effort)
		}
	}
	return levels
}
