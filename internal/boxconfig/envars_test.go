package boxconfig

// envars.yaml: the box's named env entries, written whole by the API and read back as a map.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheEnvMapFile(t *testing.T) {
	cases := []struct {
		name    string
		on      string
		check   func(path string) error
		wantErr bool
	}{
		{"an absent envars.yaml is an empty map", "", func(path string) error {
			envs, err := LoadEnvars(path)
			if err == nil && len(envs) != 0 {
				t.Errorf("got %v, want an empty map", envs)
			}
			return err
		}, false},
		{"a malformed one is an error naming the file, not an empty map", "stg: [not, a, map\n", func(path string) error {
			_, err := LoadEnvars(path)
			return err
		}, true},
		{"a write over a malformed file is refused, the file left for a person to fix", "stg: [not, a, map\n", func(path string) error {
			err := PutEnvar(path, "prod", map[string]string{"API_URL": "https://api.example.com"})
			if raw, _ := os.ReadFile(path); string(raw) != "stg: [not, a, map\n" {
				t.Errorf("the file was rewritten:\n%s", raw)
			}
			return err
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "envars.yaml")
			if c.on != "" {
				if err := os.WriteFile(path, []byte(c.on), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			err := c.check(path)

			if (err != nil) != c.wantErr || (err != nil && !strings.Contains(err.Error(), path)) {
				t.Errorf("err = %v, want an error naming %s: %v", err, path, c.wantErr)
			}
		})
	}
}
