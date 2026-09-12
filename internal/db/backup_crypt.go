package db

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

const (
	BackupMagic   = "LKB1"
	backupSaltLen = 16
	backupNonce   = 12
	scryptN       = 1 << 15
	scryptR       = 8
	scryptP       = 1
	scryptKeyLen  = 32
)

func EncryptBackup(plain []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, fmt.Errorf("加密密码不能为空")
	}
	salt := make([]byte, backupSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, backupNonce)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, 4+backupSaltLen+backupNonce+len(ct))
	out = append(out, BackupMagic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func DecryptBackup(raw []byte, password string) ([]byte, error) {
	if len(raw) < 4+backupSaltLen+backupNonce+16 {
		return nil, fmt.Errorf("加密备份损坏")
	}
	if string(raw[:4]) != BackupMagic {
		return nil, fmt.Errorf("不是加密备份")
	}
	if password == "" {
		return nil, fmt.Errorf("需要备份密码")
	}
	salt := raw[4 : 4+backupSaltLen]
	nonce := raw[4+backupSaltLen : 4+backupSaltLen+backupNonce]
	ct := raw[4+backupSaltLen+backupNonce:]
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("备份密码不对或文件损坏")
	}
	return plain, nil
}

func IsEncryptedBackup(raw []byte) bool {
	return len(raw) >= 4 && string(raw[:4]) == BackupMagic
}

func DecodeSnapshotMaybeEncrypted(r io.Reader, password string) (*Snapshot, error) {
	br := bufio.NewReader(r)
	magic, _ := br.Peek(4)
	if IsEncryptedBackup(magic) {
		raw, err := io.ReadAll(io.LimitReader(br, maxBackupJSON+1<<20))
		if err != nil {
			return nil, err
		}
		plain, err := DecryptBackup(raw, password)
		if err != nil {
			return nil, err
		}
		return DecodeSnapshot(bytes.NewReader(plain))
	}
	return DecodeSnapshot(br)
}
