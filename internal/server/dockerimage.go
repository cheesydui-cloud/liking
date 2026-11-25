package server

import (
	"bytes"
	"os"
	"path/filepath"
)

// managedDockerfileMarker marks the Dockerfile this project writes. A file
// without the marker is left alone so a hand-edited image is not overwritten.
const managedDockerfileMarker = "liking-managed-dockerfile"

// managedDockerfile is the image used by install.sh docker and by later
// in-container upgrades. It is scratch on purpose: the build must not pull a
// base image. The CA bundle is copied to the path Go's Linux cert pool reads.
const managedDockerfile = `# liking-managed-dockerfile
FROM scratch
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY liking-server /usr/local/bin/liking-server
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
EXPOSE 8899
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/liking-server"]
CMD ["--addr", "0.0.0.0:8899", "--db", "/var/lib/liking/panel.db"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 CMD ["/usr/local/bin/liking-server", "--health", "http://127.0.0.1:8899/healthz"]
`

func ensureManagedDockerfile(dir string) error {
	path := filepath.Join(dir, "Dockerfile")
	want := []byte(managedDockerfile)
	b, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(b)) > 0 && !bytes.Contains(b, []byte(managedDockerfileMarker)) {
		return nil
	}
	return os.WriteFile(path, want, 0o644)
}

func ensureCABundle(dir string) error {
	dst := filepath.Join(dir, "ca-certificates.crt")
	if st, err := os.Stat(dst); err == nil && !st.IsDir() && st.Size() > 1000 {
		return nil
	}
	for _, src := range []string{
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/ssl/cert.pem",
		"/etc/pki/tls/certs/ca-bundle.crt",
		"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
	} {
		st, err := os.Stat(src)
		if err != nil || st.IsDir() || st.Size() < 1000 {
			continue
		}
		if err := copyFileMode(src, dst, 0o644); err == nil {
			return nil
		}
	}
	return os.ErrNotExist
}
