FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/codo ./cmd/codo

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends bash ca-certificates curl \
	&& rm -rf /var/lib/apt/lists/*
RUN useradd --create-home --uid 10001 assistant

COPY --from=build /out/codo /usr/local/bin/codo

USER assistant
WORKDIR /workspace
CMD ["sleep", "infinity"]
