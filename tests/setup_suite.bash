setup_suite() {
  # Test helpers
  load "${BATS_TEST_DIRNAME}"/lib/common.bash
  load "${BATS_TEST_DIRNAME}"/lib/kind.bash

  preflight_check
  local leftover_clusters
  leftover_clusters="$(list_test_clusters)"
  if [ -n "$leftover_clusters" ]; then
    log warn "There are Kind clusters matching FeCNI test naming convention. Cleaning up!"
    cleanup_clusters
  fi
}

teardown_suite() {
  # Test helpers
  load "${BATS_TEST_DIRNAME}"/lib/common.bash
  load "${BATS_TEST_DIRNAME}"/lib/kind.bash

  # Remove all remaining Kind clusters that might've
  # leaked from the tests.
  cleanup_clusters
}
