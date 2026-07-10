// Standalone module: deploy-svc is the system-under-test for the bad-deploy
// scenario. Stdlib-only, so its image builds from a tiny context.
module deploy-svc

go 1.26
