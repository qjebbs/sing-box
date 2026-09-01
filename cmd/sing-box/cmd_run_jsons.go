package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
	"github.com/pelletier/go-toml"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	myjson "github.com/sagernet/sing/common/json"

	"github.com/qjebbs/go-jsons"
	"github.com/qjebbs/go-jsons/preprocess"
)

// Formats and extensions.
var (
	merger *jsons.Merger

	formatJSON jsons.Format = "json"
	formatYAML jsons.Format = "yaml"
	formatTOML jsons.Format = "toml"

	extJSON = []string{".json", ".jsonc"}
	extYAML = []string{".yaml", ".yml"}
	extTOML = []string{".toml"}
)

func mergeOptionsListExtended(optionsList []*OptionsEntry) (option.Options, error) {
	contents := common.Map(optionsList, func(e *OptionsEntry) []byte {
		return e.content
	})
	merged, err := merger.Merge(contents)
	if err != nil {
		return option.Options{}, err
	}
	var options option.Options
	err = options.UnmarshalJSONContext(globalCtx, merged)
	if err != nil {
		return option.Options{}, E.Cause(err, "unmarshal merged config")
	}
	return options, nil
}

// Contents merges files content into a single json.
func Contents(contents ...[]byte) ([]byte, error) {
	return merger.Merge(contents)
}

// Extensions returns all supported extensions.
func Extensions() []string {
	return append(append(extJSON, extYAML...), extTOML...)
}

// NewMerger creates a new json files Merger.
func init() {
	merger = jsons.NewMerger(
		jsons.WithMergeBy("tag"),
		jsons.WithMergeByAndRemove("_tag"),
		jsons.WithOrderByAndRemove("_order"),
		jsons.WithIndent("", "  "),
		jsons.WithPreprocessor(preprocess.ExpandTypedEnv),
	)
	merger.RegisterOrderedLoader(
		formatJSON,
		extJSON,
		func(b []byte) (*jsons.OrderedMap, error) {
			m := jsons.NewOrderedMap()
			decoder := json.NewDecoder(myjson.NewCommentFilter(bytes.NewReader(b)))
			err := decoder.Decode(m)
			if err != nil {
				return nil, err
			}
			return m, nil
		},
	)
	merger.RegisterOrderedLoader(
		formatYAML,
		extYAML,
		func(b []byte) (*jsons.OrderedMap, error) {
			m := jsons.NewOrderedMap()
			err := yaml.UnmarshalWithOptions(b, m, yaml.UseJSONUnmarshaler())
			if err != nil {
				return nil, err
			}
			return m, nil
		},
	)
	merger.RegisterLoader(
		formatTOML,
		extTOML,
		func(b []byte) (map[string]interface{}, error) {
			m := make(map[string]interface{})
			err := toml.Unmarshal(b, &m)
			if err != nil {
				return nil, err
			}
			return m, nil
		},
	)
}

func readConfigExtended() ([]*OptionsEntry, error) {
	var optionsList []*OptionsEntry
	extensions := Extensions()
	for _, path := range configPaths {
		if !common.Contains(extensions, filepath.Ext(path)) {
			return nil, E.New("unsupported file extension: ", path)
		}
		optionsEntry, err := readConfigAt(path)
		if err != nil {
			return nil, err
		}
		optionsList = append(optionsList, optionsEntry)
	}
	for _, dir := range configDirectories {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, E.Cause(err, "read config directory at ", dir)
		}
		var list []*OptionsEntry
		for _, entry := range entries {
			if entry.IsDir() || !common.Contains(extensions, filepath.Ext(entry.Name())) {
				continue
			}
			optionsEntry, err := readConfigAt(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			list = append(list, optionsEntry)
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].path < list[j].path
		})
		optionsList = append(optionsList, list...)
	}
	return optionsList, nil
}
