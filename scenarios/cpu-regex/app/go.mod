// Standalone module: regex-svc is the system-under-test for the cpu-regex
// scenario. It uses dlclark/regexp2 (a backtracking regex engine) because Go's
// stdlib regexp is RE2-based and cannot exhibit catastrophic backtracking.
module regex-svc

go 1.26

require github.com/dlclark/regexp2 v1.11.5
