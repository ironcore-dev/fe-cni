# NAME_PREFIX is the prefix of Kind cluster names created
# by the create_cluster function
NAME_PREFIX="fecni-test-"

# generate_cluster_name TEST_ID
#
# Prints (reasonably) unique name for the cluster in a
# '$NAME_PREFIX-$TEST_ID-<hash>' format.
generate_cluster_name() {
  local test_id=$1; shift

  if [ -z "$test_id" ]; then
     log err "generate_cluster_name: argument TEST_ID is required"
    return 1
  fi

  local hash
  hash=$(openssl rand -hex 4)
  echo "$NAME_PREFIX$test_id-$hash"
}

# cluster_exists NAME
#
# This function returns false if the Kind cluster with the
# given NAME does not exist.
cluster_exists() {
  local name=$1; shift
  if [ -z "$name" ]; then
    log err "cluster_exists: argument NAME is required"
    return 1
  fi

  kind get clusters | grep -qx "$name"
}

# create_cluster TEST_ID CONFIG
#
# This function creates a new Kind cluster using a specific CONFIG manifest. The
# manifests live under the 'manifests/' in the tests root directory. The files are
# in the format "kind_${CONFIG}.yaml". Along with the cluster, the function sets up
# also IPv6 routes for the podCIDR of each individual node in the cluster. The route
# management requires sudo privileges.
# The TEST_ID parameter is an arbitrary string that can be used as a hint to identify
# which test the cluster belongs to. However, multiple calls to create_cluster with the
# same TEST_ID can be made and will result in independent clusters.
#
# The function returns the full name of the cluster it created.
create_cluster() {
  local test_id=$1; shift
  local config=$1; shift

  local name
  name=$(generate_cluster_name "$test_id")

  log info "Creating kind cluster '$name'"
  if cluster_exists "$name"; then
    log err "Cluster '$name' already exists. Perhaps a leftover from a previous test run?"
    return 1
  fi

  kind create cluster --config "${BATS_TEST_DIRNAME}/manifests/kind_${config}.yaml" --name "$name"

  log info "Waiting for podCIDRs to be assigned in the cluster '$name'"
  kind_kubectl "$name" wait --for=jsonpath='{.spec.podCIDR}' node --all --timeout=30s > /dev/null

  declare -A pod_cidr_map
  if ! node_pod_cidrs "$name" pod_cidr_map; then
    log err "Failed to collect podCIDRs of the cluster '$name'"
    return 1
  fi

  local node_ip pod_cidr
  for node_ip in "${!pod_cidr_map[@]}"; do
    pod_cidr="${pod_cidr_map[$node_ip]}"
    log info "Routing podCIDR '$pod_cidr' via '$node_ip'"
    sudo ip -6 route add "$pod_cidr" via "$node_ip"
  done

  echo "$name"
}

# cleanup_clusters CLUSTERS
#
# Removes every Kind cluster in the CLUSTERS argument along with
# any existing routes to its podCIDRs. The function
# tolerates if the cluster with the given name does not exist.
cleanup_clusters() {
  local clusters=$1; shift

  for cluster in $clusters; do
    if ! cluster_exists "$cluster"; then
      log warn "Kind cluster '$cluster' is already gone"
      continue
    fi

    log info "Removing routes to podCIDRs of the cluster '$cluster'"
    local pod_cidr
    for pod_cidr in $(kind_kubectl "$cluster" get no -o jsonpath='{.items[*].spec.podCIDR}'); do
      log on_fail "Removing route to '$pod_cidr'"
      sudo ip -6 route del "$pod_cidr" 2>/dev/null || true
    done

    log info "Removing Kind cluster '$cluster'"
    kind delete cluster --name "$cluster"
  done
}

