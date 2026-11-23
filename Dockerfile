FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN gofmt -w $(find . -name '*.go' -type f) && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/crag ./cmd/crag
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/crag /app/crag
COPY --chown=nonroot:nonroot data /app/data
ENV CRAG_KB_PATH=/app/data/knowledge_base.json CRAG_EVAL_PATH=/app/data/evaluation_set.json CRAG_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/crag"]
CMD ["serve"]
