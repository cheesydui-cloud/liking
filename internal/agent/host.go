package agent

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func snapshotDisk() (free, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, 0
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		return 0, 0
	}
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize
}

func snapshotMem() (avail, total int64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	return parseMeminfo(b)
}

func parseMeminfo(b []byte) (avail, total int64) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = memKB(line) * 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = memKB(line) * 1024
		}
	}
	return avail, total
}

func memKB(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(fields[1], 10, 64)
	return n
}

func snapshotLoadMilli() int64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	return parseLoadMilli(b)
}

func parseLoadMilli(b []byte) int64 {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(f * 1000)
}

func countEstablished(ports []int) int {
	if len(ports) == 0 {
		return 0
	}
	want := map[int]struct{}{}
	for _, p := range ports {
		if p > 0 {
			want[p] = struct{}{}
		}
	}
	n := 0
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		n += parseTCPEstablished(b, want)
	}
	return n
}

func parseTCPEstablished(b []byte, want map[int]struct{}) int {
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(b))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !strings.EqualFold(fields[3], "01") {
			continue
		}
		port := hexPort(fields[1])
		if _, ok := want[port]; ok {
			n++
		}
	}
	return n
}

func hexPort(local string) int {
	i := strings.LastIndexByte(local, ':')
	if i < 0 || i+1 >= len(local) {
		return 0
	}
	n, err := strconv.ParseInt(local[i+1:], 16, 64)
	if err != nil {
		return 0
	}
	return int(n)
}
