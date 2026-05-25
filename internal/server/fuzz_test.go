package server

import "testing"

func FuzzValidatePath(f *testing.F) {
	f.Add("/var/www", "/index.html")
	f.Add("/var/www", "/../secret")
	f.Add("/var/www", "/static/%2e%2e/secret")
	f.Add("/var/www", "/.git/config")

	f.Fuzz(func(t *testing.T, root, reqPath string) {
		if len(root) > 256 || len(reqPath) > 2048 {
			t.Skip()
		}

		_, _ = validatePath(root, reqPath, []string{".git", ".env", ".htaccess"})
	})
}
