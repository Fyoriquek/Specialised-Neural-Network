# Stage 1: Build the Go binary
# Go binaries to be compatible to yours!

FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
COPY agent/go.mod agent/go.sum* ./agent/
COPY model/go.mod model/go.sum* ./model/
COPY server/go.mod server/go.sum* ./server/

RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the statically linked Linux binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/csv-neural-network .

# Minimal runtime image
FROM alpine:3.20

WORKDIR /app

# Copy the compiled binary and initial training dataset
COPY --from=builder /app/csv-neural-network /app/csv-neural-network
COPY --from=builder /app/dataset.csv /app/dataset.csv

# Ensure directories exist for persistent data
RUN mkdir -p /app/persisted_csvs

EXPOSE 8080

CMD ["/app/csv-neural-network"]

