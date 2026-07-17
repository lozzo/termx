package client_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientPackagesDoNotDependOnUIOrCommandOwners(t *testing.T) {
	for _, root := range []string{"endpoint", "runtime", "port", "adapter"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				for _, forbidden := range []string{"github.com/lozzow/termx/tui/", "github.com/lozzow/termx/cmd/termx", "github.com/lozzow/termx/private/"} {
					if strings.Contains(line, forbidden) {
						t.Errorf("%s imports forbidden owner %s", path, forbidden)
					}
				}
			}
			return scanner.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
