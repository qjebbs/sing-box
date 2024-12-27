package remote

import (
	"os"
)

func saveCache(file string, c *fileContent) error {
	w, err := os.Create(file)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.WriteString(c.raw)
	if err != nil {
		return err
	}
	return nil
}

func saveCacheIfNeed(file string, content *fileContent) error {
	if content.links == "" {
		return nil
	}
	saved, _ := loadCache(file)
	if saved == nil || saved.linksHash != content.linksHash {
		return saveCache(file, content)
	}
	return nil
}

func loadCache(file string) (*fileContent, error) {
	stat, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return parseFileContent(string(content), stat.ModTime())
}
