package server

import (
	"bytes"
	"io"
	"net/url"
	"os"
	"strings"
)

// agentBinDownloadURL is relative on purpose. Agents since 0.1.32 resolve it
// against their own --connect host. An absolute panel_url often differs
// (domain vs IP, localhost, or a later settings change) and every node then
// rejects the download with 下载地址与面板不一致.
func agentBinDownloadURL(osName, arch string) string {
	q := url.Values{}
	q.Set("os", osName)
	q.Set("arch", arch)
	return "/v1/agent-bin?" + q.Encode()
}

func agentBinaryHasVersion(path, want string) (bool, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 80<<20))
	if err != nil {
		return false, err
	}
	return bytesContainVersion(b, want), nil
}

func bytesContainVersion(b []byte, want string) bool {
	needle := []byte(want)
	if len(needle) == 0 {
		return false
	}
	for from := 0; from < len(b); {
		i := bytes.Index(b[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(needle)
		leftOK := i == 0 || !isVersionByte(b[i-1])
		rightOK := end >= len(b) || !isVersionByte(b[end])
		if leftOK && rightOK {
			return true
		}
		from = i + 1
	}
	return false
}

func isVersionByte(c byte) bool {
	return c == '.' || (c >= '0' && c <= '9')
}
