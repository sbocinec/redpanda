/*
 * Copyright 2021 Redpanda Data, Inc.
 *
 * Use of this software is governed by the Business Source License
 * included in the file licenses/BSL.md
 *
 * As of the Change Date specified in that file, in accordance with
 * the Business Source License, use of this software will be governed
 * by the Apache License, Version 2.0
 */

#include "partition_proxy.h"

#include "cluster/partition_manager.h"
#include "kafka/server/replicated_partition.h"

namespace kafka {

template<typename Impl, typename... Args>
partition_proxy make_with_impl(Args&&... args) {
    return partition_proxy(std::make_unique<Impl>(std::forward<Args>(args)...));
}

std::optional<partition_proxy> make_partition_proxy(
  const model::ktp& ktp, cluster::partition_manager& cluster_pm) {
    auto partition = cluster_pm.get(ktp);
    if (partition) {
        return make_with_impl<replicated_partition>(partition);
    }
    return std::nullopt;
}

} // namespace kafka
