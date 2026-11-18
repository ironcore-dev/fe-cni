# deploy_fecni CLUSTER
#
# Deploys FeCNI from an image in the project's build directory
# into the Kind CLUSTER.
deploy_fecni() {
  local cluster=$1; shift

  local fecni_image
  fecni_image=$(fecni_image_path)

  log info "Uploading FeCNI image ($fecni_image) to cluster '$cluster'"
  kind load image-archive "$fecni_image" --name "$cluster"

  kind_kubectl "$cluster" apply -f "$FECNI_MANIFEST"
}

# node_pod_cidrs CLUSTER OUT_MAP
#
# Fills the associative array OUT_MAP with the hostIP:podCIDR pairs of all
# nodes in the given CLUSTER. Node InternalIPs serve as the keys, their
# podCIDRs as the values. The OUT_MAP has to be declared by the caller,
# e.g.:
#   declare -A cidrs
#   node_pod_cidrs my-cluster cidrs
node_pod_cidrs() {
  local cluster=$1; shift
  local map_name=$1; shift

  if [ -z "$map_name" ]; then
    log err "node_pod_cidrs: argument OUT_MAP is required"
    return 1
  fi
  if ! declare -p "$map_name" 2>/dev/null | grep -q '^declare -[^ ]*A'; then
    log err "node_pod_cidrs: OUT_MAP must be a declared associative array (declare -A $map_name)"
    return 1
  fi

  local -n _node_pod_cidrs_map="$map_name"
  local ip cidr

  while read -r ip cidr; do
    if [ -z "$ip" ] || [ -z "$cidr" ]; then
      log err "node_pod_cidrs: a node in cluster '$cluster' is missing InternalIP or podCIDR, is the cluster fully registered?"
      return 1
    fi
    # shellcheck disable=SC2034
    _node_pod_cidrs_map["$ip"]="$cidr"
  done < <(kind_kubectl "$cluster" get nodes -o jsonpath='{range .items[*]}{.status.addresses[?(@.type=="InternalIP")].address}{"\t"}{.spec.podCIDR}{"\n"}{end}')
}
