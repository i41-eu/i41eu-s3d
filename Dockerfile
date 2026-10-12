# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY main.go ./
COPY web ./web
COPY data ./data
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/i41eu-s3d .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/i41eu-s3d /i41eu-s3d
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/i41eu-s3d", "-listen", ":8080"]
