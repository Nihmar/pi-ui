// Package spike measures the Phase 1 spike on the host it runs on
// (docs/spike-interfaces.md §5.6, §12): child spawn latency and resident memory,
// and end-to-end WebSocket throughput and latency through the real
// rpc → sessions → ws pipeline, driven by the deterministic fake-pi child.
//
// Nothing here invents numbers: every result carries the raw samples it was
// derived from, and a non-empty Config.RawDir stores them as JSON next to the
// summary, so the spike report can point at the exact run that produced it.
//
// Measurements that read /proc are Linux-only. ProcessRSSMiB and the throughput
// memory series return a descriptive error elsewhere; spawn and throughput
// measurement themselves still run on any platform, minus the memory numbers.
package spike
