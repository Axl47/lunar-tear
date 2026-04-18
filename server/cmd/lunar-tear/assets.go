package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateAssets(root string) error {
	listPath := filepath.Join(root, "assets", "revisions", "0", "list.bin")
	releaseGlob := filepath.Join(root, "assets", "release", "*.bin.e")

	var problems []string

	info, err := os.Stat(listPath)
	if err != nil || info.IsDir() {
		problems = append(problems, fmt.Sprintf("missing required asset list: %s", filepath.Clean(listPath)))
	}

	matches, globErr := filepath.Glob(releaseGlob)
	if globErr != nil {
		problems = append(problems, fmt.Sprintf("invalid release glob %q: %v", filepath.Clean(releaseGlob), globErr))
	} else if len(matches) == 0 {
		problems = append(problems, fmt.Sprintf("missing required master data matching: %s", filepath.Clean(releaseGlob)))
	}

	if len(problems) == 0 {
		return nil
	}

	return errors.New(strings.Join(problems, "; "))
}
