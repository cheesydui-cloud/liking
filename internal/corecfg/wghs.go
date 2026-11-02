package corecfg

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"time"

	"golang.org/x/crypto/blake2s"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	wgNoiseConstruction = "Noise_IKpsk2_25519_ChaChaPoly_BLAKE2s"
	wgIdentifier        = "WireGuard v1 zx2c4 Jason@zx2c4.com"
	wgLabelMAC1         = "mac1----"
	wgInitiationSize    = 148
	wgResponseSize      = 92
	wgCookieReplySize   = 64
	wgTAI64Base         = uint64(0x400000000000000a)
)

var wgZeroNonce = make([]byte, chacha20poly1305.NonceSize)

// WireGuardInitiation builds a 148-byte handshake initiation from a known
// peer private key. The hub answers only when that peer is configured.
func WireGuardInitiation(peerPriv, serverPub string) ([]byte, error) {
	priv, err := decodeWGKey(peerPriv)
	if err != nil {
		return nil, err
	}
	pub, err := decodeWGKey(serverPub)
	if err != nil {
		return nil, err
	}
	ephRaw := make([]byte, 32)
	if _, err := rand.Read(ephRaw); err != nil {
		return nil, err
	}
	clampWG(ephRaw)
	ephPub, err := wgPublic(ephRaw)
	if err != nil {
		return nil, err
	}
	peerPub, err := wgPublic(priv)
	if err != nil {
		return nil, err
	}

	chain := blake2s.Sum256([]byte(wgNoiseConstruction))
	sum := mixWGHash(chain[:], []byte(wgIdentifier))
	sum = mixWGHash(sum[:], pub)

	chain = kdf1(chain[:], ephPub)
	sum = mixWGHash(sum[:], ephPub)

	ss, err := wgDH(ephRaw, pub)
	if err != nil {
		return nil, err
	}
	var key [32]byte
	chain, key = kdf2(chain[:], ss)
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, err
	}
	staticCT := aead.Seal(nil, wgZeroNonce, peerPub, sum[:])
	sum = mixWGHash(sum[:], staticCT)

	ss, err = wgDH(priv, pub)
	if err != nil {
		return nil, err
	}
	chain, key = kdf2(chain[:], ss)
	aead, err = chacha20poly1305.New(key[:])
	if err != nil {
		return nil, err
	}
	tsCT := aead.Seal(nil, wgZeroNonce, wgTimestamp(time.Now()), sum[:])
	if len(staticCT) != 48 || len(tsCT) != 28 {
		return nil, fmt.Errorf("WireGuard 握手长度无效")
	}

	msg := make([]byte, wgInitiationSize)
	binary.LittleEndian.PutUint32(msg[0:4], 1)
	sender := make([]byte, 4)
	if _, err := rand.Read(sender); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(sender) == 0 {
		sender[0] = 1
	}
	copy(msg[4:8], sender)
	copy(msg[8:40], ephPub)
	copy(msg[40:88], staticCT)
	copy(msg[88:116], tsCT)
	mac, err := wgMAC1(pub, msg)
	if err != nil {
		return nil, err
	}
	copy(msg[116:132], mac)
	return msg, nil
}

// WireGuardReplyOK reports a handshake response or a cookie reply.
func WireGuardReplyOK(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	switch binary.LittleEndian.Uint32(b[:4]) {
	case 2:
		return len(b) >= wgResponseSize
	case 3:
		return len(b) >= wgCookieReplySize
	default:
		return false
	}
}

func decodeWGKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("WireGuard 密钥无效")
	}
	return b, nil
}

func wgPublic(scalar []byte) ([]byte, error) {
	key, err := ecdh.X25519().NewPrivateKey(scalar)
	if err != nil {
		return nil, fmt.Errorf("WireGuard 密钥无效")
	}
	return key.PublicKey().Bytes(), nil
}

func wgDH(scalar, peer []byte) ([]byte, error) {
	key, err := ecdh.X25519().NewPrivateKey(scalar)
	if err != nil {
		return nil, fmt.Errorf("WireGuard 密钥无效")
	}
	pub, err := ecdh.X25519().NewPublicKey(peer)
	if err != nil {
		return nil, fmt.Errorf("WireGuard 密钥无效")
	}
	ss, err := key.ECDH(pub)
	if err != nil || wgAllZero(ss) {
		return nil, fmt.Errorf("WireGuard 密钥无效")
	}
	return ss, nil
}

