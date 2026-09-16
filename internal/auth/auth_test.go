package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skifity/internal/crypto"
	"skifity/internal/store"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("the hash contains the password")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %q", hash)
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Fatalf("VerifyPassword with the right password: %v", err)
	}
	if err := VerifyPassword("correct horse battery stapler", hash); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("VerifyPassword with a wrong password gave %v, want ErrPasswordMismatch", err)
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Fatal("hashing the same password twice produced the same hash, so it is not salted")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=19456,t=2,p=4$c2FsdA$aGFzaA",  // wrong variant
		"$argon2id$v=16$m=19456,t=2,p=4$c2FsdA$aGFzaA", // wrong version
		"$argon2id$v=19$nonsense$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=4$!!!$aGFzaA",
	} {
		err := VerifyPassword("whatever", bad)
		if err == nil {
			t.Fatalf("VerifyPassword accepted the malformed hash %q", bad)
		}
		if errors.Is(err, ErrPasswordMismatch) {
			t.Fatalf("a malformed hash %q was reported as a wrong password, which hides a corrupt database", bad)
		}
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := HashPassword("something long enough")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if NeedsRehash(current) {
		t.Fatal("a freshly created hash was marked as needing a rehash")
	}
	if !NeedsRehash("$argon2id$v=19$m=4096,t=1,p=1$c2FsdA$aGFzaA") {
		t.Fatal("a hash with weaker parameters was not marked for upgrade")
	}
	if !NeedsRehash("not a hash at all") {
		t.Fatal("an unreadable hash should be replaced")
	}
}

