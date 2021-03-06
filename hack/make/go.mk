# ----------------------------------------------------------------------------
# global

SHELL = /usr/bin/env bash

GO_PATH ?= $(shell go env GOPATH)
GO_OS ?= $(shell go env GOOS)
GO_ARCH ?= $(shell go env GOARCH)
GOLANG_VERSION ?= 1.11
ALPINE_VERSION ?= 3.9

PKG := $(subst $(GO_PATH)/src/,,$(CURDIR))
GO_PKGS := $(shell go list ./... | grep -v -e '.pb.go')
GO_APP_PKGS := $(shell go list -f '{{if and (or .GoFiles .CgoFiles) (ne .Name "main")}}{{.ImportPath}}{{end}}' ${PKG}/...)
GO_TEST_PKGS := $(shell go list -f='{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
GO_VENDOR_PKGS=
ifneq ($(wildcard ./vendor),)
	GO_VENDOR_PKGS = $(shell go list -f '{{if and (or .GoFiles .CgoFiles) (ne .Name "main")}}./vendor/{{.ImportPath}}{{end}}' ./vendor/...)
endif

GO_TEST ?= go test
GO_TEST_FUNC ?= .
GO_TEST_FLAGS ?=
GO_BENCH_FUNC ?= .
GO_BENCH_FLAGS ?= -benchmem

VERSION := $(shell cat VERSION.txt)
GIT_COMMIT := $(shell git rev-parse --short HEAD)
GIT_UNTRACKED_CHANGES := $(shell git status --porcelain --untracked-files=no)
# disable '-dirty' suffix if $CLOUD_BUILD, $CIRCLECI are not empty
ifeq (${CIRCLECI},)
ifneq (${GIT_UNTRACKED_CHANGES},)
	GIT_COMMIT := $(GIT_COMMIT)-dirty
endif
endif
CTIMEVAR := -X main.version=v$(VERSION) -X main.gitCommit=$(GIT_COMMIT)

CGO_ENABLED ?= 0
GO_LDFLAGS=-s -w $(CTIMEVAR)
GO_LDFLAGS_STATIC=-s -w '-extldflags=-static' $(CTIMEVAR)

# disable '-d' ldflags if $CIRCLECI is not empty, or ($GO_OS != darwin)
ifeq (${CIRCLECI},)
ifneq (${GO_OS},darwin)
	GO_LDFLAGS_STATIC+=-d
endif
endif

GO_BUILDTAGS := osusergo netgo
GO_BUILDTAGS_STATIC=static static_build
GO_FLAGS ?= -tags='$(GO_BUILDTAGS)' -ldflags="${GO_LDFLAGS}"
GO_INSTALLSUFFIX_STATIC=netgo

ifneq ($(wildcard go.mod),)  # exist go.mod
ifeq ($(GO111MODULE),on)
	GO_FLAGS+=-mod=vendor
endif
endif

CONTAINER_REGISTRY := gcr.io/container-image/microservice-platform
CONTAINER_BUILD_TAG ?= $(VERSION)
CONTAINER_BUILD_ARGS_BASE = --rm --pull --build-arg GOLANG_VERSION=${GOLANG_VERSION} --build-arg ALPINE_VERSION=${ALPINE_VERSION}
ifneq (${SHORT_SHA},)
	CONTAINER_BUILD_ARGS_BASE+=--build-arg SHORT_SHA=${SHORT_SHA}
endif
CONTINUOUS_INTEGRATION ?=
ifneq (${CONTINUOUS_INTEGRATION},)
	CONTAINER_BUILD_ARGS_BASE+=--progress=plain
endif
CONTAINER_BUILD_ARGS ?= ${CONTAINER_BUILD_ARGS_BASE}
CONTAINER_BUILD_TARGET ?= ${APP}
ifneq (${CONTAINER_BUILD_TARGET},${APP})
	CONTAINER_BUILD_TAG=${VERSION}-${CONTAINER_BUILD_TARGET}
endif

# ----------------------------------------------------------------------------
# defines

GOPHER = ""
define target
@printf "$(GOPHER)  \\x1b[1;32m$(patsubst ,$@,$(1))\\x1b[0m\\n"
endef

# ----------------------------------------------------------------------------
# targets

## build and install

.PHONY: $(APP)
$(APP): VERSION.txt
	$(call target)
	@CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GO_OS) GOARCH=$(GO_ARCH) go build -v $(strip $(GO_FLAGS)) -o $(APP) $(CMD)

.PHONY: build
build: $(APP)  ## Builds a dynamic executable or package.

