package connectapi_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

// loginFromClient sends one Login as the forwarded client IP through the trusted loopback proxy.
func loginFromClient(env *authTestEnv, clientIP, email, password string) error {
	req := connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})
	req.Header().Set("X-Forwarded-For", clientIP)
	_, err := env.raw.Login(context.Background(), req)
	return err
}

func TestAttackerCannotExhaustAnotherClientsLoginBucket(t *testing.T) {
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	env := newAuthTestEnv(t, authEnvOptions{trustedProxies: []*net.IPNet{loopback}, rateLimit: true})
	const victim, password = "sole-admin@example.com", "sole-admin-password-1"
	const attackerIP, victimIP, otherVictimIP = "203.0.113.7", "198.51.100.20", "198.51.100.21"
	bootstrap := connect.NewRequest(&portcullisv1.BootstrapRequest{Email: victim, Password: password, DisplayName: "Admin"})
	bootstrap.Header().Set("X-Forwarded-For", "192.0.2.1")
	if _, err := env.raw.Bootstrap(context.Background(), bootstrap); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Refusal: one client hammering one account is throttled once its account bucket is spent, before its IP bucket.
	throttledAt := 0
	for attempt := 1; attempt <= 8; attempt++ {
		if connect.CodeOf(loginFromClient(env, attackerIP, victim, "wrong-password-guess")) == connect.CodeResourceExhausted {
			throttledAt = attempt
			break
		}
	}
	if throttledAt == 0 || throttledAt > 6 {
		t.Fatalf("attacker throttled at attempt %d, want within the 5-token account burst", throttledAt)
	}
	// Refusal: a differently spelled email and a spoofed left forwarded entry still hit the attacker's spent bucket.
	if code := connect.CodeOf(loginFromClient(env, attackerIP, " SOLE-ADMIN@Example.com ", "wrong-password-guess")); code != connect.CodeResourceExhausted {
		t.Errorf("respelled email from the attacker code = %v, want ResourceExhausted", code)
	}
	if code := connect.CodeOf(loginFromClient(env, "10.9.9.9, "+attackerIP, victim, "wrong-password-guess")); code != connect.CodeResourceExhausted {
		t.Errorf("spoofed left forwarded entry code = %v, want ResourceExhausted", code)
	}

	// Success: the attacker's spent account bucket does not throttle the victim's own clients.
	// Refusal: the shared account backoff still counts the attacker's failures across IPs, so the correct password is refused uniformly while locked.
	if code := connect.CodeOf(loginFromClient(env, victimIP, victim, password)); code != connect.CodeUnauthenticated {
		t.Errorf("victim login during account backoff code = %v, want Unauthenticated (not ResourceExhausted)", code)
	}
	if _, err := env.pool.Exec(context.Background(), `update login_backoff set locked_until = now() - interval '1 second'
		where user_id = (select id from users where lower(email) = lower($1))`, victim); err != nil {
		t.Fatalf("expire lockout: %v", err)
	}
	if err := loginFromClient(env, victimIP, victim, password); err != nil {
		t.Errorf("victim login from its own IP after the backoff window: %v", err)
	}
	if err := loginFromClient(env, otherVictimIP, victim, password); err != nil {
		t.Errorf("victim login from a second IP: %v", err)
	}
	// Success: the attacker's IP is throttled for the targeted account only; its IP bucket still admits another account.
	if code := connect.CodeOf(loginFromClient(env, attackerIP, "someone-else@example.com", "wrong-password-guess")); code != connect.CodeUnauthenticated {
		t.Errorf("attacker IP on another account code = %v, want Unauthenticated", code)
	}

	// Refusal: spraying many accounts from one IP is still shed by the per-IP bucket.
	sprayThrottled := false
	for attempt := range 30 {
		if connect.CodeOf(loginFromClient(env, "203.0.113.99", fmt.Sprintf("spray-%d@example.com", attempt), "wrong-password-guess")) == connect.CodeResourceExhausted {
			sprayThrottled = true
			break
		}
	}
	if !sprayThrottled {
		t.Error("an email spray from one IP was never throttled")
	}
}
