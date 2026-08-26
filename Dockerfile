FROM docker.m.daocloud.io/library/golang:1.26.3-bookworm AS build

WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/atomjump ./cmd/task265-atomjump

FROM docker.m.daocloud.io/library/alpine:3.20
COPY --from=build /app/atomjump /app/atomjump
ENTRYPOINT ["/app/atomjump"]
CMD ["--smoke-test"]
