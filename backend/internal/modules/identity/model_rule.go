package identity

// ModelRule is the canonical model_access_rules contract. Nil Models inherits;
// an empty non-nil Models denies all. Exclude remains meaningful with inherit.
type ModelRule struct {
	AccessGroupID *int64   `json:"access_group_id"`
	Models        []string `json:"models"`
	Exclude       []string `json:"exclude"`
	Revision      int64    `json:"revision"`
}

func ResolveModelRule(rule ModelRule, named []string) []string {
	models := rule.Models
	if rule.AccessGroupID != nil {
		if named == nil {
			named = []string{}
		}
		if models == nil {
			models = named
		} else {
			allowed := map[string]bool{}
			for _, value := range named {
				allowed[value] = true
			}
			intersection := []string{}
			for _, value := range models {
				if allowed[value] {
					intersection = append(intersection, value)
				}
			}
			models = intersection
		}
	}
	if models == nil {
		return nil
	}
	excluded := map[string]bool{}
	for _, value := range rule.Exclude {
		excluded[value] = true
	}
	result := []string{}
	for _, value := range models {
		if !excluded[value] {
			result = append(result, value)
		}
	}
	return result
}
