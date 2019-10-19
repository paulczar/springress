# build stage
FROM golang:alpine AS build
RUN apk --no-cache add build-base git bzr mercurial gcc
ADD . /src
RUN cd /src && go build -o springress-controller

# final stage
FROM alpine
WORKDIR /app
COPY --from=build /src/springress-controller /app/
CMD ./springress-controller