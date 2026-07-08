# minato-server image. Built by `make deploy-server` and pushed to the local
# kind registry; not published anywhere.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/minato-server ./cmd/minato-server

FROM alpine:3.22
# git: smart HTTP receiver (git receive-pack / git archive)
# curl: post-receive hooks call back into the server to run the pipeline
RUN apk add --no-cache git curl
COPY --from=build /out/minato-server /usr/local/bin/minato-server
ENTRYPOINT ["minato-server"]
