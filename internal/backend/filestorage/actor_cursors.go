package filestorage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const actorCursorsFilename = "cursors.yaml"

type actorCursorsRepository struct {
	dir string
}

type cursorsFile struct {
	Cursors map[string]string `yaml:"cursors"`
}

func (r *actorCursorsRepository) Get(actor string) (time.Time, error) {
	cf, err := readCursorsFile(r.dir)
	if err != nil {
		return time.Time{}, err
	}
	raw, ok := cf.Cursors[actor]
	if !ok {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cursor for %q: %w", actor, err)
	}
	return ts, nil
}

func (r *actorCursorsRepository) Set(actor string, ts time.Time) error {
	cf, err := readCursorsFile(r.dir)
	if err != nil {
		return err
	}
	if cf.Cursors == nil {
		cf.Cursors = map[string]string{}
	}
	cf.Cursors[actor] = ts.UTC().Format(time.RFC3339Nano)
	return writeCursorsFile(r.dir, cf)
}

func readCursorsFile(dir string) (*cursorsFile, error) {
	path := filepath.Join(dir, actorCursorsFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &cursorsFile{Cursors: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("read cursors file: %w", err)
	}
	var cf cursorsFile
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("parse cursors file: %w", err)
	}
	if cf.Cursors == nil {
		cf.Cursors = map[string]string{}
	}
	return &cf, nil
}

func writeCursorsFile(dir string, cf *cursorsFile) error {
	raw, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("marshal cursors file: %w", err)
	}
	path := filepath.Join(dir, actorCursorsFilename)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write cursors file: %w", err)
	}
	return nil
}
