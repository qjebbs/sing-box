# sing-box

The universal proxy platform.

[![Packaging status](https://repology.org/badge/vertical-allrepos/sing-box.svg)](https://repology.org/project/sing-box/versions)

## Documentation

https://sing-box.sagernet.org

For extended features

> [!WARNING]
> Outbound groups, including `urltest` / `loadbalance` are significantly changed. Check the doc above to update your configuration.
> [!WARNING]
> The `chain` outbound is renamed to `relay` and lists its nodes in the order the connection passes through them, so the last one is the final exit. An existing chain has to be renamed and reversed.

- Providers: [中文](./docs/configuration/provider/index.zh.md), [English](./docs/configuration/provider/index.md)
- URL Test: [中文](./docs/configuration/outbound/urltest.zh.md), [English](./docs/configuration/outbound/urltest.md)
- Load Balance: [中文](./docs/configuration/outbound/loadbalance.zh.md), [English](./docs/configuration/outbound/loadbalance.md)
- Relay: [中文](./docs/configuration/outbound/relay.zh.md), [English](./docs/configuration/outbound/relay.md)
- Fallback: [中文](./docs/configuration/outbound/fallback.zh.md), [English](./docs/configuration/outbound/fallback.md)
- Health Check: [中文](./docs/configuration/shared/health-check.zh.md), [English](./docs/configuration/shared/health-check.md)
- Muiltifile Configuration Extended: [中文](./docs/configuration/index.zh.md), [English](./docs/configuration/index.md)

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