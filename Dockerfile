FROM golang:1.26 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /kube-applier-gcp .

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /kube-applier-gcp /kube-applier-gcp
USER 65532:65532
ENTRYPOINT ["/kube-applier-gcp"]
