package connectapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

// loadRotationKeyring loads the master key seeded by activeSeed with retained "<version>:<base64>" previous keys.
func loadRotationKeyring(t *testing.T, activeSeed byte, previous string) *crypto.Keyring {
	t.Helper()
	ring, err := crypto.LoadVersionedKeyring(rotationKey(activeSeed), "", previous, "")
	if err != nil {
		t.Fatalf("LoadVersionedKeyring: %v", err)
	}
	return ring
}

// rotationKey returns a base64 master key filled with seed.
func rotationKey(seed byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

// callMeWithSessionCookies sends Me through a cookie-less client with explicit session and CSRF values.
func callMeWithSessionCookies(env *authTestEnv, session, csrfCookie, csrfHeader string) error {
	req := connect.NewRequest(&portcullisv1.MeRequest{})
	req.Header().Set("Cookie", "__Host-portcullis_session="+session+"; __Host-portcullis_csrf="+csrfCookie)
	req.Header().Set("X-CSRF-Token", csrfHeader)
	_, err := env.raw.Me(context.Background(), req)
	return err
}

// callLogoutWithSessionCookies sends Logout through a cookie-less client with explicit session and CSRF values.
func callLogoutWithSessionCookies(env *authTestEnv, session, csrf string) error {
	req := connect.NewRequest(&portcullisv1.LogoutRequest{})
	req.Header().Set("Cookie", "__Host-portcullis_session="+session+"; __Host-portcullis_csrf="+csrf)
	req.Header().Set("X-CSRF-Token", csrf)
	_, err := env.raw.Logout(context.Background(), req)
	return err
}

func TestCSRFTokensFollowMasterKeyRotation(t *testing.T) {
	const firstSeed, secondSeed, thirdSeed = 0x41, 0x42, 0x43
	original := newAuthTestEnv(t, authEnvOptions{keyring: loadRotationKeyring(t, firstSeed, "")})
	rotated := newAuthTestEnv(t, authEnvOptions{sharedPool: original.pool, keyring: loadRotationKeyring(t, secondSeed, "1:"+rotationKey(firstSeed))})
	twiceRotated := newAuthTestEnv(t, authEnvOptions{sharedPool: original.pool, keyring: loadRotationKeyring(t, thirdSeed, "1:"+rotationKey(firstSeed)+",2:"+rotationKey(secondSeed))})

	const email, password = "rotation-admin@example.com", "rotation-admin-password"
	firstCSRF := original.bootstrapAndLogin(t, email, password)
	firstSession := cookieFromJar(original.jar, original.serverURL, "__Host-portcullis_session")
	if !strings.HasPrefix(firstCSRF, "1.") {
		t.Fatalf("CSRF token %q does not carry key version 1", firstCSRF)
	}

	// Success: a token issued before rotation keeps authorizing after one and after two rotations while its key version is retained.
	if err := callMeWithSessionCookies(rotated, firstSession, firstCSRF, firstCSRF); err != nil {
		t.Fatalf("pre-rotation token after one rotation: %v", err)
	}
	if err := callMeWithSessionCookies(twiceRotated, firstSession, firstCSRF, firstCSRF); err != nil {
		t.Fatalf("pre-rotation token after two rotations: %v", err)
	}

	// Refusal: a well-formed token from before key versioning cannot name its key, so Me and Logout ask the session to sign in again instead of reporting forgery.
	_, firstMacAndNonce, _ := strings.Cut(firstCSRF, ".")
	if code := connect.CodeOf(callMeWithSessionCookies(rotated, firstSession, firstMacAndNonce, firstMacAndNonce)); code != connect.CodeUnauthenticated {
		t.Errorf("pre-versioning token on Me code = %v, want Unauthenticated", code)
	}
	if code := connect.CodeOf(callLogoutWithSessionCookies(rotated, firstSession, firstMacAndNonce)); code != connect.CodeUnauthenticated {
		t.Errorf("pre-versioning token on Logout code = %v, want Unauthenticated", code)
	}

	// Attack: relabelling the version-1 MAC as version 2, or splitting cookie and header, stays a CSRF denial.
	relabelled := "2." + firstMacAndNonce
	if code := connect.CodeOf(callMeWithSessionCookies(rotated, firstSession, relabelled, relabelled)); code != connect.CodePermissionDenied {
		t.Errorf("relabelled version token code = %v, want PermissionDenied", code)
	}
	if code := connect.CodeOf(callMeWithSessionCookies(rotated, firstSession, firstCSRF, relabelled)); code != connect.CodePermissionDenied {
		t.Errorf("cookie/header mismatch code = %v, want PermissionDenied", code)
	}

	// Success: Logout works across rotation and actually revokes the session.
	if err := callLogoutWithSessionCookies(rotated, firstSession, firstCSRF); err != nil {
		t.Fatalf("Logout with a pre-rotation token: %v", err)
	}
	if code := connect.CodeOf(callMeWithSessionCookies(rotated, firstSession, firstCSRF, firstCSRF)); code != connect.CodeUnauthenticated {
		t.Errorf("Me after cross-rotation Logout code = %v, want Unauthenticated", code)
	}

	// Success: a login after rotation issues an active-version token that a later rotation still accepts.
	if _, err := rotated.client.Login(context.Background(), connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login after rotation: %v", err)
	}
	secondCSRF := csrfFromJar(rotated.jar, rotated.serverURL)
	secondSession := cookieFromJar(rotated.jar, rotated.serverURL, "__Host-portcullis_session")
	if !strings.HasPrefix(secondCSRF, "2.") {
		t.Fatalf("post-rotation CSRF token %q does not carry key version 2", secondCSRF)
	}
	if err := callMeWithSessionCookies(twiceRotated, secondSession, secondCSRF, secondCSRF); err != nil {
		t.Errorf("version-2 token after the next rotation: %v", err)
	}

	// Refusal: an instance rolled back to a keyring without version 2 asks the session to sign in again, for Me and Logout alike.
	if code := connect.CodeOf(callMeWithSessionCookies(original, secondSession, secondCSRF, secondCSRF)); code != connect.CodeUnauthenticated {
		t.Errorf("unloaded key version on Me code = %v, want Unauthenticated", code)
	}
	if code := connect.CodeOf(callLogoutWithSessionCookies(original, secondSession, secondCSRF)); code != connect.CodeUnauthenticated {
		t.Errorf("unloaded key version on Logout code = %v, want Unauthenticated", code)
	}
}