.PHONY: static
static: GO_LDFLAGS=${GO_LDFLAGS_STATIC}
static: GO_FLAGS+=-installsuffix ${GO_INSTALLSUFFIX_STATIC}
static: GO_BUILDTAGS+=${GO_BUILDTAGS_STATIC}
static: $(APP)  ## Builds a static executable or package.

.PHONY: install
install: GO_LDFLAGS=${GO_LDFLAGS_STATIC}
install: GO_FLAGS+=-a -installsuffix ${GO_INSTALLSUFFIX_STATIC}
install: GO_BUILDTAGS+=${GO_BUILDTAGS_STATIC}
install:  ## Installs the executable or package.
	$(call target)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GO_OS) GOARCH=$(GO_ARCH) go install -v $(strip $(GO_FLAGS)) $(CMD)

.PHONY: pkg/install
pkg/install: GO_LDFLAGS=${GO_LDFLAGS_STATIC}
pkg/install: GO_FLAGS+=-installsuffix ${GO_INSTALLSUFFIX_STATIC}
pkg/install: GO_BUILDTAGS+=${GO_BUILDTAGS_STATIC}
pkg/install:
	$(call target)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GO_OS) GOARCH=$(GO_ARCH) go install -v ${GO_APP_PKGS}


## test, bench and coverage

.PHONY: test
test:  ## Run the package test with checks race condition.
	$(call target)
	$(GO_TEST) -v -race $(strip $(GO_FLAGS)) -run=$(GO_TEST_FUNC) $(GO_TEST_PKGS)

.PHONY: bench
bench:  ## Take a package benchmark.
	$(call target)
	$(GO_TEST) -v $(strip $(GO_FLAGS)) -run='^$$' -bench=$(GO_BENCH_FUNC) -benchmem $(GO_TEST_PKGS)

.PHONY: bench/race
bench/race:  ## Take a package benchmark with checks race condition.
	$(call target)
	$(GO_TEST) -v -race $(strip $(GO_FLAGS)) -run='^$$' -bench=$(GO_BENCH_FUNC) -benchmem $(GO_TEST_PKGS)

.PHONY: bench/trace
bench/trace:  ## Take a package benchmark with take a trace profiling.
	$(GO_TEST) -v -c -o bench-trace.test $(PKG)/stackdriver
	GODEBUG=allocfreetrace=1 ./bench-trace.test -test.run=none -test.bench=$(GO_BENCH_FUNC) -test.benchmem -test.benchtime=10ms 2> trace.log

.PHONY: coverage
coverage:  ## Take test coverage.
	$(call target)
	$(GO_TEST) -v -race $(strip $(GO_FLAGS)) -covermode=atomic -coverpkg=$(PKG)/... -coverprofile=coverage.out $(GO_PKGS)

$(GO_PATH)/bin/go-junit-report:
	@GO111MODULE=off go get -u github.com/jstemmer/go-junit-report

.PHONY: cmd/go-junit-report
cmd/go-junit-report: $(GO_PATH)/bin/go-junit-report  # go get 'go-junit-report' binary

.PHONY: coverage/ci
coverage/ci: GO_LDFLAGS=${GO_LDFLAGS_STATIC}
coverage/ci: GO_FLAGS+=-a -installsuffix ${GO_INSTALLSUFFIX_STATIC}
coverage/ci: GO_BUILDTAGS+=${GO_BUILDTAGS_STATIC}
coverage/ci: cmd/go-junit-report  ## Take test coverage.
	$(call target)
	@mkdir -p /tmp/ci/artifacts /tmp/ci/test-results
	$(GO_TEST) -v -race $(strip $(GO_FLAGS)) -covermode=atomic -coverpkg=$(PKG)/... -coverprofile=/tmp/ci/artifacts/coverage.out $(GO_PKGS) 2>&1 | tee /dev/stderr | go-junit-report -set-exit-code > /tmp/ci/test-results/junit.xml
	@if [[ -f '/tmp/ci/artifacts/coverage.out' ]]; then go tool cover -html=/tmp/ci/artifacts/coverage.out -o /tmp/ci/artifacts/coverage.html; fi


## lint

.PHONY: lint
lint: lint/golangci-lint  ## Run all linters.

.PHONY: lint/fmt
lint/fmt:  ## Verifies all files have been `gofmt`ed.
	$(call target)
	@gofmt -s -l . 2>&1 | grep -v -E -e 'testdata' -e 'vendor' -e '\.pb.go' -e '_.*' | tee /dev/stderr

