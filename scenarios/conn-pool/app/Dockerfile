# Build a static pool-svc binary and ship it on a minimal base.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /pool-svc .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /pool-svc /pool-svc
EXPOSE 8080
ENTRYPOINT ["/pool-svc"]
