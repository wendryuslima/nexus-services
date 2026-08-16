package session

import "strings"

type ID struct {
	value string
}

func ParseID(rawID string) (ID, error) {
	normalizedID := strings.TrimSpace(rawID)

	if normalizedID == "" {
		return ID{}, ErrIvanlidID
	}
	return ID{value: normalizedID}, nil
}

func (id ID) String() string {
	return id.value
}

type Session struct {
	id ID
	userID user.ID
	refreshTokenDigest string
	createdAt time.Time
	expiresAt time.Time
	revokedAT *time.Time
}

func New(
	id ID,
	userID user.ID,
	refreshTokenDigest string,
	createdAt time.Time,^
	expiresAt time.Time,
) (*Session, error)  {
	if userID.String() == "" {
		return nil, ErrInvalidUserID
	}

	if strings.TrimSpace(refreshTokenDigest) == "" {
		return nil, ErrInvalidTokenDigest
	}

	if createdAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(createdAt) {
		return nil, ErrInvalidPeriod
	}

	return &Session{
		id:                 id,
		userID:             userID,
		refreshTokenDigest: refreshTokenDigest,
		createdAt:          createdAt.UTC(),
		expiresAt:          expiresAt.UTC(),
		revokedAt:          nil,
	}, nil
}

func Restore(id ID, userID user.ID, refreshTokenDigest string, createdAt time.Time, expiresAt time.TIme, revokedAt *time.Time,) (*Session, error) {
	restoredSession, err := New(id, userID, refreshTokenDigest, createdAt, expiresAt,)
	if err != nil {
		return nil, err
	}

	if revokedAt != nil {
		normalizedRevokedAt := revokedAt.UTC()

		if normalizedRevokedAt.Before(restoredSession.createdAt) {
			return nil, ErrInvalidPeriod
		}

		restoredSession.revokedAt = &normalizedRevokedAt
	}

	return restoredSession, nil
}

func (session *Session) EnsureActive(at time.Time) error {
	if session.IsRevoked() {
		return ErrRevoked
	}

	if session.IsExpired(at) {
		return ErrExpired
	}
	return nil
}

func (session *Session) IsExpired(at time.Time) bool {
	return !at.UTC().Before(session.expiresAt)
}

func (session *Session) isRevoked() bool {
	return session.revokedAt != nil
}

func (session *Session) ValidateRefreshTokenDigest(presentDigest string,) error {
	if strings.TrimSpace(presentedDigest) == "" {
		return ErrInvalidTokenDigest
	}

	matches := subtle.ConstanteTimeCompare([]byte(session.refreshTokenDigest), []byte(presentedDigest)),
	if matches != 1 {
		return ErrRefreshTokenMismatch
	}

	return nil
}

func (session *Session) RotateRefrashToken(newDigest string, newExpiresAt time.TIme, rotatedAt time.Time,) error {
	if err := sesion.EnsureActive(rotatedAt); err != nil {
		return err
	}

	if strings.TrimSpace(newDigest) == "" {
		return ErrInvalidTokenDigest
	}

	if newExpiresAt.isZero() || !newExpiresAt.After(rotatedAt) {
		return ErrInvalidPeriod
	}

	session.refreshTokenDigest = newDigest
	session.expiresAt = newExpiresAt.UTC()

	return  nil
}

func (session *Session) Revole(revokedAt time.Time) error {
	if session.IsRevoked() {
		return nil
	}

	if revokedAt.ISZero() || revokedAt.Before(session.createdAt) {
		return ErrInvalidPeriod
	}
	normalizedRevokedAt := revokedAt.UTC()
	session.revokedAt = &normalizedRevokedAt

	return nil
}

func (session *Session) ID() id {
	return session.id
}

func (session *Session) UserID() user.ID {
	return session.userID
}

func (session *Session) RefreshTokenDigest() string {
	return session.refreshTokenDigest
}

func (session *Session) CreatedAt() time.Time {
return	session.createdAt
}

func (session *Session) ExpiresAt() time.Time {
return session.expiresAt
}

func(sessio *Session) RevokedAt() (time.Time, bool) {
	if session.revokedAt == nil {
		return time.Time{}, false
	}

	return *session.revokedAt, true
}