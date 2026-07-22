// Package state is the shared in-memory hub that feeds the web dashboard's
// Server-Sent Events stream. Both the OLED display goroutines and the api
// package's app-liveness collector publish into a single Hub; the api package's
// /events handler subscribes to it. This is the contract package the two sides
// implement against, so the wire format and the publish/subscribe lifecycle are
// documented here rather than at either call site.
//
// # Wire contract
//
// Snapshot's JSON tags ARE the dashboard protocol. The keys and casing must
// match the seed() object in website/dashboard.html exactly, because the
// browser applies every message by merging its top-level keys into local state.
// Bytes are raw integers (the front-end formats them); durations are whole
// seconds; Tunnel.Type is one of "AUTOSSH", "WIREGUARD", "TAILSCALE".
//
// # Two message shapes, one merge
//
// A client's first event is the full Snapshot (every key), written by the
// subscriber from the initial value Subscribe returns. Every subsequent event
// is a minimal per-slice patch — {"cpu":{…}}, {"tunnels":[{…}]}, {"ssd":{…}} —
// emitted by the matching PublishX method. The front-end merges both shapes the
// same way: top-level object keys replace, and tunnels upsert by Type+Name so
// user-configurable display names cannot make two sources overwrite each other.
// Keeping per-tick messages to a single changed slice is deliberate: it minimizes
// JSON marshaled and bytes sent on a low-power device where many collectors tick
// independently.
//
// # Concurrency and lifecycle
//
// Publishing and subscribing are safe from any goroutine. Publish does a
// non-blocking send to each subscriber and drops a frame for any client whose
// buffer is full, so a stalled browser can never block a collector; because
// each delivered update carries current state, a dropped frame only costs that
// client latency. Subscribe computes the initial snapshot and registers the
// client's channel under one lock, so a client can neither miss an update that
// races its connect nor receive one already in its initial frame. Every
// Subscribe returns a cancel func the caller must invoke (typically deferred)
// to unsubscribe and release the channel.
package state
