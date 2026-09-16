package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"skifity/internal/crypto"
	"skifity/internal/store"
)

// SessionCookieName is the cookie the panel uses for browser sessions.
const SessionCookieName = "skifity_session"

// CSRFCookieName holds the double-submit CSRF token.
const CSRFCookieName = "skifity_csrf"

// CSRFHeaderName is where the frontend echoes the CSRF token back.
const CSRFHeaderName = "X-Skifity-CSRF"

// TokenPrefix marks an API token so it can be spotted in a log or a leak scan.
const TokenPrefix = "skf_"

// Lockout describes the sign-in rate limit.
//
// Two separate limits: one per account so one person cannot be locked out by an
// attacker hammering a different account, and one per address so an attacker
// cannot spray many accounts from one machine.
type Lockout struct {
	Window          time.Duration
	MaxPerAccount   int
	MaxPerIPAddress int
}

// DefaultLockout is what the panel enforces.
func DefaultLockout() Lockout {
	return Lockout{Window: 15 * time.Minute, MaxPerAccount: 5, MaxPerIPAddress: 20}
}

// ErrLockedOut is returned when the rate limit has been hit.
var ErrLockedOut = errors.New("too many failed sign-in attempts")

// ErrInvalidCredentials is the single error returned for any authentication
// failure, so an attacker cannot tell an unknown account from a wrong password.
var ErrInvalidCredentials = errors.New("email or password is not correct")

// ErrTOTPRequired is returned when a password was right but a second factor is
// needed. It is safe to distinguish: the password was already correct.
var ErrTOTPRequired = errors.New("a two-factor code is required")

// Service performs authentication against the store.
type Service struct {
	db         *store.DB
	keyring    *crypto.Keyring
	lockout    Lockout
	sessionTTL time.Duration
	// secureCookies is false only in development over plain http.
	secureCookies bool
}

// NewService builds an auth service.
func NewService(db *store.DB, keyring *crypto.Keyring, sessionTTL time.Duration, secureCookies bool) *Service {
	return &Service{
		db:            db,
		keyring:       keyring,
		lockout:       DefaultLockout(),
		sessionTTL:    sessionTTL,
		secureCookies: secureCookies,
	}
}

// LoginResult is what a successful sign-in produces.
type LoginResult struct {
	User      store.User
	Token     string
	CSRFToken string
	ExpiresAt time.Time
	// UsedRecoveryCode is true when the second factor was a recovery code
	// rather than the authenticator, so the panel can say one is gone.
	UsedRecoveryCode  bool
	RecoveryCodesLeft int
}

// Login verifies credentials and issues a session.
//
// totpCode may be empty on the first call; when the account has two-factor
// enabled, ErrTOTPRequired comes back and the caller asks for a code.
func (s *Service) Login(ctx context.Context, email, password, totpCode, ip, userAgent string) (LoginResult, error) {
	if err := s.checkLockout(ctx, email, ip); err != nil {
		return LoginResult{}, err
	}

	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Hash anyway so an unknown account takes as long as a known one.
			// Without this, response timing tells an attacker which emails exist.
			_, _ = HashPassword(password)
			s.recordFailure(ctx, email, ip)
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("look up user: %w", err)
	}

	if err := VerifyPassword(password, user.PasswordHash); err != nil {
		s.recordFailure(ctx, email, ip)
		if errors.Is(err, ErrPasswordMismatch) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if user.Disabled {
		s.recordFailure(ctx, email, ip)
		return LoginResult{}, ErrInvalidCredentials
	}

	usedRecoveryCode := false
	if user.TOTPEnabled {
		if totpCode == "" {
			return LoginResult{}, ErrTOTPRequired
		}
		secret, err := s.keyring.Open(user.TOTPSecretEnc, totpContext(user.ID))
		if err != nil {
			return LoginResult{}, fmt.Errorf("read two-factor secret: %w", err)
		}
		if err := VerifyTOTP(string(secret), totpCode, time.Now()); err != nil {
			// A phone is lost often enough that the codes written down when
			// two-factor was turned on have to actually work. They are tried
			// second, so a real code is never spent by a mistyped one.
			used, recoveryErr := s.db.UseRecoveryCode(ctx, user.ID, HashRecoveryCode(totpCode))
			if recoveryErr != nil {
				return LoginResult{}, fmt.Errorf("check recovery codes: %w", recoveryErr)
			}
			if !used {
				s.recordFailure(ctx, email, ip)
				return LoginResult{}, err
			}
			usedRecoveryCode = true
		}
	}

	// Upgrade a hash made with older parameters now that we have the password.
	if NeedsRehash(user.PasswordHash) {
		if rehashed, err := HashPassword(password); err == nil {
			user.PasswordHash = rehashed
			_ = s.db.UpdateUser(ctx, &user)
		}
	}

	result, err := s.issueSession(ctx, user, ip, userAgent)
	if err != nil {
		return LoginResult{}, err
	}

	_ = s.db.RecordLoginAttempt(ctx, email, ip, true)
	_ = s.db.ClearLoginAttempts(ctx, email)
	_ = s.db.TouchUserLogin(ctx, user.ID)
	if usedRecoveryCode {
		result.UsedRecoveryCode = true
		result.RecoveryCodesLeft, _ = s.db.CountRecoveryCodes(ctx, user.ID)
	}
	return result, nil
}

