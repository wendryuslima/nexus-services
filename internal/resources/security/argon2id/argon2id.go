package argon2id

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"golang.org/x/crypto/argon2"
)

const (
	minMemory      uint32 = 19 * 1024
	maxMemory      uint32 = 64 * 1024
	minIterations  uint32 = 2
	maxIterations  uint32 = 5
	minParallelism uint8  = 1
	maxParallelism uint8  = 4

	minSaltLength uint32 = 16
	maxSaltLength uint32 = 64
	minKeyLength  uint32 = 32
	maxKeyLength  uint32 = 64

	maxEncodedHashLength = 512
)

var _ ports.PasswordHasher = (*Hasher)(nil)

type Parameters struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLenght  uint32
	KeyLenght   uint32
}

type Hasher struct {
	parameters Parameters
}

func DefaultParameters() Parameters {
	return Parameters{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLenght:  16,
		KeyLenght:   32,
	}
}

func NewHasher(parameters Parameters) (*Hasher, error) {
	if err := validateParameters(parameters); err != nil {
		return nil, err
	}

	return &Hasher{
		parameters: parameters,
	}, nil
}

func (hasher *Hasher) Hash(ctx context.Context, plainPassword string) (user.PasswordHash, error) {
	if err := ctx.Err(); err != nil {
		return user.PasswordHash{}, err
	}

	salt := make([]byte, hasher.parameters.SaltLenght)
	if _, err := rand.Read(salt); err != nil {
		return user.PasswordHash{}, fmt.Errorf("generate Argon2id salt: %w", err)
	}
	passwordBytes := []byte(plainPassword)

	defer eraseBytes(passwordBytes)
	derivedKey := argon2.IDKey(passwordBytes, salt, hasher.parameters.Iterations, hasher.parameters.Memory, hasher.parameters.Parallelism, hasher.parameters.KeyLenght)
	defer eraseBytes(derivedKey)
	if err := ctx.Err(); err != nil {
		return user.PasswordHash{}, err
	}

	encodeHash := encodeHash(hasher.parameters, salt, derivedKey)
	passwordHash, err := user.NewPasswordHash(encodeHash)
	if err != nil {
		return user.PasswordHash{}, fmt.Errorf("create password hash value object: %w", err)

	}

	return passwordHash, nil
}

func (hasher *Hasher) Matches(ctx context.Context, plainPassword string, passwordHash user.PasswordHash) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if plainPassword == "" {
		return false, ErrEmptyPassword
	}

	parameters, salt, expectedKey, err := parseHash(passwordHash.String())
	if err != nil {
		return false, err
	}

	passwordBytes := []byte(plainPassword)
	defer eraseBytes(passwordBytes)

	actualKey := argon2.IDKey(passwordBytes, salt, parameters.Iterations, parameters.Memory, parameters.Parallelism, parameters.KeyLenght)
	defer eraseBytes(actualKey)
	if err := ctx.Err(); err != nil {
		return false, err
	}

	matches := subtle.ConstantTimeCompare(actualKey, expectedKey)
	return matches == 1, nil
}

func encodeHash(parameters Parameters, salt []byte, derivedKey []byte) string {
	encodeSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodeKey := base64.RawStdEncoding.EncodeToString(derivedKey)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		parameters.Memory,
		parameters.Iterations,
		parameters.Parallelism,
		encodeSalt,
		encodeKey,
	)
}

func parseHash(encodeHash string) (Parameters, []byte, []byte, error) {
	if encodeHash == "" || len(encodeHash) > maxEncodedHashLength {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	parts := strings.Split(encodeHash, "$")

	if len(parts) != 6 ||
		parts[0] != "" ||
		parts[1] != "argon2id" {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	version, err := parseNamedUint(parts[2], "v=", 32)
	if err != nil || uint32(version) != argon2.Version {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	costParts := strings.Split(parts[3], ",")
	if len(costParts) != 3 {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	memory, err := parseNamedUint(costParts[0], "m=", 32)
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	iterations, err := parseNamedUint(costParts[1], "t=", 32)
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	parallelism, err := parseNamedUint(costParts[2], "p=", 8)
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	derivedKey, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodeHash
	}

	parameters := Parameters{
		Memory:      uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
		SaltLenght:  uint32(len(salt)),
		KeyLenght:   uint32(len(derivedKey)),
	}

	if err := validateParameters(parameters); err != nil {
		return Parameters{}, nil, nil, fmt.Errorf("%w: %v", ErrInvalidEncodeHash, err)
	}
	return parameters, salt, derivedKey, nil
}

func parseNamedUint(value string, prefix string, bitSize int) (uint64, error) {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, ErrInvalidEncodeHash
	}

	parsedValue, err := strconv.ParseUint(value[len(prefix):], 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: parse %s", ErrInvalidEncodeHash, prefix)
	}
	return parsedValue, nil
}

func validateParameters(parameters Parameters) error {
	if parameters.Memory < minMemory || parameters.Memory > maxMemory {
		return fmt.Errorf("%w: memory must be between %d and %d KiB", ErrInvalidParameters, minMemory, maxMemory)

	}

	if parameters.Iterations < minIterations || parameters.Iterations > maxIterations {
		return fmt.Errorf("%w: iterations must be between %d and %d", ErrInvalidParameters, minIterations, maxIterations)
	}

	if parameters.Parallelism < minParallelism ||
		parameters.Parallelism > maxParallelism {
		return fmt.Errorf(
			"%w: parallelism must be between %d and %d",
			ErrInvalidParameters,
			minParallelism,
			maxParallelism,
		)
	}

	if parameters.SaltLenght < minSaltLength ||
		parameters.SaltLenght > maxSaltLength {
		return fmt.Errorf(
			"%w: salt length must be between %d and %d",
			ErrInvalidParameters,
			minSaltLength,
			maxSaltLength,
		)
	}

	if parameters.KeyLenght < minKeyLength ||
		parameters.KeyLenght > maxKeyLength {
		return fmt.Errorf(
			"%w: key length must be between %d and %d",
			ErrInvalidParameters,
			minKeyLength,
			maxKeyLength,
		)
	}

	return nil
}

func eraseBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
