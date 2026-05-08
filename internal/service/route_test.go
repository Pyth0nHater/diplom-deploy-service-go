package service

import "testing"

func TestResolveRouteDefaultsToLocalRepositoryPath(t *testing.T) {
	params := DeployParams{
		RepoURL:   "https://github.com/acme/mai-wallet.git",
		ImageName: "Mai-wallet",
	}

	route := resolveRoute(params, "mai-wallet")

	if route.AccessTarget != "localhost/acme/mai-wallet" {
		t.Fatalf("unexpected access target: %s", route.AccessTarget)
	}
	if route.Rule != "Host(`localhost`) && (Path(`/acme/mai-wallet`) || PathPrefix(`/acme/mai-wallet/`))" {
		t.Fatalf("unexpected rule: %s", route.Rule)
	}
	if !route.UseStripPrefix || route.StripPrefix != "/acme/mai-wallet" {
		t.Fatalf("unexpected strip prefix route: %#v", route)
	}
}

func TestResolveRouteUsesConfiguredBaseHost(t *testing.T) {
	t.Setenv("DEPLOY_BASE_DOMAIN", "apps.local")

	params := DeployParams{
		RepoURL:   "https://github.com/acme/mai-wallet.git",
		ImageName: "Mai-wallet",
		Domain:    "ignored.example.com",
	}

	route := resolveRoute(params, "mai-wallet")

	if route.AccessTarget != "apps.local/acme/mai-wallet" {
		t.Fatalf("unexpected access target: %s", route.AccessTarget)
	}
}

func TestRouteForProfileDisablesStripPrefixForNextJS(t *testing.T) {
	route := routeConfig{
		AccessTarget:   "localhost/acme/mai-wallet",
		Rule:           "Host(`localhost`) && (Path(`/acme/mai-wallet`) || PathPrefix(`/acme/mai-wallet/`))",
		MiddlewareName: "mai-wallet-strip",
		StripPrefix:    "/acme/mai-wallet",
		UseStripPrefix: true,
	}

	nextRoute := routeForProfile(route, nextAppProfile())

	if nextRoute.UseStripPrefix {
		t.Fatalf("expected strip prefix to be disabled: %#v", nextRoute)
	}
	if nextRoute.StripPrefix != "" {
		t.Fatalf("expected empty strip prefix: %#v", nextRoute)
	}
}
