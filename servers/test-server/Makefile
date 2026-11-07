.PHONY: build check

# The image must build with the legacy (non-BuildKit) builder too: `COPY --chmod`
# is BuildKit-only, and local contributors without the buildx component fail
# with "the --chmod option requires BuildKit" (php-ext, caddy, frankenphp and
# traefik suites all build this image). `go build` already emits 0755, so COPY
# preserves an executable mode without the flag. Fail if it comes back.
check:
	@if grep -nE -- '--chmod([=]|[[:space:]]|$$)' Dockerfile | grep -vE '^[0-9]+:[[:space:]]*#'; then \
		echo "ERROR: Dockerfile uses BuildKit-only COPY/ADD --chmod; drop it." >&2; \
		exit 1; \
	fi

build: check
	docker build --tag=crazygoat/esi-test-server:latest .
