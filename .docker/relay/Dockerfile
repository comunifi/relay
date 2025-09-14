FROM golang:1.23.1-alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=arm64 \
    GOPROXY=https://goproxy.cn,direct 
    # 'cn' is used because errors appears to be TLS handshake issues specifically on Hetzner servers.

# create build folder
RUN mkdir -p /relay-build

# create binaries folder
RUN mkdir -p /relay

# move into build folder
WORKDIR /relay-build

# copy all go files into our container
COPY . .

# copy the .env file into our binaries folder
COPY .env /relay/.env
# COPY firebase.json /cw/firebase.json

# install all plugins
RUN go mod download

# build
RUN go build -o /relay/main ./cmd/main.go

# clean up container
RUN rm -rf /relay-build

# Build a small image
FROM golang:1.23.1-alpine

RUN mkdir -p /relay

COPY --from=builder /relay/main /relay

COPY --from=builder /relay/.env /relay
# COPY --from=builder /cw/firebase.json /cw

# define the command to be run on launch
ENTRYPOINT ["/relay/main"]

CMD ["-env", "/relay/.env", "-notify"]