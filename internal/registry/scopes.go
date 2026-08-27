package registry

import "sort"

// IdentityToAccessToken maps identity to the accessTokens value in meta JSON.
// "bot" maps to "tenant"; others pass through.
func IdentityToAccessToken(identity string) string {
	if identity == "bot" {
		return "tenant"
	}
	return identity
}

// CollectScopesForProjects collects the recommended scope for each API method
// in the specified from_meta projects. For each method, only the scope with
// the highest priority score is selected (minimum privilege).
func CollectScopesForProjects(projects []string, identity string) []string {
	priorities := LoadScopePriorities()
	scopeSet := make(map[string]bool)
	for _, project := range projects {
		spec := LoadFromMeta(project)
		if spec == nil {
			continue
		}
		resources, _ := spec["resources"].(map[string]interface{})
		walkResourceMethods(resources, func(methodMap map[string]interface{}) {
			if !methodSupportedForIdentity(methodMap, identity) {
				return
			}
			scopes, ok := methodMap["scopes"].([]interface{})
			if !ok || len(scopes) == 0 {
				return
			}
			bestScope := ""
			bestScore := -1
			for _, s := range scopes {
				str, ok := s.(string)
				if !ok {
					continue
				}
				score := DefaultScopeScore
				if v, exists := priorities[str]; exists {
					score = v
				}
				if score > bestScore {
					bestScore = score
					bestScope = str
				}
			}
			if bestScope != "" {
				scopeSet[bestScope] = true
			}
		})
	}

	result := make([]string, 0, len(scopeSet))
	for s := range scopeSet {
		result = append(result, s)
	}
	sort.Strings(result)
	return result
}
