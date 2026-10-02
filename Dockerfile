FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /sitewatch .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /sitewatch /sitewatch
EXPOSE 9115
ENTRYPOINT ["/sitewatch"]
CMD ["serve", "-targets", "/etc/sitewatch/targets.txt"]
