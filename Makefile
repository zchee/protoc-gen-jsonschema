# ----------------------------------------------------------------------------
# global

APP = protoc-gen-jsonschema
PROTOC := protoc
GO_PATH = $(shell go env GOPATH)

out_path = out
comma := ,
empty:=
space := $(empty) $(empty)

importmaps := \
	${GO_PATH}/src/istio.io/api \
	gogoproto/gogo.proto=${GO_PATH}/src/github.com/gogo/protobuf/gogoproto \
	google/protobuf/any.proto=${GO_PATH}/src/github.com/gogo/protobuf/types \
	google/protobuf/descriptor.proto=${GO_PATH}/src/github.com/gogo/protobuf/protoc-gen-gogo/descriptor \
	google/protobuf/duration.proto=${GO_PATH}/src/github.com/gogo/protobuf/types \
	google/protobuf/struct.proto=${GO_PATH}/src/github.com/gogo/protobuf/types \
	google/protobuf/timestamp.proto=${GO_PATH}/src/github.com/gogo/protobuf/types \
	google/protobuf/wrappers.proto=${GO_PATH}/src/github.com/gogo/protobuf/types \
	google/rpc/status.proto=${GO_PATH}/src/github.com/gogo/googleapis/google/rpc \
	google/rpc/code.proto=${GO_PATH}/src/github.com/gogo/googleapis/google/rpc \
	google/rpc/error_details.proto=${GO_PATH}/src/github.com/gogo/googleapis/google/rpc \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
imports_with_spaces := $(foreach import,$(importmaps),-I$(import),)
imports := $(foreach import,$(importmaps),-I$(import))
# imports := $(subst $(space),$(empty),$(imports_with_spaces))

jsonschema_prefix := --jsonschema_out=
jsonschema_options := allow_null_values,disallow_additional_properties,disallow_bigints_as_strings,debug
jsonschema_plugin := $(jsonschema_prefix)$(jsonschema_options):$(out_path)

# ----------------------------------------------------------------------------
# target

.PHONY: out/clean
out/clean:
	@${RM} -r out

out: out/clean
	@mkdir -p out

.PHONY: test/istio/mcp
test/istio/mcp: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/mcp/v1alpha1/envelope.proto ${GO_PATH}/src/istio.io/api/mcp/v1alpha1/mcp.proto ${GO_PATH}/src/istio.io/api/mcp/v1alpha1/metadata.proto

.PHONY: test/istio/mesh
test/istio/mesh: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/mesh/v1alpha1/config.proto ${GO_PATH}/src/istio.io/api/mesh/v1alpha1/network.proto ${GO_PATH}/src/istio.io/api/mesh/v1alpha1/proxy.proto

.PHONY: test/istio/mixer
test/istio/mixer: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/mixer/v1/attributes.proto ${GO_PATH}/src/istio.io/api/mixer/v1/mixer.proto
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/mixer/v1/config/client/api_spec.proto ${GO_PATH}/src/istio.io/api/mixer/v1/config/client/client_config.proto ${GO_PATH}/src/istio.io/api/mixer/v1/config/client/quota.proto ${GO_PATH}/src/istio.io/api/mixer/v1/config/client/service.proto
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/mixer/adapter/model/v1beta1/quota.proto ${GO_PATH}/src/istio.io/api/mixer/adapter/model/v1beta1/report.proto ${GO_PATH}/src/istio.io/api/mixer/adapter/model/v1beta1/template.proto
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/policy/v1beta1/cfg.proto ${GO_PATH}/src/istio.io/api/policy/v1beta1/type.proto policy/v1beta1/value_type.proto

.PHONY: test/istio/routing
test/istio/routing: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/networking/v1alpha3/destination_rule.proto ${GO_PATH}/src/istio.io/api/networking/v1alpha3/envoy_filter.proto ${GO_PATH}/src/istio.io/api/networking/v1alpha3/gateway.proto ${GO_PATH}/src/istio.io/api/networking/v1alpha3/service_dependency.proto ${GO_PATH}/src/istio.io/api/networking/v1alpha3/service_entry.proto ${GO_PATH}/src/istio.io/api/networking/v1alpha3/virtual_service.proto

.PHONY: test/istio/rbac
test/istio/rbac: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/rbac/v1alpha1/rbac.proto

.PHONY: test/istio/authn
test/istio/authn: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/authentication/v1alpha1/policy.proto

.PHONY: test/istio/envoy
test/istio/envoy: out static
	@PATH=$(CURDIR):$$PATH $(PROTOC) -I${GO_PATH}/src/istio.io/api $(imports) $(jsonschema_plugin) ${GO_PATH}/src/istio.io/api/envoy/config/filter/http/authn/v2alpha1/config.proto ${GO_PATH}/src/istio.io/api/envoy/config/filter/http/jwt_auth/v2alpha1/config.proto ${GO_PATH}/src/istio.io/api/envoy/config/filter/network/tcp_cluster_rewrite/v2alpha1/config.proto

.PHONY: test/istio
test/istio: test/istio/mcp test/istio/mesh test/istio/mixer test/istio/routing test/istio/rbac test/istio/authn test/istio/envoy

# ----------------------------------------------------------------------------
# include
include hack/make/go.mk

# ----------------------------------------------------------------------------
# override

.DEFAULT_GOAL = static