.PHONY: lint/govet
lint/govet:  ## Verifies `go vet` passes.
	$(call target)
	@go vet -all $(GO_PKGS) | tee /dev/stderr

$(GO_PATH)/bin/golangci-lint:
	@GO111MODULE=off go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

.PHONY: cmd/golangci-lint
cmd/golangci-lint: $(GO_PATH)/bin/golangci-lint  # go get 'golangci-lint' binary

.PHONY: golangci-lint
lint/golangci-lint: cmd/golangci-lint .golangci.yml  ## Run golangci-lint.
	$(call target)
	@golangci-lint run ./...


## mod

.PHONY: mod/init
mod/init:
	$(call target)
	@GO111MODULE=on go mod init || true

.PHONY: mod/goget
mod/goget:  ## Update module and go.mod.
	$(call target)
	@GO111MODULE=on go get -u -m -v -x ./...

.PHONY: mod/tidy
mod/tidy:
	$(call target)
	@GO111MODULE=on go mod tidy -v

.PHONY: mod/vendor
mod/vendor:
	$(call target)
	@GO111MODULE=on go mod vendor -v

.PHONY: mod/graph
mod/graph:
	$(call target)
	@GO111MODULE=on go mod graph

.PHONY: mod/clean
mod/clean:
	$(call target)
	@$(RM) go.mod go.sum
	@$(RM) -r vendor

.PHONY: mod/install
mod/install: GO_LDFLAGS=${GO_LDFLAGS_STATIC}
mod/install: GO_FLAGS+=-installsuffix ${GO_INSTALLSUFFIX_STATIC}
mod/install: mod/tidy mod/vendor
	$(call target)
	@GO111MODULE=off go install -v $(GO_VENDOR_PKGS) || GO111MODULE=on go install -mod=vendor -v $(GO_VENDOR_PKGS)

.PHONY: mod
mod: mod/init mod/tidy mod/vendor mod/install  ## Updates the vendoring directory via go mod.
	@sed -i ':a;N;$$!ba;s|go 1\.12\n\n||g' go.mod

.PHONY: mod/update
mod/update: mod/goget mod/tidy mod/vendor mod/install  ## Updates all vendor packages.


## miscellaneous

.PHONY: container/build
container/build:  ## Create the container image from the Dockerfile.
	docker image build ${CONTAINER_BUILD_ARGS} --target ${CONTAINER_BUILD_TARGET} -t $(CONTAINER_REGISTRY)/$(APP):${CONTAINER_BUILD_TAG} .

.PHONY: container/push
container/push:  ## Push the container image to $CONTAINER_REGISTRY
	docker image push $(CONTAINER_REGISTRY)/$(APP):$(VERSION)


.PHONY: boilerplate/go/%
boilerplate/go/%: BOILERPLATE_PKG_DIR=$(shell printf $@ | cut -d'/' -f3- | rev | cut -d'/' -f2- | rev)
boilerplate/go/%: BOILERPLATE_PKG_NAME=$(if $(findstring $@,cmd),main,$(shell printf $@ | rev | cut -d/ -f2 | rev))
boilerplate/go/%: hack/boilerplate/boilerplate.go.txt  ## Create go file from boilerplate.go.txt
	@if [ ${BOILERPLATE_PKG_DIR} != "*.go" ] && [ ! -d ${BOILERPLATE_PKG_DIR} ]; then mkdir -p ${BOILERPLATE_PKG_DIR}; fi
	@cat hack/boilerplate/boilerplate.go.txt <(printf "package ${BOILERPLATE_PKG_NAME}\\n") > $*
	@sed -i "s|YEAR|$(shell date '+%Y')|g" $*


.PHONY: AUTHORS
AUTHORS:  ## Creates AUTHORS file.
	@$(file >$@,# This file lists all individuals having contributed content to the repository.)
	@$(file >>$@,# For how it is generated, see `make AUTHORS`.)
	@printf "$(shell git log --format="\n%aN <%aE>" | LC_ALL=C.UTF-8 sort -uf)" >> $@


.PHONY: clean
clean:  ## Cleanup any build binaries or packages.
	$(call target)
	@$(RM) $(APP) *.out *.test *.prof trace.log


.PHONY: help
help:  ## Show make target help.
	@perl -nle 'BEGIN {printf "Usage:\n  make \033[33m<target>\033[0m\n\nTargets:\n"} printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2 if /^([a-zA-Z\/_-].+)+:.*?\s+## (.*)/' ${MAKEFILE_LIST}
