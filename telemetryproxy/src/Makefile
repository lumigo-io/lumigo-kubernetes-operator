MAKEFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
SRC_ROOT := $(dir $(MAKEFILE_PATH))
VERSION := dev

# build tags required by any component should be defined as an independent variables and later added to GO_BUILD_TAGS below
GO_BUILD_TAGS=""
GOTEST_OPT?= -race -timeout 300s -parallel 4 --tags=$(GO_BUILD_TAGS)
GOTEST_INTEGRATION_OPT?= -race -timeout 360s -parallel 4
GOTEST_OPT_WITH_COVERAGE = $(GOTEST_OPT) -coverprofile=coverage.txt -covermode=atomic
GOTEST_OPT_WITH_INTEGRATION=$(GOTEST_INTEGRATION_OPT) -tags=integration,$(GO_BUILD_TAGS) -run=Integration -coverprofile=integration-coverage.txt -covermode=atomic
GOCMD?= go
GOTEST=$(GOCMD) test
GOOS=$(shell $(GOCMD) env GOOS)
GOARCH=$(shell $(GOCMD) env GOARCH)

TOOLS_MOD_DIR    := $(SRC_ROOT)internal/tools
TOOLS_MOD_REGEX  := "\s+_\s+\".*\""
TOOLS_PKG_NAMES  := $(shell grep -E $(TOOLS_MOD_REGEX) < $(TOOLS_MOD_DIR)/tools.go | tr -d " _\"")
TOOLS_BIN_DIR    := $(SRC_ROOT).tools
TOOLS_BIN_NAMES  := $(addprefix $(TOOLS_BIN_DIR)/, $(notdir $(TOOLS_PKG_NAMES)))

.PHONY: install-tools
install-tools: $(TOOLS_BIN_NAMES)

$(TOOLS_BIN_DIR):
	mkdir -p $@

$(TOOLS_BIN_NAMES): $(TOOLS_BIN_DIR) $(TOOLS_MOD_DIR)/go.mod
	cd $(TOOLS_MOD_DIR) && GOOS= GOARCH= $(GOCMD) build -o $@ -trimpath $(filter %/$(notdir $@),$(TOOLS_PKG_NAMES))

ADDLICENCESE        := $(TOOLS_BIN_DIR)/addlicense
MDLINKCHECK         := $(TOOLS_BIN_DIR)/markdown-link-check
MISSPELL            := $(TOOLS_BIN_DIR)/misspell -error
MISSPELL_CORRECTION := $(TOOLS_BIN_DIR)/misspell -w
LINT                := $(TOOLS_BIN_DIR)/golangci-lint
MULITMOD            := $(TOOLS_BIN_DIR)/multimod
CHLOGGEN            := $(TOOLS_BIN_DIR)/chloggen
GOIMPORTS           := $(TOOLS_BIN_DIR)/goimports
PORTO               := $(TOOLS_BIN_DIR)/porto
CHECKDOC            := $(TOOLS_BIN_DIR)/checkdoc
CROSSLINK           := $(TOOLS_BIN_DIR)/crosslink
GOJUNIT             := $(TOOLS_BIN_DIR)/go-junit-report 
BUILDER             := $(TOOLS_BIN_DIR)/builder

OTELCONTRIBCOL_FILENAME := "./otelcontribcol_$(GOOS)_$(GOARCH)$(EXTENSION)"

.PHONY: otelcontribcolbuilder
otelcontribcolbuilder: install-tools $(BUILDER)
	# Split source generation and compilation in two steps, because the GOARCH and GOOS may be different when cross-compiling
	mkdir -p ./dist
	$(BUILDER) --skip-compilation --config ./builder/config.yaml --version $(VERSION)
	cd ./dist/ && GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 $(GOCMD) build -trimpath -o $(OTELCONTRIBCOL_FILENAME) \
		$(BUILD_INFO) -tags $(GO_BUILD_TAGS) .
