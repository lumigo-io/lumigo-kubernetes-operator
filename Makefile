# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# Image URLs to use all building/pushing image targets
IMG_VERSION ?= latest
CONTROLLER_IMG ?= host.docker.internal:5000/controller:$(IMG_VERSION)
PROXY_IMG ?= host.docker.internal:5000/telemetry-proxy:$(IMG_VERSION)
WATCHDOG_IMG ?= host.docker.internal:5000/watchdog:$(IMG_VERSION)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

GOCMD?= go

POST_BUILD_FLAG ?= --push

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell $(GOCMD) env GOBIN))
GOBIN=$(shell $(GOCMD)go env GOPATH)/bin
else
GOBIN=$(shell $(GOCMD) env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: install-tools build

TOOLS_MOD_DIR := ./tools
.PHONY: install-tools
install-tools:
	cd $(TOOLS_MOD_DIR) && $(GOCMD) install github.com/tcnksm/ghr

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	(cd ./controller/src && $(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="." output:crd:artifacts:config=config/crd/bases )

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	(cd ./controller/src && $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="." )

.PHONY: fmt
fmt: ## Run go fmt against code.
	(cd ./controller/src && $(GOCMD) fmt .)

.PHONY: vet
vet: ## Run go vet against code.
	(cd ./controller/src && $(GOCMD) vet .)

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	(cd ./controller/src && KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" $(GOCMD) test ./... -coverprofile cover.out )

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	$(GOCMD) build -o bin/manager ./controller/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	$(GOCMD) run ./controller/main.go

.PHONY: e2e-tests
e2e-tests:
	cd tests && $(GOCMD) test

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test docker-build-without-tests
	echo "Finished building docker image"

.PHONY: docker-build-controller
docker-build-controller:
	docker build -t ${CONTROLLER_IMG} -f controller/Dockerfile controller

.PHONY: docker-build-telemetry-proxy
docker-build-telemetry-proxy:
	docker build -t ${PROXY_IMG} -f telemetryproxy/Dockerfile telemetryproxy

docker-build-watchdog:
	docker build -t ${WATCHDOG_IMG} -f watchdog/Dockerfile watchdog

# Used by our IT because we don't want to run the tests there
.PHONY: docker-build-without-tests
docker-build-without-tests: docker-build-controller docker-build-telemetry-proxy docker-build-watchdog

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${CONTROLLER_IMG}
	docker push ${PROXY_IMG}
	docker push ${WATCHDOG_IMG}

# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx CONTROLLER_IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via CONTROLLER_IMG=<myregistry/image:<tag>> than the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORM_ARRAY := linux/arm64 linux/amd64 # linux/s390x linux/ppc64le
# Convert the array to a comma-separated string for docker buildx
PLATFORMS ?= $(shell echo $(PLATFORM_ARRAY) | sed 's/ /,/g')

.PHONY: docker-buildx
docker-buildx: test docker-buildx-manager docker-buildx-telemetry-proxy docker-buildx-watchdog  ## Build and push docker image for the manager for cross-platform support

.PHONY: docker-buildx-manager
docker-buildx-manager: ## Build and push docker image for the manager for cross-platform support; this target does NOT run unit tests, it is meant for CI/CD
	( cd controller && \
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross && \
	docker buildx create --name project-v3-builder && \
	docker buildx use project-v3-builder && \
	docker buildx build --push --provenance=false --platform=$(PLATFORMS) --tag ${CONTROLLER_IMG} -f Dockerfile.cross . && \
	docker buildx rm project-v3-builder && \
	rm Dockerfile.cross )

.PHONY: docker-buildx-telemetry-proxy
docker-buildx-telemetry-proxy: ## Build and push docker image for the manager for cross-platform support
	(cd telemetryproxy && \
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross && \
	docker buildx rm project-v3-builder >/dev/null 2>&1 || true && \
	docker buildx create --name project-v3-builder && \
	docker buildx use project-v3-builder && \
	docker buildx build $(POST_BUILD_FLAG) --provenance=false --platform=$(PLATFORMS) --tag ${PROXY_IMG} -f Dockerfile.cross --build-arg "version=$(VERSION)" . && \
	docker buildx rm project-v3-builder && \
	rm Dockerfile.cross)

.PHONY: docker-buildx-watchdog
docker-buildx-watchdog: ## Build and push docker image for the watchdog for cross-platform support
	( cd watchdog && \
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross && \
	docker buildx create --name project-v3-builder && \
	docker buildx use project-v3-builder && \
	docker buildx build --push --provenance=false --platform=$(PLATFORMS) --tag ${WATCHDOG_IMG} -f Dockerfile.cross --build-arg "version=$(VERSION)" . && \
	docker buildx rm project-v3-builder && \
	rm Dockerfile.cross )

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${CONTROLLER_IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.10.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) $(GOCMD) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

## Pin the version of setup-envtest until https://github.com/kubernetes-sigs/controller-runtime/issues/2720 is resolved.
.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) $(GOCMD) install sigs.k8s.io/controller-runtime/tools/setup-envtest@c7e1dc9b