// IssueSession creates a session without checking a password. Used right after
// first-run setup, where the setup token has already proved authority.
func (s *Service) IssueSession(ctx context.Context, user store.User, ip, userAgent string) (LoginResult, error) {
	return s.issueSession(ctx, user, ip, userAgent)
}

func (s *Service) issueSession(ctx context.Context, user store.User, ip, userAgent string) (LoginResult, error) {
	token, err := crypto.RandomToken(32)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := crypto.RandomToken(32)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate csrf token: %w", err)
	}
	expires := time.Now().Add(s.sessionTTL)
	session := store.Session{
		UserID:    user.ID,
		TokenHash: HashToken(token),
		IP:        ip,
		UserAgent: truncate(userAgent, 255),
		ExpiresAt: expires,
	}
	if err := s.db.CreateSession(ctx, &session); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	return LoginResult{User: user, Token: token, CSRFToken: csrfToken, ExpiresAt: expires}, nil
}

// Authenticate resolves a session token to its user and extends the session.
func (s *Service) Authenticate(ctx context.Context, token string) (store.User, store.Session, error) {
	session, err := s.db.GetSessionByHash(ctx, HashToken(token))
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	user, err := s.db.GetUser(ctx, session.UserID)
	if err != nil {
		return store.User{}, store.Session{}, err
	}
	if user.Disabled {
		// Disabling an account must take effect immediately, not at expiry.
		_ = s.db.DeleteSession(ctx, session.ID)
		return store.User{}, store.Session{}, store.ErrNotFound
	}
	// Sliding expiry, refreshed at most once a minute to avoid a write per request.
	if time.Since(session.LastSeenAt) > time.Minute {
		_ = s.db.TouchSession(ctx, session.ID, time.Now().Add(s.sessionTTL))
	}
	return user, session, nil
}

// AuthenticateToken resolves an API token to its user.
func (s *Service) AuthenticateToken(ctx context.Context, token string) (store.User, store.APIToken, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return store.User{}, store.APIToken{}, store.ErrNotFound
	}
	apiToken, err := s.db.GetAPITokenByHash(ctx, HashToken(token))
	if err != nil {
		return store.User{}, store.APIToken{}, err
	}
	user, err := s.db.GetUser(ctx, apiToken.UserID)
	if err != nil {
		return store.User{}, store.APIToken{}, err
	}
	if user.Disabled {
		return store.User{}, store.APIToken{}, store.ErrNotFound
	}
	// Recorded once a minute at most, for the same reason as sessions.
	if time.Since(apiToken.LastUsedAt) > time.Minute {
		_ = s.db.TouchAPIToken(ctx, apiToken.ID)
	}
	return user, apiToken, nil
}

// CreateAPIToken issues a token and returns the only copy of its plaintext.
func (s *Service) CreateAPIToken(ctx context.Context, userID, teamID, name, scopes string, ttl time.Duration) (store.APIToken, string, error) {
	raw, err := crypto.RandomToken(32)
	if err != nil {
		return store.APIToken{}, "", fmt.Errorf("generate api token: %w", err)
	}
	token := TokenPrefix + raw
	record := store.APIToken{
		UserID:    userID,
		TeamID:    teamID,
		Name:      name,
		TokenHash: HashToken(token),
		// The prefix is shown in the UI so a token can be identified without
		// storing anything that would let it be used.
		Prefix: token[:min(len(token), 12)],
		Scopes: scopes,
	}
	if ttl > 0 {
		record.ExpiresAt = time.Now().Add(ttl)
	}
	if err := s.db.CreateAPIToken(ctx, &record); err != nil {
		return store.APIToken{}, "", err
	}
	return record, token, nil
}

// Logout ends one session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.db.DeleteSession(ctx, sessionID)
}

// SetupTOTP generates a secret for a user and returns it with its otpauth URI.
// The secret is not enabled until a code from it is confirmed.
func (s *Service) SetupTOTP(ctx context.Context, user *store.User, issuer string) (secret, uri string, err error) {
	secret, err = GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.keyring.Seal([]byte(secret), totpContext(user.ID))
	if err != nil {
		return "", "", fmt.Errorf("encrypt two-factor secret: %w", err)
	}
	user.TOTPSecretEnc = sealed
	user.TOTPEnabled = false
	if err := s.db.UpdateUser(ctx, user); err != nil {
		return "", "", err
	}
	return secret, TOTPURI(issuer, user.Email, secret), nil
}

// StoreRecoveryCodes keeps the hashes of the codes shown to a user.
//
// They are stored when the setup screen shows them, not when two-factor is
// confirmed: the screen is the only place they exist in readable form, and a
// user who writes them down and then closes the tab has to be able to use
// them. They do nothing until two-factor is on.
func (s *Service) StoreRecoveryCodes(ctx context.Context, userID string, codes []string) error {
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hashes = append(hashes, HashRecoveryCode(code))
	}
	return s.db.ReplaceRecoveryCodes(ctx, userID, hashes)
}

