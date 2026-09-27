package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AuthTokenUpdate represents credential updates for a single account (primary or polling).
type AuthTokenUpdate struct {
	EpicRefreshToken   *string
	SteamSessionTicket *string
	SteamID64          *string
	SteamLoginKey      *string
}

// TokenUpdates encapsulates credential updates to persist back to configuration files.
type TokenUpdates struct {
	Primary AuthTokenUpdate
	Polling AuthTokenUpdate
}

// SaveTokensToFile updates authentication tokens in the specified configuration file
// (YAML or JSON) while preserving comments, formatting, and unrelated fields.
// If filePath is empty or the file does not exist, it safely returns nil.
func SaveTokensToFile(filePath string, updates TokenUpdates) error {
	if strings.TrimSpace(filePath) == "" {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config file for token update %s: %w", filePath, err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".json" {
		return saveTokensToJSON(filePath, data, updates)
	}

	return saveTokensToYAML(filePath, data, updates)
}

func saveTokensToYAML(filePath string, data []byte, updates TokenUpdates) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("unmarshaling YAML AST: %w", err)
	}

	if len(root.Content) == 0 {
		return nil
	}

	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return nil
	}

	// 1. Apply primary auth updates under "auth"
	applyAuthNodeUpdates(docNode, "auth", updates.Primary)

	// 2. Apply polling auth updates under "polling_auth"
	applyAuthNodeUpdates(docNode, "polling_auth", updates.Polling)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("encoding updated YAML AST: %w", err)
	}
	_ = enc.Close()

	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing updated YAML config %s: %w", filePath, err)
	}

	return nil
}

func applyAuthNodeUpdates(rootMap *yaml.Node, sectionKey string, update AuthTokenUpdate) {
	if update.EpicRefreshToken == nil && update.SteamSessionTicket == nil &&
		update.SteamID64 == nil && update.SteamLoginKey == nil {
		return
	}

	sectionNode := findOrCreateMappingKey(rootMap, sectionKey)
	if sectionNode == nil || sectionNode.Kind != yaml.MappingNode {
		return
	}

	// Epic updates
	if update.EpicRefreshToken != nil {
		epicNode := findOrCreateMappingKey(sectionNode, "epic")
		if epicNode != nil && epicNode.Kind == yaml.MappingNode {
			setOrUpdateScalar(epicNode, "refresh_token", *update.EpicRefreshToken)
		}
	}

	// Steam updates
	if update.SteamSessionTicket != nil || update.SteamID64 != nil || update.SteamLoginKey != nil {
		steamNode := findOrCreateMappingKey(sectionNode, "steam")
		if steamNode != nil && steamNode.Kind == yaml.MappingNode {
			if update.SteamSessionTicket != nil {
				setOrUpdateScalar(steamNode, "session_ticket", *update.SteamSessionTicket)
			}
			if update.SteamID64 != nil {
				setOrUpdateScalar(steamNode, "steam_id_64", *update.SteamID64)
			}
			if update.SteamLoginKey != nil {
				setOrUpdateScalar(steamNode, "login_key", *update.SteamLoginKey)
			}
		}
	}
}

func findOrCreateMappingKey(mappingNode *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mappingNode.Content)-1; i += 2 {
		keyNode := mappingNode.Content[i]
		valNode := mappingNode.Content[i+1]
		if keyNode.Value == key {
			return valNode
		}
	}

	// Create new entry
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	mappingNode.Content = append(mappingNode.Content, keyNode, valNode)
	return valNode
}

func setOrUpdateScalar(mappingNode *yaml.Node, key, value string) {
	for i := 0; i < len(mappingNode.Content)-1; i += 2 {
		keyNode := mappingNode.Content[i]
		valNode := mappingNode.Content[i+1]
		if keyNode.Value == key {
			valNode.Value = value
			valNode.Tag = "!!str"
			return
		}
	}

	// Not found: append
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
	mappingNode.Content = append(mappingNode.Content, keyNode, valNode)
}

func saveTokensToJSON(filePath string, data []byte, updates TokenUpdates) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}

	applyJSONMapUpdates(raw, "auth", updates.Primary)
	applyJSONMapUpdates(raw, "polling_auth", updates.Polling)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, append(out, '\n'), 0644)
}

func applyJSONMapUpdates(root map[string]interface{}, sectionKey string, update AuthTokenUpdate) {
	secRaw, ok := root[sectionKey]
	var secMap map[string]interface{}
	if ok && secRaw != nil {
		if sm, ok := secRaw.(map[string]interface{}); ok {
			secMap = sm
		}
	}
	if secMap == nil {
		secMap = make(map[string]interface{})
		root[sectionKey] = secMap
	}

	if update.EpicRefreshToken != nil {
		epicMap, _ := secMap["epic"].(map[string]interface{})
		if epicMap == nil {
			epicMap = make(map[string]interface{})
			secMap["epic"] = epicMap
		}
		epicMap["refresh_token"] = *update.EpicRefreshToken
	}

	if update.SteamSessionTicket != nil || update.SteamID64 != nil || update.SteamLoginKey != nil {
		steamMap, _ := secMap["steam"].(map[string]interface{})
		if steamMap == nil {
			steamMap = make(map[string]interface{})
			secMap["steam"] = steamMap
		}
		if update.SteamSessionTicket != nil {
			steamMap["session_ticket"] = *update.SteamSessionTicket
		}
		if update.SteamID64 != nil {
			steamMap["steam_id_64"] = *update.SteamID64
		}
		if update.SteamLoginKey != nil {
			steamMap["login_key"] = *update.SteamLoginKey
		}
	}
}