func TestPasswordPolicy(t *testing.T) {
	p := DefaultPasswordPolicy()
	for _, bad := range []string{"", "short", "password1234", "PASSWORD123", "aaaaaaaaaaaaaaaa"} {
		if err := p.Check(bad); err == nil {
			t.Fatalf("policy accepted %q", bad)
		}
	}
	for _, good := range []string{"a reasonable passphrase", "Tr0ub4dor&3xyzzy!", "十分に長いパスワードです"} {
		if err := p.Check(good); err != nil {
			t.Fatalf("policy rejected %q: %v", good, err)
		}
	}
	// A very long password would make every login expensive.
	if err := p.Check(strings.Repeat("a", 2000)); err == nil {
		t.Fatal("policy accepted a 2000-character password")
	}
}

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	// RFC 6238 appendix B uses the ASCII secret "12345678901234567890".
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, tc := range cases {
		got, err := TOTPCode(secret, time.Unix(tc.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode: %v", err)
		}
		if got != tc.want {
			t.Fatalf("code at %d is %s, want %s (RFC 6238 test vector)", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyTOTPAcceptsClockDrift(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Now()

	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	if err := VerifyTOTP(secret, code, now); err != nil {
		t.Fatalf("the current code was rejected: %v", err)
	}
	// A phone 30 seconds fast or slow must still work.
	for _, drift := range []time.Duration{-30 * time.Second, 30 * time.Second} {
		drifted, err := TOTPCode(secret, now.Add(drift))
		if err != nil {
			t.Fatalf("TOTPCode: %v", err)
		}
		if err := VerifyTOTP(secret, drifted, now); err != nil {
			t.Fatalf("a code %s out of date was rejected: %v", drift, err)
		}
	}
	// Two minutes out is a replay, not drift.
	stale, _ := TOTPCode(secret, now.Add(-2*time.Minute))
	if err := VerifyTOTP(secret, stale, now); !errors.Is(err, ErrInvalidTOTP) {
		t.Fatalf("a two-minute-old code was accepted: %v", err)
	}
}

func TestVerifyTOTPRejectsBadInput(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()
	for _, bad := range []string{"", "123", "abcdef", "1234567"} {
		if err := VerifyTOTP(secret, bad, now); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatalf("VerifyTOTP(%q) gave %v, want ErrInvalidTOTP", bad, err)
		}
	}
	// A code typed with the space authenticator apps display must work.
	code, _ := TOTPCode(secret, now)
	spaced := code[:3] + " " + code[3:]
	if err := VerifyTOTP(secret, spaced, now); err != nil {
		t.Fatalf("a code typed with a space was rejected: %v", err)
	}
}

func TestTOTPURIIsScannable(t *testing.T) {
	uri := TOTPURI("Skifity", "owner@example.test", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{"otpauth://totp/", "secret=JBSWY3DPEHPK3PXP", "issuer=Skifity", "digits=6", "period=30", "algorithm=SHA1"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("otpauth URI is missing %q: %s", want, uri)
		}
	}
}

func TestGenerateRecoveryCodesAreUnique(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("duplicate recovery code %q", c)
		}
		seen[c] = true
		if len(c) != 14 || c[4] != '-' || c[9] != '-' {
			t.Fatalf("recovery code %q is not in the XXXX-XXXX-XXXX shape", c)
		}
		// These are hashed with a fast hash on purpose, so their length is the
		// only thing protecting them in a stolen database. Twelve base32
		// characters is sixty bits.
		if len(NormaliseRecoveryCode(c)) < 12 {
			t.Fatalf("recovery code %q carries too little entropy", c)
		}
	}
}

func TestHashTokenIsStableAndOneWay(t *testing.T) {
	a := HashToken("tok_abc123")
	if a != HashToken("tok_abc123") {
		t.Fatal("HashToken is not deterministic, so no session would ever be found")
	}
	if a == HashToken("tok_abc124") {
		t.Fatal("two different tokens hashed to the same value")
	}
	if strings.Contains(a, "abc123") {
		t.Fatal("the hash contains the token")
	}
}

// Sign-in rate limiting is a safe default the product promises, and an
// untested limit is a limit that quietly stops working.
func TestSignInIsRateLimited(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer db.Close()

	keyring, err := crypto.InitKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatalf("create a keyring: %v", err)
	}
	service := NewService(db, keyring, time.Hour, false)

	const email = "owner@example.test"
	const password = "a reasonable passphrase"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash the password: %v", err)
	}
	user := store.User{Email: email, Name: "Owner", PasswordHash: hash, IsAdmin: true}
	if err := db.CreateUser(ctx, &user); err != nil {
		t.Fatalf("create the user: %v", err)
	}

	limit := DefaultLockout().MaxPerAccount

	// Wrong password, up to the limit: each is refused as a bad credential and
	// never leaks whether the account exists.
	for attempt := 0; attempt < limit; attempt++ {
		_, err := service.Login(ctx, email, "not the password", "", "198.51.100.10", "test")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d gave %v, want ErrInvalidCredentials", attempt+1, err)
		}
	}

	// One more, and the account is locked rather than checked.
	if _, err := service.Login(ctx, email, "not the password", "", "198.51.100.10", "test"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("after %d failures the answer was %v, want ErrLockedOut", limit, err)
	}

	// The *right* password must be refused too. A limit that lets the correct
	// password through is not a limit: an attacker who guesses it on the next
	// attempt walks in.
	if _, err := service.Login(ctx, email, password, "", "198.51.100.10", "test"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("a locked-out account accepted the right password: %v", err)
	}

	// Another address must not be punished for this one's failures, or one
	// attacker could lock everybody out of a shared account on purpose.
	second := store.User{Email: "other@example.test", Name: "Other", PasswordHash: hash}
	if err := db.CreateUser(ctx, &second); err != nil {
		t.Fatalf("create the second user: %v", err)
	}
	if _, err := service.Login(ctx, second.Email, password, "", "203.0.113.20", "test"); err != nil {
		t.Fatalf("an unrelated account from another address was refused: %v", err)
	}
}

// An unknown account and a wrong password must be indistinguishable, or the
// sign-in form becomes a way to enumerate who has an account here.
func TestUnknownAccountLooksLikeAWrongPassword(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer db.Close()

	keyring, err := crypto.InitKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatalf("create a keyring: %v", err)
	}
	service := NewService(db, keyring, time.Hour, false)

	hash, err := HashPassword("a reasonable passphrase")
	if err != nil {
		t.Fatalf("hash the password: %v", err)
	}
	user := store.User{Email: "owner@example.test", Name: "Owner", PasswordHash: hash}
	if err := db.CreateUser(ctx, &user); err != nil {
		t.Fatalf("create the user: %v", err)
	}

	_, wrongPassword := service.Login(ctx, user.Email, "wrong", "", "198.51.100.30", "test")
	_, noSuchAccount := service.Login(ctx, "nobody@example.test", "wrong", "", "198.51.100.31", "test")

	if !errors.Is(wrongPassword, ErrInvalidCredentials) || !errors.Is(noSuchAccount, ErrInvalidCredentials) {
		t.Fatalf("expected both to be ErrInvalidCredentials, got %v and %v", wrongPassword, noSuchAccount)
	}
	if wrongPassword.Error() != noSuchAccount.Error() {
		t.Errorf("the two failures read differently: %q and %q", wrongPassword, noSuchAccount)
	}
}
