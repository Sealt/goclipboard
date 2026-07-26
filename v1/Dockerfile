FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY static ./static

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath -ldflags="-s -w" -o /out/goclipboard .

FROM scratch

COPY --from=build /out/goclipboard /goclipboard

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/goclipboard"]
