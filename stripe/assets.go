package stripe

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
)

//go:embed frontend/dist
var assetsFS embed.FS

// assetsSubFS returns a sub-filesystem rooted at frontend/dist so that paths
// served by the asset router do not include the "frontend/dist" prefix.
func assetsSubFS() http.FileSystem {
	sub, _ := fs.Sub(assetsFS, "frontend/dist")
	return http.FS(sub)
}

// sriHash computes the SHA-256 SRI hash for the given embedded file.
// The returned string has the format "sha256-<base64>".
func sriHash(name string) string {
	data, err := assetsFS.ReadFile(name)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}
