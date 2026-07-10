FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /deploy-svc .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /deploy-svc /deploy-svc
EXPOSE 8080
ENTRYPOINT ["/deploy-svc"]
