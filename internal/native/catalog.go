package native

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func LoadCatalog(bundle string) ([]Pack, error) {
	raw, e := os.ReadFile(filepath.Join(bundle, "models", "catalog.json"))
	if e != nil {
		return nil, e
	}
	var packs []Pack
	if e = json.Unmarshal(raw, &packs); e != nil {
		return nil, e
	}
	for _, p := range packs {
		if e = p.Validate(); e != nil {
			return nil, e
		}
	}
	return packs, nil
}
