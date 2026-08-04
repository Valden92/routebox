package subscription

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func DeleteCache(dataDir, subscriptionID string) error {
	path := filepath.Join(dataDir, "subscriptions", subscriptionID+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
