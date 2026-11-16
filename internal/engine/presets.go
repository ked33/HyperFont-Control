package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Presets use one atomic catalog so a backup merge never exposes half its schemes.
type Preset struct {
	ID      string  `json:"id"`
	Profile Profile `json:"profile"`
	Created string  `json:"created"`
	Updated string  `json:"updated"`
}

type presetCatalog struct {
	Version  int      `json:"version"`
	Revision string   `json:"revision"`
	Items    []Preset `json:"items"`
}

func catalogBytes(items []Preset) ([]byte, error) {
	return json.MarshalIndent(presetCatalog{1, token(), items}, "", "  ")
}

func (s *Store) writePresets(items []Preset) error {
	b, err := catalogBytes(items)
	if err != nil {
		return err
	}
	if len(b) > maxManifestSize {
		return fmt.Errorf("方案目录过大")
	}
	return writeAtomic(s.data("presets.json"), b, 0600)
}

func (s *Store) Presets() ([]Preset, error) {
	items := []Preset{}
	var catalog presetCatalog
	err := readJSON(s.data("presets.json"), &catalog)
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if catalog.Version != 1 || !tokenPattern.MatchString(catalog.Revision) {
		return nil, fmt.Errorf("方案目录版本无效")
	}
	items = catalog.Items
	seen := map[string]bool{}
	for _, p := range items {
		if !tokenPattern.MatchString(p.ID) || seen[p.ID] {
			return nil, fmt.Errorf("方案目录无效")
		}
		seen[p.ID] = true
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Updated > items[j].Updated })
	return items, nil
}

func (s *Store) SavePreset(id string, profile Profile) (Preset, error) {
	var result Preset
	if _, err := s.Validate(profile); err != nil {
		return result, err
	}
	items, err := s.Presets()
	if err != nil {
		return result, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if id == "" {
		result = Preset{token(), profile, now, now}
		items = append(items, result)
	} else {
		found := false
		for i := range items {
			if items[i].ID == id {
				items[i].Profile, items[i].Updated = profile, now
				result, found = items[i], true
			}
		}
		if !found {
			return result, fmt.Errorf("方案不存在，请刷新")
		}
	}
	return result, s.writePresets(items)
}

func (s *Store) preset(id string) (Preset, error) {
	items, err := s.Presets()
	if err != nil {
		return Preset{}, err
	}
	for _, p := range items {
		if p.ID == id {
			return p, nil
		}
	}
	return Preset{}, fmt.Errorf("方案不存在，请刷新")
}

func (s *Store) DeletePreset(id string) (any, error) {
	items, err := s.Presets()
	if err != nil {
		return nil, err
	}
	for i, p := range items {
		if p.ID == id {
			items = append(items[:i], items[i+1:]...)
			return map[string]bool{"deleted": true}, s.writePresets(items)
		}
	}
	return nil, fmt.Errorf("方案不存在，请刷新")
}

func profileAssets(p Profile) map[string]bool {
	ids := map[string]bool{}
	for _, slot := range profileSlots(p) {
		for _, face := range slotReferences(slot) {
			ids[face.AssetID] = true
		}
	}
	return ids
}

func sameProfile(a, b Profile) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
