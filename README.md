# sing-box

The universal proxy platform.

[![Packaging status](https://repology.org/badge/vertical-allrepos/sing-box.svg)](https://repology.org/project/sing-box/versions)

## Documentation

https://sing-box.sagernet.org

For extended features

- Providers: [中文](./docs/configuration/provider/index.zh.md), [English](./docs/configuration/provider/index.md)
- Load Balance: [中文](./docs/configuration/outbound/loadbalance.zh.md), [English](./docs/configuration/outbound/loadbalance.md)
- Proxy Chain: [中文](./docs/configuration/outbound/chain.zh.md), [English](./docs/configuration/outbound/chain.md)
- Health Check: [中文](./docs/configuration/shared/health-check.zh.md), [English](./docs/configuration/shared/health-check.md)
- Muiltifile Configuration Extended: [中文](./docs/configuration/index.zh.md), [English](./docs/configuration/index.md)

Warn: The health check feature has been moved to the top-level `health_checks` option, and URLTest/Loadbalance outbounds now require an explicit `health_check` field. You may need to update your configuration.

Warn: The `leastload` / `leastping` objectives no longer accept `expected` and `baselines`. They now rank the nodes by a score and select the nodes within a relative `tolerance` of the best one. You may need to update your configuration, see [Load Balance](./docs/configuration/outbound/loadbalance.md). The new algorithm gives a better network quality in practice: it no longer pads the selection with nodes that are more than twice as slow as the best one just to reach a node count.

Warn: The `urltest` outbound now ranks the nodes by a score, the median delay of the health check results plus the share of the failed checks of the timeout they waited for. Its `tolerance` is compared with that score and is relative to it now: `0.2` means 20% of the score of the best node, not a delay in milliseconds, and values of `1` or more are rejected. The selection no longer follows a single check result, so it is more stable, but a node that failed a check recently is demoted. You may need to update your configuration, see [URLTest](./docs/configuration/outbound/urltest.md).

Warn: The `log_health` option of the `loadbalance` outbound is removed. Node statistics are now logged after every health check round, as long as the group is in use, and the `urltest` outbound does the same. You may need to remove `log_health` from your configuration.

## License

```
Copyright (C) 2022 by nekohasekai <contact-sagernet@sekai.icu>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.

In addition, no derivative work may use the name or imply association
with this application without prior consent.
```