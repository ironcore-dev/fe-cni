# log LEVEL MESSAGE
#
# Output a TAP-compliant message that displays nicely in the BATS logs. The
# MESSAGE is prepended with a severity indicator based on the provided LEVEL.
# Accepted LEVEL values:
#   * info
#   * warn
#   * err
#   * on_fail
#
# Note that the messages with "on_fail" level are displayed only if
# the test fails.
log() {
  if [ "$#" -ne 2 ]; then
    log err "Function '${FUNCNAME[0]}' expects exactly 2 arguments. $# given."
    return 1
  fi
  local level=$1; shift
  local message=$1; shift

  case "$level" in
    "info")
      echo "# INFO: $message" >&3
      ;;
    "warn")
      echo "# WARN: $message" >&3
      ;;
    "err")
      echo "# ERROR: $message" >&3
      ;;
    "on_fail")
      echo "$message"
      ;;
    *)
      echo "Unknown log level given to 'log' function: $level"
      return 1
      ;;
  esac
}

# preflight_check
#
# The function basic requirements required for tests to run. If any
# of the dependencies are missing, it fails.
preflight_check() {
    if ! uname -o | grep -iq linux; then
      log err "E2E tests are supported only on linux"
      return 1
    fi

    if ! sudo -n id &>/dev/null; then
      log err "E2E tests require passwordless sudo access"
    fi

    check_env
    check_build
}

# fecni_image_path
#
# Returns path to the OCI image of the FeCNI for the current architecture
# from the project's build directory. The image is always built for Linux.
fecni_image_path() {
  local arch

  arch=$(go env GOARCH)
  echo "$FECNI_BUILD_DIR/fecni_linux_${arch}_${FECNI_VERSION}.tar"
}

# check_env
#
# Ensures that all variables that are typically required by tests
# are present. If any of them is missing, the function returns non-zero
# return code.
check_env() {
  local required_vars="FECNI_BUILD_DIR FECNI_VERSION FECNI_MANIFEST"
  local missing=0

  for var in $required_vars; do
    if [ -z "${!var}" ]; then
      log err  "Missing environment variable '$var'"
      (( missing +=1 ))
    fi
  done

  return $missing
}

# check_build
#
# Ensures that the OCI image file for the current architecture is present in the
# build directory. It returns non-zero return code if the file is missing.
check_build() {
  local image_path
  image_path="$(fecni_image_path)"
  if ! test -f "$image_path"; then
    log err "Missing FeCNI container image: $image_path"
    return 1
  fi
}

# kind_kubectl CLUSTER [ARGS ...]
#
# A wrapper for the kubectl command that adds correct --context
# for the given CLUSTER. Variadic arguments ARGS are passed to the
# kubectl unchanged.
kind_kubectl() {
  local cluster=$1; shift
  kubectl --context "kind-$cluster" "$@"
}

# mesh_ping CLUSTER
#
# Deploys the probe DaemonSet on the given CLUSTER and verifies the
# connectivity: first, every probe pod is pinged from the current host
# (which relies on the podCIDR routes added by create_cluster), then every
# pod pings every other pod in a full-mesh fashion. The function returns
# non-zero if any of the pings fail.
mesh_ping() {
  local cluster=$1; shift

  log info "Deploying the probe DaemonSet on the cluster '$cluster'"
  kind_kubectl "$cluster" apply -f "${BATS_TEST_DIRNAME}/manifests/probes.yaml"
  log info "Waiting for the probe DaemonSet to be ready on the cluster '$cluster'"
  kind_kubectl "$cluster" rollout status daemonset/probe --timeout=60s

  local probes
  probes=$(kind_kubectl "$cluster" get pods -l app=probe -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.podIP}{"\n"}{end}')

  if [ -z "$probes" ]; then
    log err "No probe pods found in cluster '$cluster'"
    return 1
  fi

  local failed=0
  local pod ip target_pod target_ip

  log info "Pinging the probe pods from the host on the cluster '$cluster'"
  while read -r pod ip; do
    if [ -z "$pod" ] || [ -z "$ip" ]; then
      continue
    fi
    log info "Ping host -> '$pod' ($ip)"
    if ! ping -6 -c 1 -W 2 "$ip" > /dev/null; then
      log err "Failed to ping pod '$pod' ($ip) from the host"
      (( failed += 1 ))
    fi
  done <<< "$probes"

  log info "Running the full-mesh ping between the probe pods on the cluster '$cluster'"
  while read -r pod ip; do
    while read -r target_pod target_ip; do
      if [ -z "$target_pod" ] || [ -z "$target_ip" ] || [ "$pod" = "$target_pod" ]; then
        continue
      fi
      log info "Ping '$pod' -> '$target_pod' ($target_ip)"
      if ! kind_kubectl "$cluster" exec "$pod" -- ping -c 1 -W 2 "$target_ip" > /dev/null; then
        log err "Failed to ping pod '$target_pod' ($target_ip) from pod '$pod'"
        (( failed += 1 ))
      fi
    done <<< "$probes"
  done <<< "$probes"

  return $failed
}