// RecoveryCodesLeft reports how many unused codes a user has.
func (s *Service) RecoveryCodesLeft(ctx context.Context, userID string) (int, error) {
	return s.db.CountRecoveryCodes(ctx, userID)
}

// ConfirmTOTP enables two-factor once the user proves they can generate a code.
func (s *Service) ConfirmTOTP(ctx context.Context, user *store.User, code string) error {
	if user.TOTPSecretEnc == "" {
		return errors.New("two-factor setup has not been started")
	}
	secret, err := s.keyring.Open(user.TOTPSecretEnc, totpContext(user.ID))
	if err != nil {
		return fmt.Errorf("read two-factor secret: %w", err)
	}
	if err := VerifyTOTP(string(secret), code, time.Now()); err != nil {
		return err
	}
	user.TOTPEnabled = true
	return s.db.UpdateUser(ctx, user)
}

// DisableTOTP turns two-factor off and forgets the secret.
func (s *Service) DisableTOTP(ctx context.Context, user *store.User) error {
	user.TOTPEnabled = false
	user.TOTPSecretEnc = ""
	if err := s.db.UpdateUser(ctx, user); err != nil {
		return err
	}
	// The codes only ever existed to get past this secret. Keeping them would
	// leave a second way in that the user believes they have turned off.
	return s.db.DeleteRecoveryCodes(ctx, user.ID)
}

// ChangePassword sets a new password and signs every other session out.
func (s *Service) ChangePassword(ctx context.Context, user *store.User, current, next, keepSessionID string) error {
	if err := VerifyPassword(current, user.PasswordHash); err != nil {
		return ErrInvalidCredentials
	}
	if err := DefaultPasswordPolicy().Check(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	if err := s.db.UpdateUser(ctx, user); err != nil {
		return err
	}
	// A password change must invalidate anything an attacker already holds.
	sessions, err := s.db.ListSessions(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.ID == keepSessionID {
			continue
		}
		_ = s.db.DeleteSession(ctx, sess.ID)
	}
	return nil
}

func (s *Service) checkLockout(ctx context.Context, email, ip string) error {
	since := time.Now().Add(-s.lockout.Window)
	byAccount, byIP, err := s.db.CountFailedLogins(ctx, email, ip, since)
	if err != nil {
		return fmt.Errorf("check sign-in rate limit: %w", err)
	}
	if byAccount >= s.lockout.MaxPerAccount || byIP >= s.lockout.MaxPerIPAddress {
		return ErrLockedOut
	}
	return nil
}

func (s *Service) recordFailure(ctx context.Context, email, ip string) {
	_ = s.db.RecordLoginAttempt(ctx, email, ip, false)
}

// LockoutWindow is how long a locked-out caller must wait.
func (s *Service) LockoutWindow() time.Duration { return s.lockout.Window }

// SessionCookie builds the cookie for a session token.
func (s *Service) SessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:  SessionCookieName,
		Value: token,
		Path:  "/",
		// HttpOnly stops any script from reading the session, which is the
		// difference between an XSS bug and an account takeover.
		HttpOnly: true,
		Secure:   s.secureCookies,
		// Lax rather than Strict so that following a link from an email into
		// the panel does not look signed out.
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	}
}

// CSRFCookie builds the double-submit cookie. It is deliberately readable by
// the frontend so it can echo the value in a header.
func (s *Service) CSRFCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	}
}

// ClearCookie expires a cookie by name.
func (s *Service) ClearCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: name == SessionCookieName,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// totpContext binds a two-factor secret to the user it belongs to, so a stolen
// ciphertext cannot be moved onto another account.
func totpContext(userID string) string { return "totp:" + userID }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// API token scopes.
//
// A token with no scopes can do everything its owner can, which is what every
// token issued before this existed does. The named scopes narrow it.
const (
	// ScopeRead allows requests that only read.
	ScopeRead = "read"
	// ScopeWrite allows everything else.
	ScopeWrite = "write"
)

// TokenAllows reports whether a token's scopes permit a request method.
//
// The scopes were stored from the day tokens were added and never once
// consulted, so a token marked read-only could do anything its owner could.
func TokenAllows(scopes, method string) bool {
	scopes = strings.TrimSpace(scopes)
	if scopes == "" {
		return true
	}
	readOnly := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	for _, scope := range strings.Split(scopes, ",") {
		switch strings.TrimSpace(scope) {
		case ScopeWrite:
			return true
		case ScopeRead:
			if readOnly {
				return true
			}
		}
	}
	return false
}

// ValidateScopes rejects a scope the panel would not enforce, so a token is
// never narrower than its owner believes.
func ValidateScopes(scopes string) error {
	if strings.TrimSpace(scopes) == "" {
		return nil
	}
	for _, scope := range strings.Split(scopes, ",") {
		switch strings.TrimSpace(scope) {
		case ScopeRead, ScopeWrite:
		default:
			return fmt.Errorf("%q is not a scope; use read, write, or leave it empty for full access", strings.TrimSpace(scope))
		}
	}
	return nil
}
