# Build a static leaky-svc binary and ship it on a minimal base.
# The build context is scenarios/oom-killed/app (a standalone, stdlib-only
# module), so this image builds fast with nothing to download.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /leaky-svc .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /leaky-svc /leaky-svc
EXPOSE 8080
ENTRYPOINT ["/leaky-svc"]
