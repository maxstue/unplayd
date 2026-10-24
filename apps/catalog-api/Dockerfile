FROM golang:1.27.2-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /catalog-api ./cmd/catalog-api && CGO_ENABLED=0 go build -trimpath -o /migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /catalog-api /catalog-api
COPY --from=build /migrate /migrate
ENV PORT=8446
EXPOSE 8446
ENTRYPOINT ["/catalog-api"]
