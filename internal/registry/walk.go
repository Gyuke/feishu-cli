package registry

// methodVisitor 访问一个 method 对象（meta_data.json 里的 methods 项）。
type methodVisitor func(method map[string]interface{})

// walkResourceMethods 递归遍历 resources（含 nested resources）下的全部 methods。
// 飞书官方 meta 既有扁平 dotted key（chat.members），也有真正的嵌套 resources。
func walkResourceMethods(resources map[string]interface{}, visit methodVisitor) {
	if resources == nil || visit == nil {
		return
	}
	for _, resSpec := range resources {
		resMap, ok := resSpec.(map[string]interface{})
		if !ok {
			continue
		}
		if methods, ok := resMap["methods"].(map[string]interface{}); ok {
			for _, methodSpec := range methods {
				if methodMap, ok := methodSpec.(map[string]interface{}); ok {
					visit(methodMap)
				}
			}
		}
		if nested, ok := resMap["resources"].(map[string]interface{}); ok {
			walkResourceMethods(nested, visit)
		}
	}
}

// walkNamedResources 递归遍历 resources，回调带上 dotted 资源名。
func walkNamedResources(resources map[string]interface{}, prefix string, visit func(name string, res map[string]interface{})) {
	if resources == nil || visit == nil {
		return
	}
	for name, resSpec := range resources {
		resMap, ok := resSpec.(map[string]interface{})
		if !ok {
			continue
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		visit(full, resMap)
		if nested, ok := resMap["resources"].(map[string]interface{}); ok {
			walkNamedResources(nested, full, visit)
		}
	}
}

func methodSupportedForIdentity(methodMap map[string]interface{}, identity string) bool {
	tokens, ok := methodMap["accessTokens"].([]interface{})
	if !ok {
		return true
	}
	want := IdentityToAccessToken(identity)
	for _, t := range tokens {
		if ts, ok := t.(string); ok && ts == want {
			return true
		}
	}
	return false
}

// CountMethods 递归统计 catalog 中的 method 数。
func CountMethods(services map[string]map[string]interface{}) int {
	n := 0
	for _, spec := range services {
		resources, _ := spec["resources"].(map[string]interface{})
		walkResourceMethods(resources, func(map[string]interface{}) { n++ })
	}
	return n
}
