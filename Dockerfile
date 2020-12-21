#----------------- Build
FROM golang:1.15.6-buster as build

WORKDIR /go/src/app
ADD . /go/src/app

RUN go get -d -v ./...

RUN go build -o /go/bin/k8s-nodeless


#----------------- Running
FROM gcr.io/distroless/base-debian10
COPY --from=build /go/bin/k8s-nodeless /
CMD ["/k8s-nodeless"]