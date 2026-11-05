FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY plugins/example/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /plugin ./main.go

FROM scratch
COPY --from=build /plugin /plugin
USER 65534:65534
ENTRYPOINT ["/plugin"]
