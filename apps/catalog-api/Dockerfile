FROM golang:1.27.2-bookworm AS build
WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /catalog-api ./cmd/catalog-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /catalog-api /catalog-api
ENV PORT=8082
EXPOSE 8082
ENTRYPOINT ["/catalog-api"]
