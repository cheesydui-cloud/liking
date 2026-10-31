package agent

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestQueryV2RayStatsRoundTrip(t *testing.T) {
	msg := encodeQueryStats([]statRow{
		{Name: "user>>>u1.i2>>>traffic>>>uplink", Value: 100},
		{Name: "user>>>u1.i2>>>traffic>>>downlink", Value: 250},
		{Name: "inbound>>>in-2>>>traffic>>>uplink", Value: 9},
	})
	frame := make([]byte, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(msg)))
	copy(frame[5:], msg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &http2.Server{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.ServeConn(conn, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != v2rayStatsPath || r.Header.Get("Content-Type") != "application/grpc" {
					http.Error(w, "bad grpc", http.StatusBadRequest)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/grpc")
				w.Header().Set("Trailer", "Grpc-Status")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(frame)
				w.Header().Set("Grpc-Status", "0")
			})})
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := queryV2RayStats(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	got := samplesFromStatRows(rows)
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 100 || got[0].Down != 250 {
		t.Fatalf("samples %+v rows %+v", got, rows)
	}
}

func TestDecodeQueryStatsEmpty(t *testing.T) {
	rows, err := decodeGRPCStats([]byte{0, 0, 0, 0, 0})
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows %+v err %v", rows, err)
	}
}

func encodeQueryStats(rows []statRow) []byte {
	var msg []byte
	for _, row := range rows {
		stat := appendProtoString(nil, 1, row.Name)
		stat = appendProtoVarint(stat, 2, uint64(row.Value))
		msg = appendProtoBytes(msg, 1, stat)
	}
	return msg
}

func appendProtoString(dst []byte, field int, s string) []byte {
	dst = appendProtoKey(dst, field, 2)
	dst = appendVarint(dst, uint64(len(s)))
	return append(dst, s...)
}

func appendProtoBytes(dst []byte, field int, b []byte) []byte {
	dst = appendProtoKey(dst, field, 2)
	dst = appendVarint(dst, uint64(len(b)))
	return append(dst, b...)
}

func appendProtoVarint(dst []byte, field int, v uint64) []byte {
	dst = appendProtoKey(dst, field, 0)
	return appendVarint(dst, v)
}

func appendProtoKey(dst []byte, field, wire int) []byte {
	return appendVarint(dst, uint64(field<<3|wire))
}

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}
