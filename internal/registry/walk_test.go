package registry

import (
	"encoding/json"
	"testing"
)

func nestedScopeFixture() map[string]interface{} {
	return map[string]interface{}{
		"name": "nestsvc",
		"resources": map[string]interface{}{
			"spaces": map[string]interface{}{
				"methods": map[string]interface{}{
					"list": map[string]interface{}{
						"accessTokens": []interface{}{"user"},
						"scopes":       []interface{}{"wiki:space:read"},
					},
				},
				"resources": map[string]interface{}{
					"items": map[string]interface{}{
						"methods": map[string]interface{}{
							"get": map[string]interface{}{
								"accessTokens": []interface{}{"user"},
								"scopes":       []interface{}{"wiki:node:read"},
							},
						},
					},
				},
			},
		},
	}
}

func TestWalkResourceMethods_RecursesNested(t *testing.T) {
	var got []string
	walkResourceMethods(nestedScopeFixture()["resources"].(map[string]interface{}), func(m map[string]interface{}) {
		scopes, _ := m["scopes"].([]interface{})
		if len(scopes) > 0 {
			got = append(got, scopes[0].(string))
		}
	})
	if len(got) != 2 {
		t.Fatalf("methods = %v, want 2 nested+top", got)
	}
	found := map[string]bool{}
	for _, s := range got {
		found[s] = true
	}
	if !found["wiki:space:read"] || !found["wiki:node:read"] {
		t.Fatalf("missing nested scopes: %v", got)
	}
}

func TestCollectAllScopesFromMeta_IncludesNestedResources(t *testing.T) {
	ResetForTest()
	t.Setenv("FEISHU_CLI_REMOTE_META", "off")
	t.Setenv("FEISHU_CLI_CONFIG_DIR", t.TempDir())

	raw, _ := json.Marshal(MergedRegistry{
		Version:  "9.9.9",
		Services: []map[string]interface{}{nestedScopeFixture()},
	})
	old := embeddedMetaJSON
	t.Cleanup(func() {
		ResetForTest()
		embeddedMetaJSON = old
	})
	embeddedMetaJSON = raw
	Init()

	scopes := CollectAllScopesFromMeta("user")
	found := map[string]bool{}
	for _, s := range scopes {
		found[s] = true
	}
	if !found["wiki:space:read"] {
		t.Errorf("top-level resource scope missing: %v", scopes)
	}
	if !found["wiki:node:read"] {
		t.Fatalf("nested resource scope missing (walk not recursive): %v", scopes)
	}

	proj := CollectScopesForProjects([]string{"nestsvc"}, "user")
	pfound := map[string]bool{}
	for _, s := range proj {
		pfound[s] = true
	}
	if !pfound["wiki:node:read"] {
		t.Fatalf("CollectScopesForProjects missed nested scope: %v", proj)
	}
}

func TestCountMethods_Nested(t *testing.T) {
	svc := map[string]map[string]interface{}{"nestsvc": nestedScopeFixture()}
	if n := CountMethods(svc); n != 2 {
		t.Fatalf("CountMethods = %d, want 2", n)
	}
}
