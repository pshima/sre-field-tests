// Standalone module: leaky-svc is the system-under-test for the oom-killed
// scenario. It is intentionally separate from the benchmark module and uses
// only the standard library, so its container image builds from a tiny context
// with no dependencies to resolve.
module leaky-svc

go 1.26
