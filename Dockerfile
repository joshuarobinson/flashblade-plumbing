FROM golang:1.16.3-alpine3.13 AS builder

RUN apk add build-base git musl-dev

RUN mkdir /app

ADD . /app
WORKDIR /app

# Run go build to compile
RUN go build -tags musl -o fb-plumbing .

# Copy only the binary into the final Docker image
FROM golang:1.16.3-alpine3.13
COPY --from=builder /app/fb-plumbing /app/fb-plumbing

# Set entrypoint to automatically invoke program
ENTRYPOINT ["/app/fb-plumbing"]
