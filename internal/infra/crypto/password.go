package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// DefaultArgon2Params is the v1 cost profile.
var DefaultArgon2Params = Argon2Params{Memory: 64 * 1024, Time: 3, Threads: 2, KeyLen: 32, SaltLen: 16}

// HashPassword hashes password with Argon2id and returns a PHC-style encoded
// string that embeds the cost parameters and salt. It rejects out-of-range
// parameters, which would otherwise panic Argon2 or exhaust memory.
func HashPassword(password string, p Argon2Params) (string, error) {
	if !validParams(p) {
		return "", ErrInvalidParams
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return encodeHash(p, salt, hash), nil
}

// VerifyPassword reports whether password matches encoded, and whether the
// stored profile differs from want (so the caller can transparently re-hash on
// the next successful login). A mismatch returns (false, false, nil).
func VerifyPassword(password, encoded string, want Argon2Params) (ok, needsRehash bool, err error) {
	p, salt, hash, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	computed := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(hash)))
	if subtle.ConstantTimeCompare(computed, hash) != 1 {
		return false, false, nil
	}
	needsRehash = p.Memory != want.Memory ||
		p.Time != want.Time ||
		p.Threads != want.Threads ||
		p.SaltLen != want.SaltLen ||
		uint32(len(hash)) != want.KeyLen
	return true, needsRehash, nil
}

// validParams bounds the cost parameters before they reach Argon2, which panics
// on time<1 or threads<1 and would exhaust memory on huge values. The ceilings
// keep a single hash from OOM-ing a small container.
func validParams(p Argon2Params) bool {
	const (
		maxMemoryKiB = 256 * 1024 // 256 MiB
		maxTime      = 10
		maxThreads   = 16
		maxLen       = 64
	)
	switch {
	case p.Time < 1 || p.Time > maxTime:
		return false
	case p.Threads < 1 || p.Threads > maxThreads:
		return false
	case p.Memory < 8*uint32(p.Threads) || p.Memory > maxMemoryKiB:
		return false
	case p.SaltLen < 8 || p.SaltLen > maxLen:
		return false
	case p.KeyLen < 16 || p.KeyLen > maxLen:
		return false
	default:
		return true
	}
}

func encodeHash(p Argon2Params, salt, hash []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "" / argon2id / v=19 / m=..,t=..,p=.. / salt / hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrBadHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, ErrBadHash
	}

	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, ErrBadHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrBadHash
	}

	p.SaltLen = uint32(len(salt))
	p.KeyLen = uint32(len(hash))
	if !validParams(p) {
		return Argon2Params{}, nil, nil, ErrBadHash
	}
	return p, salt, hash, nil
}
