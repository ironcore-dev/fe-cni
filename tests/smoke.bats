setup_file() {
    load "${BATS_TEST_DIRNAME}"/lib/common.bash
    check_env
    check_build
}

setup() {
    # BATS utilities
    load "${BATS_TEST_DIRNAME}"/.bats/assert/load.bash
    load "${BATS_TEST_DIRNAME}"/.bats/support/load.bash

    # Test helpers
    load "${BATS_TEST_DIRNAME}"/lib/common.bash
    load "${BATS_TEST_DIRNAME}"/lib/k8s.bash
    load "${BATS_TEST_DIRNAME}"/lib/kind.bash

    # Tests can add space-separated names of Kind cluster to this variable
    # and they will be cleaned up on teardown.
    export TEARDOWN_CLUSTERS=""
}

teardown() {
    cleanup_clusters "$TEARDOWN_CLUSTERS"
}

register_tests() {
    bats_test_function \
        --description "Basic test of the FeCNI functionality" \
        -- test_smoke
}

# test_smoke
#
# This test follows a happy path of deploying FeCNI on correctly configured
# multi-node k8s cluster. It then deploys DaemonSet and ensures that all pods can be
# reached from the host and among each other.
test_smoke() {
    local cluster_name
    cluster_name=$(create_cluster smoke default)
    TEARDOWN_CLUSTERS+=" $cluster_name"

    deploy_fecni "$cluster_name"
    log info "Waiting for nodes in the '$cluster_name' to become ready"
    kind_kubectl "$cluster_name" wait --for=condition=Ready node --all --timeout=30s

    mesh_ping "$cluster_name"
}

register_tests
