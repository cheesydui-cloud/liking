package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/http2"
)

const v2rayStatsPath = "/v2ray.core.app.stats.command.StatsService/QueryStats"

// queryV2RayStatsFn is replaced in tests. Production queries sing-box
// experimental.v2ray_api over h2c. xray api statsquery cannot speak this
// service name, and the call must not reset counters: a failed stats frame
// would otherwise drop the interval.
var queryV2RayStatsFn = queryV2RayStats

func queryV2RayStats(ctx context.Context, addr string) ([]statRow, error) {
	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+v2rayStatsPath, bytes.NewReader([]byte{0, 0, 0, 0, 0}))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("Te", "trailers")
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 4<<20 {
		return nil, fmt.Errorf("统计响应过大")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("统计口 HTTP %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/grpc") {
		return nil, fmt.Errorf("统计口不是 gRPC")
	}
	st := res.Trailer.Get("Grpc-Status")
	if st == "" {
		st = res.Header.Get("Grpc-Status")
	}
	if st != "" && st != "0" {
		msg := res.Trailer.Get("Grpc-Message")
		if msg == "" {
			msg = res.Header.Get("Grpc-Message")
		}
		return nil, fmt.Errorf("grpc %s %s", st, msg)
	}
	if len(raw) == 0 {
		if st == "0" {
			return nil, nil
		}
		return nil, fmt.Errorf("统计响应为空")
	}
	return decodeGRPCStats(raw)
}

func decodeGRPCStats(body []byte) ([]statRow, error) {
	var all []statRow
	for len(body) > 0 {
		if len(body) < 5 {
			return nil, fmt.Errorf("grpc 帧不完整")
		}
		if body[0] != 0 {
			return nil, fmt.Errorf("grpc 压缩未支持")
		}
		n := int(binary.BigEndian.Uint32(body[1:5]))
		body = body[5:]
		if n < 0 || n > 4<<20 || n > len(body) {
			return nil, fmt.Errorf("grpc 消息长度异常")
		}
		rows, err := decodeQueryStats(body[:n])
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		body = body[n:]
	}
	return all, nil
}

// decodeQueryStats reads QueryStatsResponse: field 1 is repeated Stat
// (name string field 1, value int64 field 2).
func decodeQueryStats(msg []byte) ([]statRow, error) {
	var rows []statRow
	for len(msg) > 0 {
		key, n, err := decodeVarint(msg)
		if err != nil {
			return nil, err
		}
		msg = msg[n:]
		field, wire := int(key>>3), int(key&7)
		switch {
		case field == 1 && wire == 2:
			ln, n, err := decodeVarint(msg)
			if err != nil {
				return nil, err
			}
			msg = msg[n:]
			if ln > uint64(len(msg)) {
				return nil, fmt.Errorf("stat 长度异常")
			}
			row, err := decodeStat(msg[:ln])
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
			msg = msg[ln:]
		default:
			msg, err = skipProto(msg, wire)
			if err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

func decodeStat(msg []byte) (statRow, error) {
	var row statRow
	for len(msg) > 0 {
		key, n, err := decodeVarint(msg)
		if err != nil {
			return row, err
		}
		msg = msg[n:]
		field, wire := int(key>>3), int(key&7)
		switch {
		case field == 1 && wire == 2:
			ln, n, err := decodeVarint(msg)
			if err != nil {
				return row, err
			}
			msg = msg[n:]
			if ln > uint64(len(msg)) {
				return row, fmt.Errorf("name 长度异常")
			}
			row.Name = string(msg[:ln])
			msg = msg[ln:]
		case field == 2 && wire == 0:
			v, n, err := decodeVarint(msg)
			if err != nil {
				return row, err
			}
			msg = msg[n:]
			if v > 1<<63-1 {
				return row, fmt.Errorf("counter 超出范围")
			}
			row.Value = int64(v)
		default:
			msg, err = skipProto(msg, wire)
			if err != nil {
				return row, err
			}
		}
	}
	return row, nil
}

func decodeVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("varint 无效")
}

func skipProto(msg []byte, wire int) ([]byte, error) {
	switch wire {
	case 0:
		_, n, err := decodeVarint(msg)
		if err != nil {
			return nil, err
		}
		return msg[n:], nil
	case 1:
		if len(msg) < 8 {
			return nil, fmt.Errorf("proto 截断")
		}
		return msg[8:], nil
	case 2:
		ln, n, err := decodeVarint(msg)
		if err != nil {
			return nil, err
		}
		msg = msg[n:]
		if ln > uint64(len(msg)) {
			return nil, fmt.Errorf("proto 截断")
		}
		return msg[ln:], nil
	case 5:
		if len(msg) < 4 {
			return nil, fmt.Errorf("proto 截断")
		}
		return msg[4:], nil
	default:
		return nil, fmt.Errorf("proto wire %d", wire)
	}
}
