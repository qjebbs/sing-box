package jsonsmerge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
	"github.com/pelletier/go-toml"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	myjson "github.com/sagernet/sing/common/json"

	"github.com/qjebbs/go-jsons"
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

// Files merges files into a single json.
func Files(files, dirs []string) ([]byte, error) {
	return merger.Merge(allFiles(files, dirs))
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

func allFiles(files, dirs []string) ([]string, error) {
	extensions := Extensions()
	all := make([]string, len(files))
	for i, file := range files {
		if !common.Contains(extensions, filepath.Ext(file)) {
			return nil, E.New("unsupported file extension: ", file)
		}
		all[i] = file
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, E.Cause(err, "read config directory at ", dir)
		}
		for _, entry := range entries {
			if entry.IsDir() || !common.Contains(extensions, filepath.Ext(entry.Name())) {
				continue
			}
			all = append(all, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i] < all[j]
	})
	return all, nil
}
