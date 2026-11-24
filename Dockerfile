# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/crag ./cmd/crag

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/crag /crag
# The app listens on $PORT when the host sets it (Render, Railway, Cloud Run), else :8000.
EXPOSE 8000
USER nonroot
ENTRYPOINT ["/crag"]
CMD ["serve"]
