FROM golang:1.15.8-alpine3.13 AS builder

RUN apk add build-base git musl-dev

RUN mkdir /app

RUN go get -tags musl -u github.com/llaaiiqq/go-nfs-client/nfs
RUN go get -u github.com/aws/aws-sdk-go/...

ADD . /app
WORKDIR /app

# Run go build to compile
RUN go build -tags musl -o main .

# Copy only the binary into the final Docker image
FROM golang:1.15.8-alpine3.13
COPY --from=builder /app/main /app/main

# Set entrypoint to automatically invoke program
ENTRYPOINT ["/app/main"]