func wgAllZero(b []byte) bool {
	n := byte(0)
	for _, x := range b {
		n |= x
	}
	return n == 0
}

func mixWGHash(h, data []byte) [32]byte {
	mac, _ := blake2s.New256(nil)
	_, _ = mac.Write(h)
	_, _ = mac.Write(data)
	var out [32]byte
	mac.Sum(out[:0])
	return out
}

func wgHMAC(key []byte, parts ...[]byte) [32]byte {
	m := hmac.New(func() hash.Hash {
		h, _ := blake2s.New256(nil)
		return h
	}, key)
	for _, p := range parts {
		_, _ = m.Write(p)
	}
	var out [32]byte
	m.Sum(out[:0])
	return out
}

func kdf1(key, input []byte) [32]byte {
	prk := wgHMAC(key, input)
	return wgHMAC(prk[:], []byte{0x1})
}

func kdf2(key, input []byte) (t0, t1 [32]byte) {
	prk := wgHMAC(key, input)
	t0 = wgHMAC(prk[:], []byte{0x1})
	t1 = wgHMAC(prk[:], t0[:], []byte{0x2})
	return t0, t1
}

func wgMAC1(serverPub, msg []byte) ([]byte, error) {
	if len(serverPub) != 32 || len(msg) < 32 {
		return nil, fmt.Errorf("WireGuard 握手长度无效")
	}
	sum := blake2s.Sum256(append([]byte(wgLabelMAC1), serverPub...))
	mac, err := blake2s.New128(sum[:])
	if err != nil {
		return nil, err
	}
	_, _ = mac.Write(msg[:len(msg)-32])
	return mac.Sum(nil), nil
}

func wgMAC1OK(serverPub string, msg []byte) bool {
	pub, err := decodeWGKey(serverPub)
	if err != nil || len(msg) < 32 {
		return false
	}
	want, err := wgMAC1(pub, msg)
	if err != nil || len(want) != 16 {
		return false
	}
	return subtle.ConstantTimeCompare(want, msg[len(msg)-32:len(msg)-16]) == 1
}

func wgTimestamp(now time.Time) []byte {
	b := make([]byte, 12)
	sec := now.Unix()
	if sec < 0 {
		sec = 0
	}
	nano := now.Nanosecond()
	if nano < 0 {
		nano = 0
	}
	binary.BigEndian.PutUint64(b[:8], wgTAI64Base+uint64(sec))
	binary.BigEndian.PutUint32(b[8:], uint32(nano))
	return b
}

// wgInitiationPeer opens an initiation with the hub private key and returns
// the peer public key. Tests use it to check the handshake layout.
func wgInitiationPeer(serverPriv string, msg []byte) (string, error) {
	if len(msg) != wgInitiationSize || binary.LittleEndian.Uint32(msg[:4]) != 1 {
		return "", fmt.Errorf("不是握手发起")
	}
	priv, err := decodeWGKey(serverPriv)
	if err != nil {
		return "", err
	}
	serverPub, err := wgPublic(priv)
	if err != nil {
		return "", err
	}
	if !wgMAC1OK(base64.StdEncoding.EncodeToString(serverPub), msg) {
		return "", fmt.Errorf("mac1")
	}
	chain := blake2s.Sum256([]byte(wgNoiseConstruction))
	sum := mixWGHash(chain[:], []byte(wgIdentifier))
	sum = mixWGHash(sum[:], serverPub)
	eph := msg[8:40]
	chain = kdf1(chain[:], eph)
	sum = mixWGHash(sum[:], eph)
	ss, err := wgDH(priv, eph)
	if err != nil {
		return "", err
	}
	var key [32]byte
	chain, key = kdf2(chain[:], ss)
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return "", err
	}
	peerPub, err := aead.Open(nil, wgZeroNonce, msg[40:88], sum[:])
	if err != nil || len(peerPub) != 32 {
		return "", fmt.Errorf("静态密钥解不开")
	}
	sum = mixWGHash(sum[:], msg[40:88])
	ss, err = wgDH(priv, peerPub)
	if err != nil {
		return "", err
	}
	_, key = kdf2(chain[:], ss)
	aead, err = chacha20poly1305.New(key[:])
	if err != nil {
		return "", err
	}
	if _, err := aead.Open(nil, wgZeroNonce, msg[88:116], sum[:]); err != nil {
		return "", fmt.Errorf("时间戳解不开")
	}
	return base64.StdEncoding.EncodeToString(peerPub), nil
}
