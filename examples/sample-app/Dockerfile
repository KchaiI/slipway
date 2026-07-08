FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /app .

FROM scratch
COPY --from=build /app /app
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app"]
