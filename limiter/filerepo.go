package limiter

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type FileRepository struct {
	path string
}

func NewFileRepository(path string) *FileRepository {
	return &FileRepository{path: path}
}

func (r *FileRepository) Load() ([]Rule, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *FileRepository) Save(rules []Rule) error {
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })

	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(r.path), ".rules-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, r.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
