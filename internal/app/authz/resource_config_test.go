package authz_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
)

func TestResourceAuthorizationConfigBounds(t *testing.T) {
	for _, settings := range []authz.ResourceConfig{{MaxBindings: 1, Timeout: time.Nanosecond}, authz.DefaultResourceConfig(), {MaxBindings: authz.MaxResourceBindings, Timeout: 5 * time.Second}} {
		if err := settings.Validate(); err != nil {
			t.Fatalf("finite limits: %+v %v", settings, err)
		}
	}
	for _, settings := range []authz.ResourceConfig{{MaxBindings: 0, Timeout: time.Second}, {MaxBindings: -1, Timeout: time.Second}, {MaxBindings: authz.MaxResourceBindings + 1, Timeout: time.Second}, {MaxBindings: 1, Timeout: 0}, {MaxBindings: 1, Timeout: -time.Second}, {MaxBindings: 1, Timeout: 5*time.Second + time.Nanosecond}} {
		if err := settings.Validate(); err == nil {
			t.Fatalf("unsafe limits: %+v", settings)
		}
	}
	t.Setenv("PORTCULLIS_RESOURCE_AUTHORIZATION_MAX_BINDINGS", "17")
	t.Setenv("PORTCULLIS_RESOURCE_AUTHORIZATION_TIMEOUT", "750ms")
	settings, err := authz.LoadResourceConfig()
	if err != nil || settings.MaxBindings != 17 || settings.Timeout != 750*time.Millisecond {
		t.Fatalf("environment limits: %+v %v", settings, err)
	}
	t.Setenv("PORTCULLIS_RESOURCE_AUTHORIZATION_MAX_BINDINGS", "0")
	if _, err := authz.LoadResourceConfig(); err == nil {
		t.Fatal("unsafe environment accepted")
	}
	t.Setenv("PORTCULLIS_RESOURCE_AUTHORIZATION_MAX_BINDINGS", "invalid")
	if _, err := authz.LoadResourceConfig(); err == nil {
		t.Fatal("malformed environment accepted")
	}
}
