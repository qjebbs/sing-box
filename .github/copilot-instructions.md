# Working notes

## Outbound chain

`adapter.OutboundChain` travels in the connection context and is filled by the outbounds
while a connection is dialed (`adapter.RecordOutboundChain`). Only the caller knows the dial
is over, because hops record themselves on the way back:

- `Seal()` — the dial phase is over, in `route.ConnectionManager`, right after the dial
  succeeds (`NewConnection`, `NewPacketConnection`). A failed dial does not seal.
- `HandOver()` — the connection is carried by an outbound that does not dial.
- `Fail()` — the dial failed (`ConnectionManager.reportDialFailure`), so the chain stays
  unsealed by design.

`route.Router` calls `warnUnsealedChain` after the connection manager returns: a chain that
is neither sealed nor failed means a dial path was added without a seal, and the connection
silently never reaches the API.

Checklist when merging from upstream:

- A new dial path in the connection manager must seal the chain when it succeeds, e.g.
  `adapter.OutboundChainFrom(ctx).Seal()`; otherwise the warning fires for every connection.
- A new dial failure branch must call `reportDialFailure` (it feeds the group failure records
  and marks the chain failed); otherwise the warning fires for every failed dial.
- A new route entry point that creates a chain must end it (`Seal` / `HandOver`) and call
  `warnUnsealedChain`.
- Never seal inside an outbound or a hop.
