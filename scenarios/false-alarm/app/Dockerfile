FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /calm-svc .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /calm-svc /calm-svc
EXPOSE 8080
ENTRYPOINT ["/calm-svc"]
