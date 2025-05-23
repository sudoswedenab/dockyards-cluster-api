FROM docker.io/library/golang:1.24.3 AS builder
COPY . /src
WORKDIR /src
ENV CGO_ENABLED=0
RUN go build -o dockyards-cluster-api -ldflags="-s -w" .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /src/dockyards-cluster-api /usr/bin/dockyards-cluster-api
ENTRYPOINT ["/usr/bin/dockyards-cluster-api"]
