FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
	-o /build/bin/fecni \
	./cmd/fecni && \
	CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
	-o /build/bin/fecni-init \
	./cmd/fecni-init

FROM alpine:3.22

COPY --from=builder /build/bin/fecni /fecni
COPY --from=builder /build/bin/fecni-init /fecni-init

ENTRYPOINT ["/fecni"]
