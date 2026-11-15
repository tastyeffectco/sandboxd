package agentcatalog

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeepYAMLSettings puts sandboxd's rendered config over the agent's current
// one: sandboxd's keys win, everything the person or the agent added (its
// own app saves settings in the same file) stays, with its comments.
func KeepYAMLSettings(current, rendered string) (string, error) {
	// The header says what sandboxd sets: sandboxd's replaces the old one.
	current = withoutHeader(current)
	var cur, ren yaml.Node
	if err := yaml.Unmarshal([]byte(current), &cur); err != nil || len(cur.Content) == 0 {
		return rendered, nil // unreadable or empty: start again from sandboxd's
	}
	if err := yaml.Unmarshal([]byte(rendered), &ren); err != nil || len(ren.Content) == 0 {
		return "", errors.New("sandboxd's config is not valid YAML")
	}
	if cur.Content[0].Kind != yaml.MappingNode || ren.Content[0].Kind != yaml.MappingNode {
		return rendered, nil
	}
	overlay(cur.Content[0], ren.Content[0])
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&cur); err != nil {
		return "", err
	}
	return header(rendered) + b.String(), nil
}

// header is the comment lines a config starts with.
func header(config string) string {
	return config[:len(config)-len(withoutHeader(config))]
}

func withoutHeader(config string) string {
	rest := config
	for strings.HasPrefix(rest, "#") {
		i := strings.IndexByte(rest, '\n')
		if i < 0 {
			return ""
		}
		rest = rest[i+1:]
	}
	return rest
}

// overlay sets src's keys in dst: maps merge, anything else is replaced.
func overlay(dst, src *yaml.Node) {
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, val := src.Content[i], src.Content[i+1]
		found := false
		for j := 0; j+1 < len(dst.Content); j += 2 {
			if dst.Content[j].Value != key.Value {
				continue
			}
			found = true
			if dst.Content[j+1].Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
				overlay(dst.Content[j+1], val)
			} else {
				dst.Content[j+1] = val
			}
		}
		if !found {
			dst.Content = append(dst.Content, key, val)
		}
	}
}
